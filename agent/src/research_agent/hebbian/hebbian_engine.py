import math
import time
import random
from itertools import combinations

from .knowledge_graph import KnowledgeGraph, RelationType, GraphEdge


class HebbianEdgeEngine:
    def __init__(
        self,
        graph: KnowledgeGraph,
        learning_rate: float = 0.05,
        decay_factor: float = 0.95,
        decay_threshold_days: int = 14,
        prune_threshold: float = 0.05,
        min_weight_for_influence: float = 0.6,
        in_degree_weight_ceiling: float = 5.0,
        half_life_days: float = 30.0,
        max_spacing_bonus: float = 2.0,
    ):
        self.graph = graph
        self.learning_rate = learning_rate
        self.decay_factor = decay_factor
        self.decay_threshold_days = decay_threshold_days
        self.prune_threshold = prune_threshold
        self.min_weight_for_influence = min_weight_for_influence
        self.in_degree_weight_ceiling = in_degree_weight_ceiling
        self.half_life_days = half_life_days
        self.max_spacing_bonus = max_spacing_bonus

    def _get_or_create_edge(self, a: str, b: str):
        edges = self.graph.get_edges(source_id=a, target_id=b)
        if edges:
            return edges[0]
        return self.graph.add_edge(a, b, RelationType.RELATED_TO, weight=0.1)

    @staticmethod
    def _record_task_usage(edge: GraphEdge, task_type: str, success: bool) -> None:
        if task_type not in edge.success_by_task_type:
            edge.success_by_task_type[task_type] = [0, 0]
        edge.success_by_task_type[task_type][success] += 1

    def _strengthen_edge(
        self, edge, importance_factor: float, now: float, task_type: str
    ) -> None:
        time_gap = now - edge.last_strengthened if edge.last_strengthened > 0 else 0
        spacing_bonus = min(
            self.max_spacing_bonus, 1.0 + math.log(1 + time_gap / 86400)
        )
        success_rate = edge.successful_co_activations / max(1, edge.co_activations)
        adaptive_lr = self.learning_rate * (0.5 + 1.5 * success_rate)
        delta = adaptive_lr * importance_factor * spacing_bonus

        # SNAP synapse-wise sigmoidal growth (Xu et al. 2410.15318):
        # ΔW = |W|·(1 - |W|)·δW. Plateaus at 0 and 1, most plastic mid-range,
        # so noisy late-cycle strengthens can't pile on top of a saturated edge.
        w = edge.weight
        snap_delta = abs(w) * (1.0 - abs(w)) * delta

        edge.successful_co_activations += 1
        edge.weight = min(1.0, max(0.0, edge.weight + snap_delta))
        edge.last_strengthened = now

        stats = edge.success_by_task_type.setdefault(task_type, [0, 0])
        stats[0] += 1
        stats[1] += 1

    def strengthen(
        self, node_ids: list[str], task_success: bool, task_type: str = "general"
    ) -> dict:
        now = time.time()
        updated = 0
        for a, b in combinations(node_ids, 2):
            if a not in self.graph.nodes or b not in self.graph.nodes:
                continue

            edge_ab = self._get_or_create_edge(a, b)
            edge_ab.co_activations += 1
            self._record_task_usage(edge_ab, task_type, task_success)

            reverse_edges = self.graph.get_edges(source_id=b, target_id=a)
            edge_ba = reverse_edges[0] if reverse_edges else None
            if edge_ba:
                edge_ba.co_activations += 1
                self._record_task_usage(edge_ba, task_type, task_success)

            if task_success:
                importance_a = self.graph.nodes[a].importance
                importance_b = self.graph.nodes[b].importance
                importance_factor = (importance_a + importance_b) / 2

                self._strengthen_edge(edge_ab, importance_factor, now, task_type)

                if edge_ba:
                    self._strengthen_edge(edge_ba, importance_factor, now, task_type)

            updated += 1
        return {"pairs_updated": updated}

    def sleep(
        self, lam: float = 0.9, w_target: float = 0.1, noise_std: float = 0.0
    ) -> dict:
        """Periodic homeostatic renormalization (Massey et al. 2601.08447, SHY).

        Power-law pull toward `w_target`:
            w <- w_target * (w / w_target) ** lam_effective
        For w > w_target with 0<lam<1, this shrinks weights toward target.
        For w < w_target, it pushes them up. Preserves relative ordering,
        bounds unbounded growth, lets self-eval noise erode between cycles
        the in-session counterpart to `decay_all()` (which is wall-clock gated).

        Heterogeneous: edges that are fresh and frequently successful resist
        decay more (SI-style, per 2405.16922 survey), so learned structure
        survives while ephemeral noise erodes.
        """
        import random as _rng

        touched = 0
        now = time.time()
        for edge in self.graph.edges:
            w = edge.weight
            if w <= 0 or w_target <= 0:
                continue
            # Importance in [0,1] from recency + cumulative useful count.
            age_hours = max(0.0, now - edge.last_strengthened) / 3600.0
            recency = 1.0 / (1.0 + age_hours)
            use_factor = min(1.0, edge.successful_co_activations / 10.0)
            importance = 0.5 * recency + 0.5 * use_factor
            # lam_eff -> 1 (no decay) for important edges; -> lam for neglected.
            lam_eff = lam + (1.0 - lam) * importance
            new_w = w_target * (w / w_target) ** lam_eff
            if noise_std > 0:
                new_w += _rng.gauss(0, noise_std)
            edge.weight = min(1.0, max(0.0, new_w))
            edge.last_decayed = now
            touched += 1
        return {"edges_touched": touched, "lambda": lam, "w_target": w_target}

    def decay_all(self) -> dict:
        """Decay edges using a forgetting curve.

        Edges past the grace period decay with an exponential curve that
        flattens over time: rapid initial decay for new unused edges,
        stabilization for edges that have survived (proving their value).

        The effective decay factor interpolates from `decay_factor` (aggressive,
        for recently-past-grace edges) toward 1.0 (stable, for old survivors).
        """
        now = time.time()
        threshold_seconds = self.decay_threshold_days * 86400
        decayed = 0
        for edge in self.graph.edges:
            last_active = (
                edge.last_strengthened
                if edge.last_strengthened > 0
                else edge.last_decayed
                if edge.last_decayed > 0
                else 0
            )
            age = now - last_active if last_active > 0 else float("inf")
            if age > threshold_seconds:
                age_past_grace_days = (age - threshold_seconds) / 86400
                stabilization = 1.0 - math.exp(
                    -age_past_grace_days / self.half_life_days
                )
                effective_factor = self.decay_factor + (
                    (1.0 - self.decay_factor) * stabilization
                )
                edge.weight = max(0.0, edge.weight * effective_factor)
                edge.last_decayed = now
                decayed += 1
        return {"edges_decayed": decayed}

    def prune(self) -> int:
        to_remove = [e for e in self.graph.edges if e.weight < self.prune_threshold]
        for edge in to_remove:
            self.graph.edges.remove(edge)
            if edge in self.graph.outgoing_edges.get(edge.source_id, []):
                self.graph.outgoing_edges[edge.source_id].remove(edge)
            if edge in self.graph.incoming_edges.get(edge.target_id, []):
                self.graph.incoming_edges[edge.target_id].remove(edge)
            if edge in self.graph.edges_by_type.get(edge.relation_type, []):
                self.graph.edges_by_type[edge.relation_type].remove(edge)
        self.graph.edge_count = len(self.graph.edges)
        return len(to_remove)

    def enforce_in_degree_ceiling(self):
        for node_id in self.graph.nodes:
            incoming = self.graph.incoming_edges.get(node_id, [])
            total = sum(e.weight for e in incoming)
            if total > self.in_degree_weight_ceiling and incoming:
                scale = self.in_degree_weight_ceiling / total
                for e in incoming:
                    e.weight = max(0.0, min(1.0, e.weight * scale))

    def get_exploration_candidates(
        self, active_ids: list[str], n: int = 2
    ) -> list[str]:
        candidates = []
        for node_id in active_ids:
            for edge in self.graph.outgoing_edges.get(node_id, []):
                if edge.weight < self.min_weight_for_influence:
                    target = edge.target_id
                    if target not in active_ids and target not in candidates:
                        candidates.append(target)
        random.shuffle(candidates)
        return candidates[:n]
