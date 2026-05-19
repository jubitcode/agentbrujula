import time as _time

import numpy as np
from typing import Dict, List, Optional, Any, Set, Tuple
from dataclasses import dataclass, field
from enum import Enum
from collections import defaultdict
import json


class RelationType(Enum):
    IS_A = "is_a"
    PART_OF = "part_of"
    HAS_A = "has_a"
    RELATED_TO = "related_to"
    SIMILAR_TO = "similar_to"
    OPPOSITE_OF = "opposite_of"
    CREATED_BY = "created_by"
    USED_BY = "used_by"
    DEPENDS_ON = "depends_on"
    BEFORE = "before"
    AFTER = "after"
    DURING = "during"
    LOCATED_IN = "located_in"
    ADJACENT_TO = "adjacent_to"
    CONTAINS = "contains"
    ROUTES_TO = "routes_to"
    QUERIES = "queries"
    STORES = "stores"
    REQUIRES = "requires"
    ENABLES = "enables"
    CONTRADICTS = "contradicts"
    PRECEDES = "precedes"
    ANALOGOUS_TO = "analogous_to"
    CAUSES = "causes"
    REFINES = "refines"
    CUSTOM = "custom"


@dataclass
class GraphNode:
    id: str
    label: str
    node_type: str
    attributes: Dict[str, Any] = field(default_factory=dict)
    embedding: Optional[np.ndarray] = None
    created_at: float = 0.0
    updated_at: float = 0.0
    metadata: Dict[str, Any] = field(default_factory=dict)
    content: str = ""
    summary: str = ""
    importance: float = 0.5
    access_count: int = 0
    last_accessed: float = 0.0
    pinned: bool = False
    valid_until: float = 0.0

    def get_attribute(self, key: str, default: Any = None) -> Any:
        return self.attributes.get(key, default)

    def set_attribute(self, key: str, value: Any):
        self.attributes[key] = value

    def to_dict(self) -> Dict[str, Any]:
        return {
            "id": self.id,
            "label": self.label,
            "node_type": self.node_type,
            "attributes": self.attributes,
            "metadata": self.metadata,
            "content": self.content,
            "summary": self.summary,
            "importance": self.importance,
            "access_count": self.access_count,
            "last_accessed": self.last_accessed,
            "pinned": self.pinned,
            "valid_until": self.valid_until,
        }


@dataclass
class GraphEdge:
    source_id: str
    target_id: str
    relation_type: RelationType
    weight: float = 1.0
    confidence: float = 1.0
    bidirectional: bool = False
    context: Dict[str, Any] = field(default_factory=dict)
    metadata: Dict[str, Any] = field(default_factory=dict)
    co_activations: int = 0
    successful_co_activations: int = 0
    last_strengthened: float = 0.0
    last_decayed: float = 0.0
    success_by_task_type: Dict[str, list] = field(default_factory=dict)

    def __post_init__(self):
        self.weight = max(0.0, min(1.0, self.weight))

    def to_dict(self) -> Dict[str, Any]:
        return {
            "source_id": self.source_id,
            "target_id": self.target_id,
            "relation_type": self.relation_type.value,
            "weight": self.weight,
            "confidence": self.confidence,
            "bidirectional": self.bidirectional,
            "context": self.context,
            "co_activations": self.co_activations,
            "successful_co_activations": self.successful_co_activations,
            "last_strengthened": self.last_strengthened,
            "last_decayed": self.last_decayed,
            "success_by_task_type": self.success_by_task_type,
        }


class KnowledgeGraph:
    def __init__(self):
        self.nodes: Dict[str, GraphNode] = {}
        self.edges: List[GraphEdge] = []
        self.outgoing_edges: Dict[str, List[GraphEdge]] = defaultdict(list)
        self.incoming_edges: Dict[str, List[GraphEdge]] = defaultdict(list)
        self.nodes_by_type: Dict[str, Set[str]] = defaultdict(set)
        self.edges_by_type: Dict[RelationType, List[GraphEdge]] = defaultdict(list)
        self.edge_count = 0

    def add_node(
        self,
        node_id: str,
        label: str,
        node_type: str,
        attributes: Optional[Dict[str, Any]] = None,
        embedding: Optional[np.ndarray] = None,
        content: str = "",
        summary: str = "",
        importance: float = 0.5,
        pinned: bool = False,
        valid_until: float = 0.0,
    ) -> GraphNode:
        if node_id in self.nodes:
            node = self.nodes[node_id]
            node.label = label
            node.node_type = node_type
            if attributes:
                node.attributes.update(attributes)
            if embedding is not None:
                node.embedding = embedding
            if content:
                node.content = content
            if summary:
                node.summary = summary
            node.importance = importance
            node.pinned = pinned
            node.valid_until = valid_until
            return node

        node = GraphNode(
            id=node_id,
            label=label,
            node_type=node_type,
            attributes=attributes or {},
            embedding=embedding,
            content=content,
            summary=summary,
            importance=importance,
            pinned=pinned,
            valid_until=valid_until,
        )
        self.nodes[node_id] = node
        self.nodes_by_type[node_type].add(node_id)
        return node

    def add_edge(
        self,
        source_id: str,
        target_id: str,
        relation_type: RelationType,
        weight: float = 1.0,
        confidence: float = 1.0,
        bidirectional: bool = False,
        context: Optional[Dict[str, Any]] = None,
    ) -> GraphEdge:
        if source_id not in self.nodes:
            raise ValueError(f"Source node {source_id} not found")
        if target_id not in self.nodes:
            raise ValueError(f"Target node {target_id} not found")

        edge = GraphEdge(
            source_id=source_id,
            target_id=target_id,
            relation_type=relation_type,
            weight=weight,
            confidence=confidence,
            bidirectional=bidirectional,
            context=context or {},
        )
        self.edges.append(edge)
        self.outgoing_edges[source_id].append(edge)
        self.incoming_edges[target_id].append(edge)
        self.edges_by_type[relation_type].append(edge)

        if bidirectional:
            reverse_edge = GraphEdge(
                source_id=target_id,
                target_id=source_id,
                relation_type=relation_type,
                weight=weight,
                confidence=confidence,
                bidirectional=False,
                context=context or {},
            )
            self.edges.append(reverse_edge)
            self.outgoing_edges[target_id].append(reverse_edge)
            self.incoming_edges[source_id].append(reverse_edge)

        self.edge_count = len(self.edges)
        return edge

    def get_node(self, node_id: str) -> Optional[GraphNode]:
        return self.nodes.get(node_id)

    def get_nodes_by_type(self, node_type: str) -> List[GraphNode]:
        node_ids = self.nodes_by_type.get(node_type, set())
        return [self.nodes[nid] for nid in node_ids]

    def get_neighbors(
        self,
        node_id: str,
        relation_type: Optional[RelationType] = None,
        direction: str = "outgoing",
    ) -> List[GraphNode]:
        neighbors = []
        if direction in ["outgoing", "both"]:
            edges = self.outgoing_edges.get(node_id, [])
            if relation_type:
                edges = [e for e in edges if e.relation_type == relation_type]
            for edge in edges:
                if edge.target_id in self.nodes:
                    neighbors.append(self.nodes[edge.target_id])
        if direction in ["incoming", "both"]:
            edges = self.incoming_edges.get(node_id, [])
            if relation_type:
                edges = [e for e in edges if e.relation_type == relation_type]
            for edge in edges:
                if edge.source_id in self.nodes:
                    neighbors.append(self.nodes[edge.source_id])
        return neighbors

    def get_strong_edges(
        self,
        node_id: str,
        min_weight: float = 0.6,
    ) -> List[GraphEdge]:
        edges = self.outgoing_edges.get(node_id, [])
        strong = [e for e in edges if e.weight >= min_weight]
        strong.sort(key=lambda e: e.weight, reverse=True)
        return strong

    def get_edges(
        self,
        source_id: Optional[str] = None,
        target_id: Optional[str] = None,
        relation_type: Optional[RelationType] = None,
    ) -> List[GraphEdge]:
        edges = self.edges
        if source_id:
            edges = [e for e in edges if e.source_id == source_id]
        if target_id:
            edges = [e for e in edges if e.target_id == target_id]
        if relation_type:
            edges = [e for e in edges if e.relation_type == relation_type]
        return edges

    def find_path(
        self,
        start_id: str,
        end_id: str,
        max_depth: int = 3,
    ) -> Optional[List[Tuple[GraphNode, GraphEdge]]]:
        if start_id not in self.nodes or end_id not in self.nodes:
            return None
        if start_id == end_id:
            return [(self.nodes[start_id], None)]

        queue = [(start_id, [])]
        visited = {start_id}

        while queue:
            current_id, path = queue.pop(0)
            if len(path) >= max_depth:
                continue
            for edge in self.outgoing_edges.get(current_id, []):
                target_id = edge.target_id
                if target_id == end_id:
                    result_path = path + [(self.nodes[current_id], edge)]
                    result_path.append((self.nodes[end_id], None))
                    return result_path
                if target_id not in visited:
                    visited.add(target_id)
                    new_path = path + [(self.nodes[current_id], edge)]
                    queue.append((target_id, new_path))
        return None

    def find_similar_nodes(
        self,
        node_id: str,
        top_k: int = 5,
        node_type: Optional[str] = None,
    ) -> List[Tuple[GraphNode, float]]:
        if node_id not in self.nodes:
            return []
        reference_node = self.nodes[node_id]
        if reference_node.embedding is None:
            return []

        similarities = []
        for nid, node in self.nodes.items():
            if nid == node_id:
                continue
            if node_type and node.node_type != node_type:
                continue
            if node.embedding is None:
                continue
            similarity = np.dot(reference_node.embedding, node.embedding) / (
                np.linalg.norm(reference_node.embedding)
                * np.linalg.norm(node.embedding)
                + 1e-8
            )
            similarities.append((node, float(similarity)))
        similarities.sort(key=lambda x: x[1], reverse=True)
        return similarities[:top_k]

    def get_subgraph(
        self,
        node_ids: List[str],
        include_edges_between: bool = True,
    ) -> Tuple[List[GraphNode], List[GraphEdge]]:
        nodes = [self.nodes[nid] for nid in node_ids if nid in self.nodes]
        edges = []
        if include_edges_between:
            node_id_set = set(node_ids)
            for edge in self.edges:
                if edge.source_id in node_id_set and edge.target_id in node_id_set:
                    edges.append(edge)
        return nodes, edges

    def get_statistics(self) -> Dict[str, Any]:
        return {
            "node_count": len(self.nodes),
            "edge_count": self.edge_count,
            "node_types": {
                node_type: len(node_ids)
                for node_type, node_ids in self.nodes_by_type.items()
            },
            "relation_types": {
                rel_type.value: len(edges)
                for rel_type, edges in self.edges_by_type.items()
            },
        }

    def get_stale_nodes(
        self,
        stale_access_days: float = 90.0,
        high_weight_threshold: float = 0.7,
        now: float | None = None,
    ) -> List[Dict[str, Any]]:
        now = now or _time.time()
        stale = []
        access_threshold = now - (stale_access_days * 86400)
        for node in self.nodes.values():
            reasons = []
            if 0 < node.valid_until < now:
                reasons.append("expired")
            if (
                node.importance >= high_weight_threshold
                and node.last_accessed > 0
                and node.last_accessed < access_threshold
            ):
                reasons.append("high_weight_stale")
            if reasons:
                stale.append(
                    {
                        "node_id": node.id,
                        "label": node.label,
                        "importance": node.importance,
                        "last_accessed": node.last_accessed,
                        "valid_until": node.valid_until,
                        "reasons": reasons,
                    }
                )
        return stale

    def to_dict(self) -> Dict[str, Any]:
        return {
            "nodes": [node.to_dict() for node in self.nodes.values()],
            "edges": [edge.to_dict() for edge in self.edges],
        }

    def save(self, filepath: str):
        graph_dict = self.to_dict()
        with open(filepath, "w") as f:
            json.dump(graph_dict, f, indent=2)

    def load(self, filepath: str):
        with open(filepath, "r") as f:
            graph_dict = json.load(f)

        self.nodes.clear()
        self.edges.clear()
        self.outgoing_edges.clear()
        self.incoming_edges.clear()
        self.nodes_by_type.clear()
        self.edges_by_type.clear()

        for node_dict in graph_dict["nodes"]:
            node = self.add_node(
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
            node.access_count = node_dict.get("access_count", 0)
            node.last_accessed = node_dict.get("last_accessed", 0.0)

        for edge_dict in graph_dict["edges"]:
            relation_type = RelationType(edge_dict["relation_type"])
            edge = self.add_edge(
                source_id=edge_dict["source_id"],
                target_id=edge_dict["target_id"],
                relation_type=relation_type,
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
