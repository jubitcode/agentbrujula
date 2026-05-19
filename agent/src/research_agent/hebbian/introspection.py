from __future__ import annotations

import re
import time
from collections import defaultdict
from dataclasses import dataclass, field
from itertools import combinations

from .knowledge_graph import KnowledgeGraph, GraphEdge


_TASK_PATTERNS: dict[str, list[str]] = {
    "comparison": [
        r"\bcompar[ei]",
        r"\bvs\.?\b",
        r"\bversus\b",
        r"\bdifference\b",
        r"\bbetter\b",
        r"\bworse\b",
        r"\btradeoff\b",
        r"\btrade-off\b",
        r"\badvantage\b",
        r"\bdisadvantage\b",
        r"\bpros?\b.*\bcons?\b",
    ],
    "mechanism": [
        r"\bhow does\b",
        r"\bhow do\b",
        r"\bwork[s]?\b",
        r"\bmechanism\b",
        r"\bunder the hood\b",
        r"\binternals?\b",
        r"\bimplement",
    ],
    "definition": [
        r"\bwhat is\b",
        r"\bwhat are\b",
        r"\bdefin[ei]",
        r"\bmean[s]?\b",
        r"\bexplain\b.*\bconcept\b",
    ],
    "survey": [
        r"\boverview\b",
        r"\bsurvey\b",
        r"\blandscape\b",
        r"\bstate of\b",
        r"\bapproaches to\b",
        r"\bmethods for\b",
        r"\btechniques\b",
    ],
    "application": [
        r"\bhow to\b",
        r"\bhow can\b",
        r"\bappl[yi]",
        r"\buse case\b",
        r"\bin practice\b",
        r"\bimplement\b",
        r"\bbuild\b",
    ],
    "synthesis": [
        r"\bconnect\b",
        r"\brelat[ei]",
        r"\bcombine\b",
        r"\bintegrat[ei]",
        r"\bsynthesize\b",
        r"\binsight\b",
        r"\bimplication\b",
    ],
}


def classify_task_type(question: str) -> str:
    """Classify a question into a task type using keyword patterns.

    Returns one of: comparison, mechanism, definition, survey,
    application, synthesis, or general (fallback).
    """
    q = question.lower()
    scores: dict[str, int] = defaultdict(int)
    for task_type, patterns in _TASK_PATTERNS.items():
        for pattern in patterns:
            if re.search(pattern, q):
                scores[task_type] += 1
    if not scores:
        return "general"
    return max(scores, key=scores.get)


@dataclass
class DomainProfile:
    domain: str
    node_count: int = 0
    edge_count: int = 0  # intra-domain edges
    cross_domain_edges: int = 0  # edges connecting to other domains
    density: float = 0.0
    avg_weight: float = 0.0
    success_rate: float = 0.0
    depth: int = 0
    recency: float = 0.0
    coverage: float = 0.0  # fraction of total graph
    active_methodologies: list[str] = field(
        default_factory=list
    )  # methodology nodes co-activated with this domain

    @property
    def proficiency(self) -> float:
        """Composite proficiency score (0-1).

        Includes cross-domain connectivity as a signal of
        integrated understanding, not just internal density.
        """
        cross_domain_bonus = min(0.2, self.cross_domain_edges * 0.02)
        return min(
            1.0,
            (
                0.25 * self.density
                + 0.25 * self.avg_weight
                + 0.2 * self.success_rate
                + 0.15 * min(1.0, self.depth / 5.0)
                + 0.15 * cross_domain_bonus / 0.2  # normalize to 0-1
            ),
        )


@dataclass
class AgentTendencies:
    depth_vs_breadth: float = 0.0  # -1 (pure breadth) to +1 (pure depth)
    theoretical_vs_practical: float = 0.0  # -1 (practical) to +1 (theoretical)
    confidence_calibration: float = 0.5  # 0 (underconfident) to 1 (overconfident)
    novelty_seeking: float = 0.5  # 0 (conservative) to 1 (novelty-seeking)


@dataclass
class KnowledgeFrontier:
    isolated_high_value: list[str] = field(default_factory=list)
    weak_bridges: list[tuple[str, str, float]] = field(default_factory=list)
    curiosity_targets: list[str] = field(default_factory=list)


@dataclass
class SkillEntry:
    task_type: str
    domain: str
    successes: int = 0
    total: int = 0

    @property
    def success_rate(self) -> float:
        return self.successes / max(1, self.total)


@dataclass
class AgentSelfModel:
    profiles: dict[str, DomainProfile] = field(default_factory=dict)
    tendencies: AgentTendencies = field(default_factory=AgentTendencies)
    frontier: KnowledgeFrontier = field(default_factory=KnowledgeFrontier)
    skills: list[SkillEntry] = field(default_factory=list)
    top_domains: list[str] = field(default_factory=list)
    weakest_domains: list[str] = field(default_factory=list)
    total_nodes: int = 0
    total_edges: int = 0
    avg_edge_weight: float = 0.0
    computed_at: float = 0.0


class IntrospectionEngine:
    """Computes personality metrics from graph topology."""

    def __init__(self, graph: KnowledgeGraph):
        self.graph = graph
        self._cache: AgentSelfModel | None = None
        self._cache_edge_count: int = -1

    def _is_stale(self) -> bool:
        return self._cache is None or self._cache_edge_count != self.graph.edge_count

    def compute_self_model(self) -> AgentSelfModel:
        """Compute the full agent self-model. Cached until graph mutates."""
        if not self._is_stale():
            return self._cache

        profiles = self.compute_all_profiles()
        tendencies = self.compute_tendencies()
        frontier = self.compute_knowledge_frontier()
        skills = self.compute_skill_matrix()

        sorted_domains = sorted(
            profiles.values(),
            key=lambda p: p.proficiency,
            reverse=True,
        )
        top = [p.domain for p in sorted_domains[:3] if p.proficiency > 0.1]
        weak = [
            p.domain
            for p in sorted_domains[-3:]
            if p.node_count > 0 and p.proficiency < 0.5
        ]

        weights = [e.weight for e in self.graph.edges]

        model = AgentSelfModel(
            profiles=profiles,
            tendencies=tendencies,
            frontier=frontier,
            skills=skills,
            top_domains=top,
            weakest_domains=weak,
            total_nodes=len(self.graph.nodes),
            total_edges=self.graph.edge_count,
            avg_edge_weight=sum(weights) / max(1, len(weights)),
            computed_at=time.time(),
        )
        self._cache = model
        self._cache_edge_count = self.graph.edge_count
        return model

    def compute_domain_profile(self, domain: str) -> DomainProfile:
        """Compute profile for a single domain."""
        node_ids = {
            nid
            for nid, node in self.graph.nodes.items()
            if node.metadata.get("domain") == domain
        }
        if not node_ids:
            return DomainProfile(domain=domain)

        # Intra-domain edges
        domain_edges = [
            e
            for e in self.graph.edges
            if e.source_id in node_ids and e.target_id in node_ids
        ]

        # Cross-domain edges (connecting this domain to others)
        cross_edges = [
            e
            for e in self.graph.edges
            if (e.source_id in node_ids) != (e.target_id in node_ids)
            and (e.source_id in node_ids or e.target_id in node_ids)
            and e.successful_co_activations > 0
        ]

        # Active methodologies: methodology/epistemic nodes connected to this domain
        methodology_ids = set()
        for e in cross_edges:
            other_id = e.target_id if e.source_id in node_ids else e.source_id
            other_node = self.graph.nodes.get(other_id)
            if other_node and other_node.node_type in ("methodology", "epistemic"):
                methodology_ids.add(other_id)

        # Use ALL edges (intra + cross) for weight and success metrics
        all_relevant_edges = domain_edges + cross_edges

        n = len(node_ids)
        max_edges = n * (n - 1) if n > 1 else 1
        density = len(domain_edges) / max_edges

        weights = [e.weight for e in all_relevant_edges]
        avg_weight = sum(weights) / max(1, len(weights))

        total_coact = sum(e.co_activations for e in all_relevant_edges)
        total_success = sum(e.successful_co_activations for e in all_relevant_edges)
        success_rate = total_success / max(1, total_coact)

        depth = self._longest_strong_chain(node_ids, domain_edges)

        strengthened_times = [
            e.last_strengthened for e in all_relevant_edges if e.last_strengthened > 0
        ]
        recency = max(strengthened_times) if strengthened_times else 0.0

        return DomainProfile(
            domain=domain,
            node_count=n,
            edge_count=len(domain_edges),
            cross_domain_edges=len(cross_edges),
            density=density,
            avg_weight=avg_weight,
            success_rate=success_rate,
            depth=depth,
            recency=recency,
            coverage=n / max(1, len(self.graph.nodes)),
            active_methodologies=[
                self.graph.nodes[mid].label
                for mid in methodology_ids
                if mid in self.graph.nodes
            ],
        )

    def compute_all_profiles(self) -> dict[str, DomainProfile]:
        """Compute profiles for all domains in the graph."""
        domains = set()
        for node in self.graph.nodes.values():
            d = node.metadata.get("domain")
            if d:
                domains.add(d)
        return {d: self.compute_domain_profile(d) for d in domains}

    def compute_tendencies(self) -> AgentTendencies:
        """Compute behavioral tendencies from graph topology."""
        strong_edges = [
            e
            for e in self.graph.edges
            if e.weight > 0.3 and e.successful_co_activations > 0
        ]

        if not strong_edges:
            return AgentTendencies()

        # depth_vs_breadth: chain length vs fan-out
        depth_vs_breadth = self._compute_depth_vs_breadth(strong_edges)

        # theoretical_vs_practical from node types
        theoretical_vs_practical = self._compute_theoretical_practical()

        # confidence_calibration: edge weight vs success rate
        confidence_calibration = self._compute_confidence_calibration(strong_edges)

        # novelty_seeking: edges to recent nodes vs total
        novelty_seeking = self._compute_novelty_seeking()

        return AgentTendencies(
            depth_vs_breadth=depth_vs_breadth,
            theoretical_vs_practical=theoretical_vs_practical,
            confidence_calibration=confidence_calibration,
            novelty_seeking=novelty_seeking,
        )

    def _compute_depth_vs_breadth(self, strong_edges: list[GraphEdge]) -> float:
        """Ratio of chain depth to fan-out. Positive = depth-oriented."""
        # Fan-out: avg number of strong outgoing edges per node
        out_counts: dict[str, int] = defaultdict(int)
        for e in strong_edges:
            out_counts[e.source_id] += 1

        if not out_counts:
            return 0.0

        avg_fanout = sum(out_counts.values()) / len(out_counts)

        # Depth: avg longest chain from each active node
        active_nodes = set(out_counts.keys())
        all_node_ids = set(self.graph.nodes.keys())
        max_chain = self._longest_strong_chain(
            active_nodes | all_node_ids, strong_edges
        )

        # Normalize: fanout 1 = neutral, higher = breadth
        # depth 1 = neutral, higher = depth
        if avg_fanout <= 0:
            return 0.0
        ratio = max_chain / avg_fanout
        # Map to [-1, 1]: ratio > 1 = depth, ratio < 1 = breadth
        return max(-1.0, min(1.0, (ratio - 1.0) / 2.0))

    def _compute_theoretical_practical(self) -> float:
        """Derive from node types and domains."""
        theoretical_types = {"epistemic", "concept", "theory"}
        practical_types = {"methodology", "application", "tool", "implementation"}

        t_count = sum(
            1 for n in self.graph.nodes.values() if n.node_type in theoretical_types
        )
        p_count = sum(
            1 for n in self.graph.nodes.values() if n.node_type in practical_types
        )

        total = t_count + p_count
        if total == 0:
            return 0.0
        return (t_count - p_count) / total

    def _compute_confidence_calibration(self, strong_edges: list[GraphEdge]) -> float:
        """Compare edge weights against actual success rates."""
        if not strong_edges:
            return 0.5

        deltas = []
        for e in strong_edges:
            if e.co_activations > 0:
                actual_success = e.successful_co_activations / e.co_activations
                deltas.append(e.weight - actual_success)

        if not deltas:
            return 0.5

        # Positive avg delta = overconfident (weights > success rate)
        avg_delta = sum(deltas) / len(deltas)
        return max(0.0, min(1.0, 0.5 + avg_delta))

    def _compute_novelty_seeking(self) -> float:
        """Ratio of edges to recently-created nodes vs total."""
        if not self.graph.edges:
            return 0.5

        now = time.time()
        recency_threshold = now - (30 * 86400)  # 30 days

        recent_nodes = {
            nid
            for nid, node in self.graph.nodes.items()
            if node.created_at > recency_threshold
        }

        if not recent_nodes:
            return 0.3  # No recent nodes = mildly conservative

        edges_to_recent = sum(
            1
            for e in self.graph.edges
            if e.target_id in recent_nodes or e.source_id in recent_nodes
        )

        ratio = edges_to_recent / len(self.graph.edges)
        return min(1.0, ratio * 2)  # Scale so 50% recent edges = 1.0

    def compute_knowledge_frontier(self) -> KnowledgeFrontier:
        """Identify gaps, bridges, and curiosity targets."""
        # Isolated high-value: importance > 0.6, few strong edges
        isolated = []
        for nid, node in self.graph.nodes.items():
            if node.importance < 0.6:
                continue
            outgoing = self.graph.outgoing_edges.get(nid, [])
            strong_out = [e for e in outgoing if e.weight > 0.3]
            if len(strong_out) <= 1:
                isolated.append(nid)

        # Weak bridges: cross-domain edges with low avg weight
        domains_map: dict[str, set[str]] = defaultdict(set)
        for nid, node in self.graph.nodes.items():
            d = node.metadata.get("domain")
            if d:
                domains_map[d].add(nid)

        weak_bridges = []
        domain_list = list(domains_map.keys())
        for d1, d2 in combinations(domain_list, 2):
            cross_edges = [
                e
                for e in self.graph.edges
                if (e.source_id in domains_map[d1] and e.target_id in domains_map[d2])
                or (e.source_id in domains_map[d2] and e.target_id in domains_map[d1])
            ]
            if cross_edges:
                avg_w = sum(e.weight for e in cross_edges) / len(cross_edges)
                if avg_w < 0.3:
                    weak_bridges.append((d1, d2, avg_w))

        # Curiosity targets: nodes adjacent to strong clusters but weakly connected
        curiosity = []
        for nid, node in self.graph.nodes.items():
            incoming = self.graph.incoming_edges.get(nid, [])
            if not incoming:
                continue
            strong_in = [e for e in incoming if e.weight > 0.5]
            weak_in = [e for e in incoming if e.weight < 0.2]
            if len(weak_in) > len(strong_in) and len(weak_in) >= 2:
                curiosity.append(nid)

        return KnowledgeFrontier(
            isolated_high_value=isolated[:10],
            weak_bridges=sorted(weak_bridges, key=lambda x: x[2])[:5],
            curiosity_targets=curiosity[:10],
        )

    def compute_skill_matrix(self) -> list[SkillEntry]:
        """Aggregate success_by_task_type across edges into domain x task_type matrix."""
        # Build domain lookup
        node_domain: dict[str, str] = {}
        for nid, node in self.graph.nodes.items():
            node_domain[nid] = node.metadata.get("domain", "unknown")

        # Aggregate
        matrix: dict[tuple[str, str], list[int]] = defaultdict(lambda: [0, 0])
        for edge in self.graph.edges:
            # Use source domain as the skill domain
            domain = node_domain.get(edge.source_id, "unknown")
            for task_type, counts in edge.success_by_task_type.items():
                if isinstance(counts, list) and len(counts) >= 2:
                    key = (domain, task_type)
                    matrix[key][0] += counts[0]
                    matrix[key][1] += counts[1]

        return [
            SkillEntry(
                task_type=task_type,
                domain=domain,
                successes=counts[0],
                total=counts[1],
            )
            for (domain, task_type), counts in sorted(matrix.items())
            if counts[1] > 0
        ]

    @staticmethod
    def _longest_strong_chain(
        node_ids: set[str], edges: list[GraphEdge], min_weight: float = 0.3
    ) -> int:
        """BFS/DFS to find longest chain of strong edges within a node set."""
        adj: dict[str, list[str]] = defaultdict(list)
        for e in edges:
            if (
                e.weight >= min_weight
                and e.source_id in node_ids
                and e.target_id in node_ids
            ):
                adj[e.source_id].append(e.target_id)

        if not adj:
            return 0

        max_depth = 0
        for start in adj:
            # BFS for longest path from this start
            visited = {start}
            queue = [(start, 1)]
            while queue:
                current, depth = queue.pop(0)
                max_depth = max(max_depth, depth)
                for neighbor in adj.get(current, []):
                    if neighbor not in visited:
                        visited.add(neighbor)
                        queue.append((neighbor, depth + 1))
        return max_depth
