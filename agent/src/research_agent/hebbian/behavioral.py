from __future__ import annotations

from .introspection import AgentSelfModel
from .knowledge_graph import KnowledgeGraph


class BehavioralEngine:
    """Computes retrieval primes from personality metrics."""

    def __init__(self, graph: KnowledgeGraph):
        self.graph = graph

    def compute_retrieval_primes(self, model: AgentSelfModel) -> dict[str, float]:
        """Compute per-node retrieval primes from the agent's self-model.

        Returns a dict of {node_id: prime_value} that gets passed
        to RetrievalGate.seed_from_context(primes=...).

        Two signals:
        - Exploitation: boost nodes in strong domains (proficiency-based)
        - Exploration: boost frontier/curiosity nodes (novelty-based)

        The balance between them is governed by novelty_seeking tendency.
        """
        primes: dict[str, float] = {}

        novelty = model.tendencies.novelty_seeking
        exploitation_weight = 1.0 - novelty
        exploration_weight = novelty

        for domain, profile in model.profiles.items():
            if profile.proficiency < 0.2:
                continue

            prime_strength = 0.05 + 0.10 * profile.proficiency
            prime_value = prime_strength * exploitation_weight

            for nid, node in self.graph.nodes.items():
                if node.metadata.get("domain") == domain:
                    primes[nid] = primes.get(nid, 0.0) + prime_value

        curiosity_prime = 0.10 * exploration_weight

        for nid in model.frontier.curiosity_targets:
            if nid in self.graph.nodes:
                primes[nid] = primes.get(nid, 0.0) + curiosity_prime

        isolated_prime = 0.05 * exploration_weight
        for nid in model.frontier.isolated_high_value:
            if nid in self.graph.nodes:
                primes[nid] = primes.get(nid, 0.0) + isolated_prime

        for nid, node in self.graph.nodes.items():
            if node.node_type not in ("methodology", "epistemic"):
                continue
            outgoing = self.graph.outgoing_edges.get(nid, [])
            incoming = self.graph.incoming_edges.get(nid, [])
            all_edges = outgoing + incoming
            if not all_edges:
                continue
            total_success = sum(e.successful_co_activations for e in all_edges)
            avg_weight = sum(e.weight for e in all_edges) / len(all_edges)
            if total_success > 0 and avg_weight > 0.1:
                prime_value = min(0.15, 0.03 * avg_weight * (1 + total_success))
                primes[nid] = primes.get(nid, 0.0) + prime_value

        return primes
