from __future__ import annotations

import json
import math
import re
import time
import uuid
from collections import Counter

from .knowledge_graph import KnowledgeGraph, RelationType
from .loader import KnowledgeLoader
from .hebbian_engine import HebbianEdgeEngine
from .similarity import tfidf_most_similar


def _tfidf_similarity(text_a: str, text_b: str, corpus: list[str]) -> float:
    """TF-IDF cosine similarity between two texts given a corpus for IDF.

    Uses smoothed IDF: log(1 + n_docs / (1 + df)) to avoid zero-ing out
    terms that appear in every document.
    """

    def _tokenize(text):
        return re.findall(r"[a-z][a-z0-9]+", text.lower())

    tokens_a = _tokenize(text_a)
    tokens_b = _tokenize(text_b)
    if not tokens_a or not tokens_b:
        return 0.0

    n_docs = len(corpus) + 2
    df: Counter = Counter()
    for doc in corpus:
        for term in set(_tokenize(doc)):
            df[term] += 1
    for term in set(tokens_a):
        df[term] += 1
    for term in set(tokens_b):
        df[term] += 1

    def _vec(tokens):
        tf = Counter(tokens)
        return {
            t: (c / len(tokens)) * math.log(1 + n_docs / (1 + df[t]))
            for t, c in tf.items()
            if df.get(t, 0) > 0
        }

    vec_a = _vec(tokens_a)
    vec_b = _vec(tokens_b)
    if not vec_a or not vec_b:
        return 0.0

    dot = sum(vec_a.get(t, 0) * v for t, v in vec_b.items())
    norm_a = math.sqrt(sum(v * v for v in vec_a.values()))
    norm_b = math.sqrt(sum(v * v for v in vec_b.values()))
    if norm_a == 0 or norm_b == 0:
        return 0.0
    return dot / (norm_a * norm_b)


_CONTRADICTION_PROMPT = """\
You are checking if a new piece of knowledge contradicts an existing one.

## Existing knowledge:
{existing_content}

## New knowledge:
{new_content}

Does the new knowledge CONTRADICT the existing knowledge?
A contradiction means they cannot both be true at the same time.
Minor differences or additions are NOT contradictions.

Respond with ONLY valid JSON:
{{"contradicts": true/false, "reason": "brief explanation"}}"""


_CONSOLIDATION_PROMPT = """\
You have multiple related memory episodes from conversations.
Consolidate them into a single higher-level fact.

## Episodes:
{episodes}

## Instructions:
Produce a single consolidated fact that captures the essential pattern
across these episodes. Return ONLY valid JSON:
{{
  "label": "Short title for the consolidated fact",
  "content": "The consolidated knowledge in 1-3 sentences",
  "summary": "Ultra-short summary (under 10 words)",
  "importance": 0.7
}}"""


class ConversationMemory:
    """Manages conversation-derived knowledge in the Hebbian graph.

    Turns conversation history into episode nodes, detects contradictions,
    handles corrections as strong learning signals, and consolidates
    repeated patterns into higher-level facts.
    """

    def __init__(
        self,
        graph: KnowledgeGraph,
        loader: KnowledgeLoader,
        engine: HebbianEdgeEngine,
        llm_fn=None,
        domain: str = "Conversation",
    ):
        self.graph = graph
        self.loader = loader
        self.engine = engine
        self.llm_fn = llm_fn
        self.domain = domain
        self._session_episodes: list[str] = []

    def ingest_turn(
        self,
        role: str,
        content: str,
        session_id: str | None = None,
        importance: float = 0.4,
    ) -> str:
        """Create an episode node from a conversation turn.

        Args:
            role: "user" or "assistant"
            content: The message content.
            session_id: Optional session identifier for grouping.
            importance: Base importance (user messages get +0.1 boost).

        Returns:
            The created episode node ID.
        """
        if role == "user":
            importance = min(1.0, importance + 0.1)

        episode_id = f"ep_{uuid.uuid4().hex[:8]}"
        summary = content[:80].replace("\n", " ").strip()
        if len(content) > 80:
            summary += "..."

        node = self.loader.add_node(
            graph=self.graph,
            node_id=episode_id,
            label=f"[{role}] {summary[:40]}",
            content=content,
            domain=self.domain,
            node_type="episode",
            summary=summary,
            importance=importance,
        )

        if session_id:
            node.metadata["session_id"] = session_id
        node.metadata["role"] = role
        node.metadata["timestamp"] = time.time()

        if self._session_episodes:
            prev_id = self._session_episodes[-1]
            if self.graph.get_node(prev_id):
                self.graph.add_edge(
                    prev_id, episode_id, RelationType.PRECEDES, weight=0.5
                )

        self._session_episodes.append(episode_id)
        return episode_id

    def detect_contradictions(
        self,
        new_content: str,
        candidate_ids: list[str] | None = None,
        similarity_threshold: float = 0.3,
    ) -> list[dict]:
        """Check if new content contradicts existing nodes.

        Uses TF-IDF to find similar nodes, then LLM to confirm contradiction.

        Args:
            new_content: The new knowledge to check.
            candidate_ids: Optional whitelist of node IDs to check against.
                If None, checks all non-episode nodes.
            similarity_threshold: Min TF-IDF similarity to consider as candidate.

        Returns:
            List of {"node_id": str, "reason": str} for confirmed contradictions.
        """
        if not self.llm_fn:
            return []

        corpus_nodes = {}
        for nid, node in self.graph.nodes.items():
            if node.node_type == "episode":
                continue
            if candidate_ids and nid not in candidate_ids:
                continue
            text = f"{node.summary} {node.content}"
            if text.strip():
                corpus_nodes[nid] = text

        if not corpus_nodes:
            return []

        similar = []
        if candidate_ids:
            similar = [(nid, 1.0) for nid in corpus_nodes]
        else:
            results = tfidf_most_similar(new_content, corpus_nodes, top_k=5)
            similar = [
                (nid, score) for nid, score in results if score >= similarity_threshold
            ]

        contradictions = []
        for nid, _ in similar[:5]:
            node = self.graph.get_node(nid)
            if not node:
                continue

            prompt = _CONTRADICTION_PROMPT.format(
                existing_content=node.content,
                new_content=new_content,
            )

            try:
                raw = self.llm_fn(prompt)
                cleaned = raw.strip()
                cleaned = re.sub(r"^```(?:json)?\s*", "", cleaned)
                cleaned = re.sub(r"\s*```$", "", cleaned)
                result = json.loads(cleaned)
                if result.get("contradicts"):
                    contradictions.append(
                        {
                            "node_id": nid,
                            "reason": result.get("reason", ""),
                        }
                    )
            except (json.JSONDecodeError, Exception):
                continue

        return contradictions

    def mark_contradictions(
        self, new_node_id: str, contradicted_ids: list[str]
    ) -> None:
        """Create CONTRADICTS edges and mark old nodes stale."""
        now = time.time()
        for old_id in contradicted_ids:
            old_node = self.graph.get_node(old_id)
            if not old_node:
                continue
            if self.graph.get_node(new_node_id):
                self.graph.add_edge(
                    new_node_id, old_id, RelationType.CONTRADICTS, weight=0.8
                )
            if old_node.valid_until == 0.0:
                old_node.valid_until = now + 7 * 86400
            old_node.importance = max(0.1, old_node.importance * 0.5)

    def record_correction(
        self,
        wrong_node_id: str,
        corrected_content: str,
        context_node_ids: list[str] | None = None,
    ) -> str:
        """Record a user correction — the strongest learning signal.

        Creates a new high-importance node with the corrected fact,
        marks the old node stale, and strengthens edges to context.

        Args:
            wrong_node_id: ID of the node that was wrong.
            corrected_content: The correct information.
            context_node_ids: Other nodes active during the correction.

        Returns:
            The new corrected node ID.
        """
        old_node = self.graph.get_node(wrong_node_id)

        correction_id = f"correction_{uuid.uuid4().hex[:8]}"
        summary = corrected_content[:80].replace("\n", " ").strip()

        edges = []
        if old_node:
            edges.append(("CONTRADICTS", wrong_node_id))
        if context_node_ids:
            for cid in context_node_ids:
                if cid != wrong_node_id and self.graph.get_node(cid):
                    edges.append(("RELATED_TO", cid))

        self.loader.add_node(
            graph=self.graph,
            node_id=correction_id,
            label=f"Correction: {summary[:40]}",
            content=corrected_content,
            domain=self.domain,
            node_type="fact",
            summary=summary,
            importance=0.9,
            edges=edges if edges else None,
        )

        if old_node:
            old_node.valid_until = time.time()
            old_node.importance = max(0.1, old_node.importance * 0.3)

        if context_node_ids:
            co_activated = [correction_id] + [
                cid
                for cid in context_node_ids
                if cid != wrong_node_id and self.graph.get_node(cid)
            ]
            if len(co_activated) >= 2:
                self.engine.strengthen(co_activated, task_success=True)

        return correction_id

    def consolidate(
        self,
        min_similarity: float = 0.4,
        min_group_size: int = 2,
        max_group_size: int = 5,
    ) -> list[str]:
        """Consolidate similar episode nodes into higher-level facts.

        Groups episodes by TF-IDF similarity, asks LLM to summarize each
        group, and creates consolidated fact nodes. Original episodes get
        reduced importance.

        Returns:
            List of newly created consolidated node IDs.
        """
        if not self.llm_fn:
            return []

        episodes = []
        for nid, node in self.graph.nodes.items():
            if node.node_type == "episode":
                episodes.append((nid, f"{node.summary} {node.content}"))

        if len(episodes) < min_group_size:
            return []

        corpus = [text for _, text in episodes]
        used = set()
        groups: list[list[str]] = []

        for _, (nid_i, text_i) in enumerate(episodes):
            if nid_i in used:
                continue
            group = [nid_i]
            used.add(nid_i)

            for _, (nid_j, text_j) in enumerate(episodes):
                if len(group) >= max_group_size:
                    break
                if nid_j in used:
                    continue
                sim = _tfidf_similarity(text_i, text_j, corpus)
                if sim >= min_similarity:
                    group.append(nid_j)
                    used.add(nid_j)

            if len(group) >= min_group_size:
                groups.append(group)

        consolidated_ids = []
        for group in groups:
            episode_texts = []
            for nid in group:
                node = self.graph.get_node(nid)
                if node:
                    role = node.metadata.get("role", "unknown")
                    episode_texts.append(f"[{role}] {node.content}")

            prompt = _CONSOLIDATION_PROMPT.format(
                episodes="\n---\n".join(episode_texts)
            )

            try:
                raw = self.llm_fn(prompt)
                cleaned = raw.strip()
                cleaned = re.sub(r"^```(?:json)?\s*", "", cleaned)
                cleaned = re.sub(r"\s*```$", "", cleaned)
                result = json.loads(cleaned)
            except (json.JSONDecodeError, Exception):
                continue

            cons_id = f"consolidated_{uuid.uuid4().hex[:8]}"
            edges = [("REFINES", nid) for nid in group if self.graph.get_node(nid)]

            self.loader.add_node(
                graph=self.graph,
                node_id=cons_id,
                label=result.get("label", f"Consolidated from {len(group)} episodes"),
                content=result.get("content", ""),
                domain=self.domain,
                node_type="fact",
                summary=result.get("summary", ""),
                importance=float(result.get("importance", 0.7)),
                edges=edges if edges else None,
            )

            for nid in group:
                node = self.graph.get_node(nid)
                if node:
                    node.importance = max(0.1, node.importance * 0.3)

            consolidated_ids.append(cons_id)

        return consolidated_ids

    def get_session_episodes(self) -> list[str]:
        """Return episode IDs from the current session in order."""
        return list(self._session_episodes)

    def reset_session(self) -> None:
        """Clear session episode tracking (graph nodes remain)."""
        self._session_episodes = []
