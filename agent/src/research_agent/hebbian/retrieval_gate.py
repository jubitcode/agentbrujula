import math
import time

from .knowledge_graph import KnowledgeGraph
from .working_memory import WorkingMemoryBuffer
from .hebbian_engine import HebbianEdgeEngine
from .similarity import tfidf_most_similar, embedding_most_similar
from .embeddings import EmbeddingProvider


class RetrievalGate:
    def __init__(
        self,
        graph: KnowledgeGraph,
        buffer: WorkingMemoryBuffer,
        hebbian_engine: HebbianEdgeEngine,
        min_hebbian_weight: float = 0.6,
        min_successful_coactivations: int = 2,
        max_pull_tokens: int = 1500,
        content_fallback_k: int = 3,
        staleness_penalty: float = 0.5,
        hebbian_boost_alpha: float = 0.5,
        max_hebbian_only: int = 3,
        min_hebbian_boost: float = 0.10,
        embedding_provider: EmbeddingProvider | None = None,
        tfidf_weight: float = 0.5,
        min_embedding_score: float = 0.3,
        min_retrieval_score: float = 0.35,
        embedding_fallback_k: int | None = None,
    ):
        self.graph = graph
        self.buffer = buffer
        self.hebbian_engine = hebbian_engine
        self.min_hebbian_weight = min_hebbian_weight
        self.min_successful_coactivations = min_successful_coactivations
        self.max_pull_tokens = max_pull_tokens
        self.content_fallback_k = embedding_fallback_k or content_fallback_k
        self.staleness_penalty = staleness_penalty
        self.hebbian_boost_alpha = hebbian_boost_alpha
        self.max_hebbian_only = max_hebbian_only
        self.min_hebbian_boost = min_hebbian_boost
        self.embedding_provider = embedding_provider
        self.tfidf_weight = tfidf_weight
        self.min_embedding_score = min_embedding_score
        self.min_retrieval_score = min_retrieval_score

    def gate(self, dag_node, context=None, context_text: str = "") -> list[str]:
        candidates = set()

        for input_id in dag_node.inputs:
            if input_id in self.graph.nodes:
                candidates.add(input_id)

        if context_text:
            seeded = self.seed_from_context(context_text, top_k=5)
            candidates.update(seeded)

        for active_id in self.buffer.active_nodes:
            strong_edges = self.graph.get_strong_edges(
                active_id, min_weight=self.min_hebbian_weight
            )
            for edge in strong_edges:
                if edge.successful_co_activations >= self.min_successful_coactivations:
                    if edge.target_id in self.graph.nodes:
                        candidates.add(edge.target_id)

        exploration = self.hebbian_engine.get_exploration_candidates(
            list(self.buffer.active_nodes),
            n=1,
        )
        candidates.update(exploration)

        if len(candidates) < 2:
            candidates |= self._content_fallback(dag_node.description)

        return self._rank_and_budget(candidates)

    def seed_from_context(
        self, text: str, top_k: int = 5, primes: dict[str, float] | None = None
    ) -> list[str]:
        """Find seed nodes from free-text context using TF-IDF + Hebbian boost.

        Matches conversation context against all node content/summaries,
        then applies a continuous Hebbian boost from seed neighbors so
        learning influences retrieval without requiring a hard threshold.
        """
        corpus = {
            nid: f"{node.label} {node.summary} {node.content}"
            for nid, node in self.graph.nodes.items()
            if node.node_type != "episode"
            and f"{node.label} {node.summary} {node.content}".strip()
        }
        if not corpus:
            return []

        results = tfidf_most_similar(text, corpus, top_k=top_k)
        tfidf_scores = {nid: score for nid, score in results if score > 0}

        if self.embedding_provider is not None:
            node_embeddings = {
                nid: node.embedding
                for nid, node in self.graph.nodes.items()
                if nid in corpus and node.embedding is not None
            }
            if node_embeddings:
                q_emb = self.embedding_provider.embed(text, for_query=True)
                emb_results = embedding_most_similar(
                    q_emb, node_embeddings, top_k=top_k
                )
                emb_scores = {
                    nid: score
                    for nid, score in emb_results
                    if score >= self.min_embedding_score
                }
                all_ids = set(tfidf_scores) | set(emb_scores)
                blended = {}
                for nid in all_ids:
                    t_score = tfidf_scores.get(nid, 0.0)
                    e_score = emb_scores.get(nid, 0.0)
                    if t_score < 0.05 and e_score < self.min_retrieval_score:
                        continue
                    blended[nid] = (
                        self.tfidf_weight * t_score + (1 - self.tfidf_weight) * e_score
                    )
                tfidf_scores = blended

        hebbian_boost: dict[str, float] = {}
        hebbian_max_path: dict[str, float] = {}
        for seed_id, seed_tfidf in tfidf_scores.items():
            for edge in self.graph.outgoing_edges.get(seed_id, []):
                if edge.target_id in self.graph.nodes:
                    co_act_term = math.log1p(edge.successful_co_activations)
                    boost = seed_tfidf * edge.weight * co_act_term
                    hebbian_boost[edge.target_id] = (
                        hebbian_boost.get(edge.target_id, 0) + boost
                    )
                    hebbian_max_path[edge.target_id] = max(
                        hebbian_max_path.get(edge.target_id, 0), boost
                    )

        combined: dict[str, float] = {}
        hebbian_only: list[tuple[str, float]] = []

        for nid in tfidf_scores:
            combined[nid] = tfidf_scores[
                nid
            ] + self.hebbian_boost_alpha * hebbian_boost.get(nid, 0)

        for nid in hebbian_boost:
            if nid not in tfidf_scores:
                if hebbian_max_path[nid] >= self.min_hebbian_boost:
                    score = self.hebbian_boost_alpha * hebbian_boost[nid]
                    hebbian_only.append((nid, score))

        hebbian_only.sort(key=lambda x: x[1], reverse=True)
        for nid, score in hebbian_only[: self.max_hebbian_only]:
            combined[nid] = score

        if primes:
            for nid, prime_val in primes.items():
                combined[nid] = combined.get(nid, 0) + prime_val

        return self._rank_and_budget(set(combined), score_override=combined)

    def _content_fallback(self, description: str) -> set[str]:
        """TF-IDF (+ optional embedding) similarity between description and graph nodes."""
        corpus = {}
        for nid, node in self.graph.nodes.items():
            if nid not in self.buffer.active_nodes:
                corpus[nid] = f"{node.summary} {node.content}"

        if not corpus:
            return set()

        tfidf_results = tfidf_most_similar(
            description, corpus, top_k=self.content_fallback_k
        )
        tfidf_scores = {nid: score for nid, score in tfidf_results if score > 0.1}

        if self.embedding_provider is not None:
            node_embeddings = {
                nid: node.embedding
                for nid, node in self.graph.nodes.items()
                if nid in corpus and node.embedding is not None
            }
            if node_embeddings:
                q_emb = self.embedding_provider.embed(description, for_query=True)
                emb_results = embedding_most_similar(
                    q_emb, node_embeddings, top_k=self.content_fallback_k
                )
                emb_scores = {
                    nid: score
                    for nid, score in emb_results
                    if score >= self.min_embedding_score
                }
                all_ids = set(tfidf_scores) | set(emb_scores)
                blended = {}
                for nid in all_ids:
                    t_score = tfidf_scores.get(nid, 0.0)
                    e_score = emb_scores.get(nid, 0.0)
                    if t_score < 0.05 and e_score < self.min_retrieval_score:
                        continue
                    blended[nid] = (
                        self.tfidf_weight * t_score + (1 - self.tfidf_weight) * e_score
                    )
                return {nid for nid, score in blended.items() if score > 0.1}

        return set(tfidf_scores)

    def _rank_and_budget(
        self, candidate_ids: set[str], score_override: dict[str, float] | None = None
    ) -> list[str]:
        now = time.time()
        scored = []
        for nid in candidate_ids:
            node = self.graph.get_node(nid)
            if node:
                effective_importance = node.importance
                if 0 < node.valid_until < now:
                    effective_importance *= self.staleness_penalty
                if score_override and nid in score_override:
                    rank_score = score_override[nid] * effective_importance
                else:
                    rank_score = effective_importance
                scored.append((nid, rank_score))
        scored.sort(key=lambda x: x[1], reverse=True)

        result = []
        total_tokens = 0
        for nid, _ in scored:
            node = self.graph.get_node(nid)
            if node:
                token_est = max(1, len(node.content or node.summary or node.label) // 4)
                if total_tokens + token_est > self.max_pull_tokens:
                    break
                result.append(nid)
                total_tokens += token_est
        return result
