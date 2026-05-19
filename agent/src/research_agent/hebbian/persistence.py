import json
import os
import sqlite3
import time

import numpy as np

from .knowledge_graph import KnowledgeGraph, GraphEdge, RelationType

EXPORT_FORMAT_VERSION = 1


def _use_postgres() -> bool:
    url = os.environ.get("DATABASE_URL", "")
    return url.startswith("postgresql://") or url.startswith("postgres://")


def _pg_conn():
    import psycopg2

    return psycopg2.connect(os.environ["DATABASE_URL"])


class GraphPersistence:
    def __init__(self, db_path: str = "hebbian_graph.db"):
        self.db_path = db_path
        self._pg = _use_postgres()
        self._init_tables()
        self._migrate_tables()

    def _init_tables(self):
        if self._pg:
            self._init_tables_pg()
        else:
            self._init_tables_sqlite()

    def _init_tables_sqlite(self):
        with sqlite3.connect(self.db_path) as conn:
            conn.execute("""
                CREATE TABLE IF NOT EXISTS nodes (
                    id TEXT PRIMARY KEY,
                    label TEXT NOT NULL,
                    node_type TEXT NOT NULL,
                    attributes TEXT DEFAULT '{}',
                    embedding BLOB,
                    created_at REAL DEFAULT 0.0,
                    updated_at REAL DEFAULT 0.0,
                    metadata TEXT DEFAULT '{}',
                    content TEXT DEFAULT '',
                    summary TEXT DEFAULT '',
                    importance REAL DEFAULT 0.5,
                    access_count INTEGER DEFAULT 0,
                    last_accessed REAL DEFAULT 0.0,
                    pinned INTEGER DEFAULT 0,
                    valid_until REAL DEFAULT 0.0
                )
            """)
            conn.execute("""
                CREATE TABLE IF NOT EXISTS edges (
                    source_id TEXT NOT NULL,
                    target_id TEXT NOT NULL,
                    relation_type TEXT NOT NULL,
                    weight REAL DEFAULT 1.0,
                    confidence REAL DEFAULT 1.0,
                    bidirectional INTEGER DEFAULT 0,
                    context TEXT DEFAULT '{}',
                    metadata TEXT DEFAULT '{}',
                    co_activations INTEGER DEFAULT 0,
                    successful_co_activations INTEGER DEFAULT 0,
                    last_strengthened REAL DEFAULT 0.0,
                    last_decayed REAL DEFAULT 0.0,
                    success_by_task_type TEXT DEFAULT '{}'
                )
            """)
            conn.execute("""
                CREATE UNIQUE INDEX IF NOT EXISTS idx_edges_unique
                ON edges(source_id, target_id, relation_type)
            """)
            conn.execute("""
                CREATE TABLE IF NOT EXISTS graph_metadata (
                    key TEXT PRIMARY KEY,
                    value TEXT NOT NULL
                )
            """)
            conn.commit()

    def _init_tables_pg(self):
        conn = _pg_conn()
        try:
            cur = conn.cursor()
            cur.execute("""
                CREATE TABLE IF NOT EXISTS hebbian_nodes (
                    id TEXT PRIMARY KEY,
                    label TEXT NOT NULL,
                    node_type TEXT NOT NULL,
                    attributes TEXT DEFAULT '{}',
                    embedding BYTEA,
                    created_at DOUBLE PRECISION DEFAULT 0.0,
                    updated_at DOUBLE PRECISION DEFAULT 0.0,
                    metadata TEXT DEFAULT '{}',
                    content TEXT DEFAULT '',
                    summary TEXT DEFAULT '',
                    importance DOUBLE PRECISION DEFAULT 0.5,
                    access_count INTEGER DEFAULT 0,
                    last_accessed DOUBLE PRECISION DEFAULT 0.0,
                    pinned INTEGER DEFAULT 0,
                    valid_until DOUBLE PRECISION DEFAULT 0.0
                )
            """)
            cur.execute("""
                CREATE TABLE IF NOT EXISTS hebbian_edges (
                    source_id TEXT NOT NULL,
                    target_id TEXT NOT NULL,
                    relation_type TEXT NOT NULL,
                    weight DOUBLE PRECISION DEFAULT 1.0,
                    confidence DOUBLE PRECISION DEFAULT 1.0,
                    bidirectional INTEGER DEFAULT 0,
                    context TEXT DEFAULT '{}',
                    metadata TEXT DEFAULT '{}',
                    co_activations INTEGER DEFAULT 0,
                    successful_co_activations INTEGER DEFAULT 0,
                    last_strengthened DOUBLE PRECISION DEFAULT 0.0,
                    last_decayed DOUBLE PRECISION DEFAULT 0.0,
                    success_by_task_type TEXT DEFAULT '{}',
                    UNIQUE(source_id, target_id, relation_type)
                )
            """)
            cur.execute("""
                CREATE TABLE IF NOT EXISTS hebbian_metadata (
                    key TEXT PRIMARY KEY,
                    value TEXT NOT NULL
                )
            """)
            conn.commit()
        finally:
            conn.close()

    def _migrate_tables(self):
        if self._pg:
            self._migrate_tables_pg()
        else:
            self._migrate_tables_sqlite()

    def _migrate_tables_sqlite(self):
        with sqlite3.connect(self.db_path) as conn:
            cursor = conn.execute("PRAGMA table_info(nodes)")
            columns = {row[1] for row in cursor.fetchall()}
            if "valid_until" not in columns:
                conn.execute(
                    "ALTER TABLE nodes ADD COLUMN valid_until REAL DEFAULT 0.0"
                )
            conn.execute("""
                CREATE UNIQUE INDEX IF NOT EXISTS idx_edges_unique
                ON edges(source_id, target_id, relation_type)
            """)
            edge_cursor = conn.execute("PRAGMA table_info(edges)")
            edge_columns = {row[1] for row in edge_cursor.fetchall()}
            if "success_by_task_type" not in edge_columns:
                conn.execute(
                    "ALTER TABLE edges ADD COLUMN success_by_task_type TEXT DEFAULT '{}'"
                )
            conn.commit()

    def _migrate_tables_pg(self):
        conn = _pg_conn()
        try:
            cur = conn.cursor()
            cur.execute("""
                SELECT column_name FROM information_schema.columns
                WHERE table_name = 'hebbian_nodes'
            """)
            columns = {row[0] for row in cur.fetchall()}
            if columns and "valid_until" not in columns:
                cur.execute(
                    "ALTER TABLE hebbian_nodes ADD COLUMN valid_until DOUBLE PRECISION DEFAULT 0.0"
                )
            cur.execute("""
                SELECT column_name FROM information_schema.columns
                WHERE table_name = 'hebbian_edges'
            """)
            edge_columns = {row[0] for row in cur.fetchall()}
            if edge_columns and "success_by_task_type" not in edge_columns:
                cur.execute(
                    "ALTER TABLE hebbian_edges ADD COLUMN success_by_task_type TEXT DEFAULT '{}'"
                )
            conn.commit()
        finally:
            conn.close()

    def get_metadata(self, key: str) -> str | None:
        if self._pg:
            conn = _pg_conn()
            try:
                cur = conn.cursor()
                cur.execute("SELECT value FROM hebbian_metadata WHERE key = %s", (key,))
                row = cur.fetchone()
                return row[0] if row else None
            finally:
                conn.close()
        else:
            with sqlite3.connect(self.db_path) as conn:
                row = conn.execute(
                    "SELECT value FROM graph_metadata WHERE key = ?", (key,)
                ).fetchone()
                return row[0] if row else None

    def set_metadata(self, key: str, value: str) -> None:
        if self._pg:
            conn = _pg_conn()
            try:
                cur = conn.cursor()
                cur.execute(
                    "INSERT INTO hebbian_metadata (key, value) VALUES (%s, %s) "
                    "ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value",
                    (key, value),
                )
                conn.commit()
            finally:
                conn.close()
        else:
            with sqlite3.connect(self.db_path) as conn:
                conn.execute(
                    "INSERT OR REPLACE INTO graph_metadata (key, value) VALUES (?, ?)",
                    (key, value),
                )
                conn.commit()

    def save_graph(self, graph: KnowledgeGraph) -> None:
        if self._pg:
            self._save_graph_pg(graph)
        else:
            self._save_graph_sqlite(graph)

    def _save_graph_sqlite(self, graph: KnowledgeGraph) -> None:
        with sqlite3.connect(self.db_path) as conn:
            conn.execute("DELETE FROM nodes")
            conn.execute("DELETE FROM edges")
            for node in graph.nodes.values():
                embedding_blob = (
                    node.embedding.tobytes() if node.embedding is not None else None
                )
                conn.execute(
                    """INSERT INTO nodes
                       (id, label, node_type, attributes, embedding, created_at, updated_at,
                        metadata, content, summary, importance, access_count, last_accessed,
                        pinned, valid_until)
                       VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)""",
                    (
                        node.id,
                        node.label,
                        node.node_type,
                        json.dumps(node.attributes),
                        embedding_blob,
                        node.created_at,
                        node.updated_at,
                        json.dumps(node.metadata),
                        node.content,
                        node.summary,
                        node.importance,
                        node.access_count,
                        node.last_accessed,
                        int(node.pinned),
                        node.valid_until,
                    ),
                )
            for edge in graph.edges:
                conn.execute(
                    """INSERT OR REPLACE INTO edges
                       (source_id, target_id, relation_type, weight, confidence,
                        bidirectional, context, metadata, co_activations,
                        successful_co_activations, last_strengthened, last_decayed,
                        success_by_task_type)
                       VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)""",
                    (
                        edge.source_id,
                        edge.target_id,
                        edge.relation_type.value,
                        edge.weight,
                        edge.confidence,
                        int(edge.bidirectional),
                        json.dumps(edge.context),
                        json.dumps(edge.metadata),
                        edge.co_activations,
                        edge.successful_co_activations,
                        edge.last_strengthened,
                        edge.last_decayed,
                        json.dumps(edge.success_by_task_type),
                    ),
                )
            conn.commit()

    def _save_graph_pg(self, graph: KnowledgeGraph) -> None:
        conn = _pg_conn()
        try:
            cur = conn.cursor()
            cur.execute("DELETE FROM hebbian_nodes")
            cur.execute("DELETE FROM hebbian_edges")
            for node in graph.nodes.values():
                embedding_blob = (
                    node.embedding.tobytes() if node.embedding is not None else None
                )
                cur.execute(
                    """INSERT INTO hebbian_nodes
                       (id, label, node_type, attributes, embedding, created_at, updated_at,
                        metadata, content, summary, importance, access_count, last_accessed,
                        pinned, valid_until)
                       VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s)""",
                    (
                        node.id,
                        node.label,
                        node.node_type,
                        json.dumps(node.attributes),
                        embedding_blob,
                        node.created_at,
                        node.updated_at,
                        json.dumps(node.metadata),
                        node.content,
                        node.summary,
                        node.importance,
                        node.access_count,
                        node.last_accessed,
                        int(node.pinned),
                        node.valid_until,
                    ),
                )
            for edge in graph.edges:
                cur.execute(
                    """INSERT INTO hebbian_edges
                       (source_id, target_id, relation_type, weight, confidence,
                        bidirectional, context, metadata, co_activations,
                        successful_co_activations, last_strengthened, last_decayed,
                        success_by_task_type)
                       VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s)
                       ON CONFLICT (source_id, target_id, relation_type) DO UPDATE SET
                        weight = EXCLUDED.weight, confidence = EXCLUDED.confidence,
                        bidirectional = EXCLUDED.bidirectional, context = EXCLUDED.context,
                        metadata = EXCLUDED.metadata, co_activations = EXCLUDED.co_activations,
                        successful_co_activations = EXCLUDED.successful_co_activations,
                        last_strengthened = EXCLUDED.last_strengthened,
                        last_decayed = EXCLUDED.last_decayed,
                        success_by_task_type = EXCLUDED.success_by_task_type""",
                    (
                        edge.source_id,
                        edge.target_id,
                        edge.relation_type.value,
                        edge.weight,
                        edge.confidence,
                        int(edge.bidirectional),
                        json.dumps(edge.context),
                        json.dumps(edge.metadata),
                        edge.co_activations,
                        edge.successful_co_activations,
                        edge.last_strengthened,
                        edge.last_decayed,
                        json.dumps(edge.success_by_task_type),
                    ),
                )
            conn.commit()
        finally:
            conn.close()

    def load_graph(self) -> KnowledgeGraph:
        if self._pg:
            return self._load_graph_pg()
        return self._load_graph_sqlite()

    def _load_graph_sqlite(self) -> KnowledgeGraph:
        graph = KnowledgeGraph()
        with sqlite3.connect(self.db_path) as conn:
            conn.row_factory = sqlite3.Row
            for row in conn.execute("SELECT * FROM nodes"):
                node = graph.add_node(
                    node_id=row["id"],
                    label=row["label"],
                    node_type=row["node_type"],
                    attributes=json.loads(row["attributes"]),
                    content=row["content"],
                    summary=row["summary"],
                    importance=row["importance"],
                    pinned=bool(row["pinned"]),
                    valid_until=row["valid_until"],
                )
                node.created_at = row["created_at"]
                node.updated_at = row["updated_at"]
                node.metadata = json.loads(row["metadata"])
                node.access_count = row["access_count"]
                node.last_accessed = row["last_accessed"]
                if row["embedding"] is not None:
                    node.embedding = np.frombuffer(row["embedding"], dtype=np.float32)

            for row in conn.execute("SELECT * FROM edges"):
                edge = graph.add_edge(
                    source_id=row["source_id"],
                    target_id=row["target_id"],
                    relation_type=RelationType(row["relation_type"]),
                    weight=row["weight"],
                    confidence=row["confidence"],
                    bidirectional=False,
                    context=json.loads(row["context"]),
                )
                edge.metadata = json.loads(row["metadata"])
                edge.co_activations = row["co_activations"]
                edge.successful_co_activations = row["successful_co_activations"]
                edge.last_strengthened = row["last_strengthened"]
                edge.last_decayed = row["last_decayed"]
                sbt = (
                    row["success_by_task_type"]
                    if "success_by_task_type" in row.keys()
                    else "{}"
                )
                edge.success_by_task_type = json.loads(sbt) if sbt else {}
        return graph

    def _load_graph_pg(self) -> KnowledgeGraph:
        import psycopg2.extras

        graph = KnowledgeGraph()
        conn = _pg_conn()
        try:
            cur = conn.cursor(cursor_factory=psycopg2.extras.RealDictCursor)
            cur.execute("SELECT * FROM hebbian_nodes")
            for row in cur.fetchall():
                node = graph.add_node(
                    node_id=row["id"],
                    label=row["label"],
                    node_type=row["node_type"],
                    attributes=json.loads(row["attributes"]),
                    content=row["content"],
                    summary=row["summary"],
                    importance=row["importance"],
                    pinned=bool(row["pinned"]),
                    valid_until=row["valid_until"],
                )
                node.created_at = row["created_at"]
                node.updated_at = row["updated_at"]
                node.metadata = json.loads(row["metadata"])
                node.access_count = row["access_count"]
                node.last_accessed = row["last_accessed"]
                if row["embedding"] is not None:
                    node.embedding = np.frombuffer(
                        bytes(row["embedding"]), dtype=np.float32
                    )

            cur.execute("SELECT * FROM hebbian_edges")
            for row in cur.fetchall():
                edge = graph.add_edge(
                    source_id=row["source_id"],
                    target_id=row["target_id"],
                    relation_type=RelationType(row["relation_type"]),
                    weight=row["weight"],
                    confidence=row["confidence"],
                    bidirectional=False,
                    context=json.loads(row["context"]),
                )
                edge.metadata = json.loads(row["metadata"])
                edge.co_activations = row["co_activations"]
                edge.successful_co_activations = row["successful_co_activations"]
                edge.last_strengthened = row["last_strengthened"]
                edge.last_decayed = row["last_decayed"]
                sbt = row.get("success_by_task_type", "{}")
                edge.success_by_task_type = json.loads(sbt) if sbt else {}
        finally:
            conn.close()
        return graph

    def update_edge_weights(self, edges: list[GraphEdge]) -> None:
        if self._pg:
            self._update_edge_weights_pg(edges)
        else:
            self._update_edge_weights_sqlite(edges)

    def _update_edge_weights_sqlite(self, edges: list[GraphEdge]) -> None:
        with sqlite3.connect(self.db_path) as conn:
            for edge in edges:
                conn.execute(
                    """UPDATE edges SET weight=?, co_activations=?,
                       successful_co_activations=?, last_strengthened=?, last_decayed=?,
                       success_by_task_type=?
                       WHERE source_id=? AND target_id=? AND relation_type=?""",
                    (
                        edge.weight,
                        edge.co_activations,
                        edge.successful_co_activations,
                        edge.last_strengthened,
                        edge.last_decayed,
                        json.dumps(edge.success_by_task_type),
                        edge.source_id,
                        edge.target_id,
                        edge.relation_type.value,
                    ),
                )
            conn.commit()

    def _update_edge_weights_pg(self, edges: list[GraphEdge]) -> None:
        conn = _pg_conn()
        try:
            cur = conn.cursor()
            for edge in edges:
                cur.execute(
                    """UPDATE hebbian_edges SET weight=%s, co_activations=%s,
                       successful_co_activations=%s, last_strengthened=%s, last_decayed=%s,
                       success_by_task_type=%s
                       WHERE source_id=%s AND target_id=%s AND relation_type=%s""",
                    (
                        edge.weight,
                        edge.co_activations,
                        edge.successful_co_activations,
                        edge.last_strengthened,
                        edge.last_decayed,
                        json.dumps(edge.success_by_task_type),
                        edge.source_id,
                        edge.target_id,
                        edge.relation_type.value,
                    ),
                )
            conn.commit()
        finally:
            conn.close()

    def export_graph(
        self,
        graph: KnowledgeGraph,
        filepath: str,
        domain: str = "",
        description: str = "",
        author: str = "",
        exclude_episodes: bool = True,
    ) -> None:
        """Export graph as versioned JSON with provenance metadata."""
        nodes_data = []
        for node in graph.nodes.values():
            if exclude_episodes and node.node_type == "episode":
                continue
            d = node.to_dict()
            d["created_at"] = node.created_at
            d["updated_at"] = node.updated_at
            nodes_data.append(d)

        edges_data = []
        exported_ids = {n["id"] for n in nodes_data}
        for edge in graph.edges:
            if edge.source_id in exported_ids and edge.target_id in exported_ids:
                edges_data.append(edge.to_dict())

        export = {
            "format_version": EXPORT_FORMAT_VERSION,
            "exported_at": time.time(),
            "provenance": {
                "domain": domain,
                "description": description,
                "author": author,
            },
            "graph": {
                "nodes": nodes_data,
                "edges": edges_data,
            },
        }

        with open(filepath, "w") as f:
            json.dump(export, f, indent=2)

    @staticmethod
    def import_graph(filepath: str) -> tuple[KnowledgeGraph, dict]:
        """Import graph from versioned JSON export."""
        with open(filepath) as f:
            data = json.load(f)

        version = data.get("format_version", 0)
        if version != EXPORT_FORMAT_VERSION:
            raise ValueError(
                f"Unsupported export format version: {version} "
                f"(expected {EXPORT_FORMAT_VERSION})"
            )

        graph = KnowledgeGraph()

        for node_dict in data["graph"]["nodes"]:
            node = graph.add_node(
                node_id=node_dict["id"],
                label=node_dict["label"],
                node_type=node_dict["node_type"],
                attributes=node_dict.get("attributes", {}),
                content=node_dict.get("content", ""),
                summary=node_dict.get("summary", ""),
                importance=node_dict.get("importance", 0.5),
                pinned=node_dict.get("pinned", False),
                valid_until=node_dict.get("valid_until", 0.0),
            )
            node.created_at = node_dict.get("created_at", 0.0)
            node.updated_at = node_dict.get("updated_at", 0.0)
            node.metadata = node_dict.get("metadata", {})
            node.access_count = node_dict.get("access_count", 0)
            node.last_accessed = node_dict.get("last_accessed", 0.0)

        for edge_dict in data["graph"]["edges"]:
            try:
                edge = graph.add_edge(
                    source_id=edge_dict["source_id"],
                    target_id=edge_dict["target_id"],
                    relation_type=RelationType(edge_dict["relation_type"]),
                    weight=edge_dict.get("weight", 1.0),
                    confidence=edge_dict.get("confidence", 1.0),
                    bidirectional=False,
                    context=edge_dict.get("context", {}),
                )
                edge.co_activations = edge_dict.get("co_activations", 0)
                edge.successful_co_activations = edge_dict.get(
                    "successful_co_activations", 0
                )
                edge.last_strengthened = edge_dict.get("last_strengthened", 0.0)
                edge.last_decayed = edge_dict.get("last_decayed", 0.0)
                edge.success_by_task_type = edge_dict.get("success_by_task_type", {})
            except (ValueError, KeyError):
                continue

        metadata = {
            "provenance": data.get("provenance", {}),
            "quality_metrics": data.get("quality_metrics", {}),
            "exported_at": data.get("exported_at", 0),
        }

        return graph, metadata
