from collections import defaultdict

from .knowledge_graph import KnowledgeGraph
from .hebbian_engine import HebbianEdgeEngine


class ClusterCollapseMonitor:
    def __init__(self, graph: KnowledgeGraph, in_degree_ceiling: float = 5.0):
        self.graph = graph
        self.in_degree_ceiling = in_degree_ceiling
        self.engine = HebbianEdgeEngine(
            graph, in_degree_weight_ceiling=in_degree_ceiling
        )

    def check(self) -> dict:
        in_degree_weights: dict[str, float] = defaultdict(float)
        for edge in self.graph.edges:
            in_degree_weights[edge.target_id] += edge.weight

        dominant = {
            nid: total
            for nid, total in in_degree_weights.items()
            if total > self.in_degree_ceiling
        }

        return {
            "dominant_clusters": dominant,
            "nodes_over_ceiling": len(dominant),
            "max_in_degree_weight": max(in_degree_weights.values())
            if in_degree_weights
            else 0.0,
        }

    def enforce_ceiling(self):
        self.engine.enforce_in_degree_ceiling()


class StalenessMonitor:
    def __init__(
        self,
        graph: KnowledgeGraph,
        stale_access_days: float = 90.0,
        high_weight_threshold: float = 0.7,
    ):
        self.graph = graph
        self.stale_access_days = stale_access_days
        self.high_weight_threshold = high_weight_threshold

    def check(self) -> dict:
        stale = self.graph.get_stale_nodes(
            stale_access_days=self.stale_access_days,
            high_weight_threshold=self.high_weight_threshold,
        )
        return {
            "stale_nodes": stale,
            "stale_count": len(stale),
            "expired_count": sum(1 for s in stale if "expired" in s["reasons"]),
            "high_weight_stale_count": sum(
                1 for s in stale if "high_weight_stale" in s["reasons"]
            ),
        }
