from __future__ import annotations

import re
from pathlib import Path

from .knowledge_graph import GraphNode, KnowledgeGraph, RelationType


def _slugify(title: str) -> str:
    slug = title.strip().lower()
    slug = re.sub(r"[^a-z0-9]+", "_", slug)
    return slug.strip("_")


_RELATION_MAP: dict[str, RelationType] = {
    "REQUIRES": RelationType.REQUIRES,
    "ENABLES": RelationType.ENABLES,
    "CONTRADICTS": RelationType.CONTRADICTS,
    "PRECEDES": RelationType.PRECEDES,
    "ANALOGOUS_TO": RelationType.ANALOGOUS_TO,
    "CAUSES": RelationType.CAUSES,
    "REFINES": RelationType.REFINES,
    "IS_A": RelationType.IS_A,
    "PART_OF": RelationType.PART_OF,
    "HAS_A": RelationType.HAS_A,
    "RELATED_TO": RelationType.RELATED_TO,
    "SIMILAR_TO": RelationType.SIMILAR_TO,
    "DEPENDS_ON": RelationType.DEPENDS_ON,
}


class KnowledgeLoader:
    def load_directory(self, dir_path: str) -> KnowledgeGraph:
        graph = KnowledgeGraph()
        md_files = sorted(Path(dir_path).glob("*.md"))
        deferred_edges: list[tuple[str, str, RelationType]] = []
        for md_file in md_files:
            node_edges = self.load_file(str(md_file), graph)
            deferred_edges.extend(node_edges)
        for source_id, target_id, rel_type in deferred_edges:
            if graph.get_node(source_id) and graph.get_node(target_id):
                graph.add_edge(
                    source_id, target_id, rel_type, weight=0.5, bidirectional=True
                )
        return graph

    def load_file(
        self, file_path: str, graph: KnowledgeGraph
    ) -> list[tuple[str, str, RelationType]]:
        content = Path(file_path).read_text()
        domain, entries = self._parse_markdown(content)
        if not domain:
            domain = Path(file_path).stem.replace("_", " ").title()

        deferred_edges: list[tuple[str, str, RelationType]] = []
        for entry in entries:
            node_id = entry["id"]
            node = graph.add_node(
                node_id=node_id,
                label=entry["label"],
                node_type=entry.get("type", "fact"),
                content=entry.get("content", ""),
                summary=entry.get("summary", ""),
                importance=float(entry.get("importance", 0.5)),
                valid_until=float(entry.get("valid_until", 0.0)),
            )
            node.metadata["domain"] = domain
            for rel_type, target_id in entry.get("edges", []):
                deferred_edges.append((node_id, target_id, rel_type))
        return deferred_edges

    @staticmethod
    def add_node(
        graph: KnowledgeGraph,
        node_id: str,
        label: str,
        content: str,
        domain: str,
        node_type: str = "fact",
        summary: str = "",
        importance: float = 0.5,
        edges: list[tuple[str, str]] | None = None,
        valid_until: float = 0.0,
    ) -> GraphNode:
        node = graph.add_node(
            node_id=node_id,
            label=label,
            node_type=node_type,
            content=content,
            summary=summary or label,
            importance=importance,
            valid_until=valid_until,
        )
        node.metadata["domain"] = domain

        if edges:
            for rel_str, target_id in edges:
                rel_type = _RELATION_MAP.get(rel_str.upper(), RelationType.RELATED_TO)
                if graph.get_node(target_id):
                    graph.add_edge(
                        node_id, target_id, rel_type, weight=0.5, bidirectional=True
                    )
        return node

    @staticmethod
    def remove_node(graph: KnowledgeGraph, node_id: str) -> bool:
        if node_id not in graph.nodes:
            return False

        graph.edges = [
            e for e in graph.edges if e.source_id != node_id and e.target_id != node_id
        ]
        graph.outgoing_edges.pop(node_id, None)
        graph.incoming_edges.pop(node_id, None)
        for nid in list(graph.outgoing_edges):
            graph.outgoing_edges[nid] = [
                e for e in graph.outgoing_edges[nid] if e.target_id != node_id
            ]
        for nid in list(graph.incoming_edges):
            graph.incoming_edges[nid] = [
                e for e in graph.incoming_edges[nid] if e.source_id != node_id
            ]
        for rel_type in list(graph.edges_by_type):
            graph.edges_by_type[rel_type] = [
                e
                for e in graph.edges_by_type[rel_type]
                if e.source_id != node_id and e.target_id != node_id
            ]

        node = graph.nodes.pop(node_id)
        if node.node_type in graph.nodes_by_type:
            graph.nodes_by_type[node.node_type].discard(node_id)
        graph.edge_count = len(graph.edges)
        return True

    def _parse_markdown(self, content: str) -> tuple[str, list[dict]]:
        entries: list[dict] = []
        current: dict | None = None
        domain: str = ""

        for line in content.split("\n"):
            if line.startswith("# ") and not line.startswith("## "):
                domain = line[2:].strip()
                continue

            if line.startswith("## "):
                if current is not None:
                    current["content"] = current["content"].strip()
                    entries.append(current)
                title = line[3:].strip()
                current = {
                    "id": _slugify(title),
                    "label": title,
                    "content": "",
                    "edges": [],
                }
                continue

            if current is None:
                continue

            meta_match = re.match(r"^- (\w+):\s*(.+)$", line)
            if meta_match:
                key, value = meta_match.group(1), meta_match.group(2).strip()
                if key == "edges":
                    current["edges"] = self._parse_edges(value)
                elif key in ("importance", "valid_until"):
                    current[key] = float(value)
                else:
                    current[key] = value
                continue

            current["content"] += line + "\n"

        if current is not None:
            current["content"] = current["content"].strip()
            entries.append(current)

        return domain, entries

    @staticmethod
    def _parse_edges(edge_str: str) -> list[tuple[RelationType, str]]:
        edges: list[tuple[RelationType, str]] = []
        for part in edge_str.split(","):
            part = part.strip()
            if ":" not in part:
                continue
            rel_str, target = part.split(":", 1)
            rel_str = rel_str.strip().upper()
            target = target.strip()
            rel_type = _RELATION_MAP.get(rel_str, RelationType.RELATED_TO)
            edges.append((rel_type, target))
        return edges
