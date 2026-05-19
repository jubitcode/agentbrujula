from __future__ import annotations

import json
import re

from .knowledge_graph import KnowledgeGraph
from .loader import KnowledgeLoader
from .similarity import tfidf_most_similar

_EXTRACTION_PROMPT = """\
You just answered a question using knowledge from a graph database.
Your task: identify NEW atomic facts from your answer that are NOT
already covered by the existing knowledge nodes listed below.

## Existing knowledge (already in graph):
{existing_summaries}

## Question asked:
{question}

## Answer produced:
{answer}

## Instructions:
Extract 0-3 new atomic facts from the answer that add knowledge NOT
already captured above. Each fact should be a single concept.

If the answer only restates existing knowledge, return an empty list.

Return ONLY valid JSON (no markdown fences):
[
  {{
    "id": "snake_case_id",
    "label": "Short Title",
    "content": "The atomic fact in 1-3 sentences.",
    "summary": "Ultra-short summary (under 10 words)",
    "node_type": "fact",
    "importance": 0.5,
    "edges": [["RELATED_TO", "existing_node_id"]]
  }}
]

Return [] if no genuinely new knowledge was produced."""


class KnowledgeExtractor:
    def __init__(
        self,
        graph: KnowledgeGraph,
        loader: KnowledgeLoader,
        llm_fn,
        domain_default: str = "Learned",
        dedup_threshold: float = 0.7,
    ):
        self.graph = graph
        self.loader = loader
        self.llm_fn = llm_fn
        self.domain_default = domain_default
        self.dedup_threshold = dedup_threshold
        self._corpus_cache: dict[str, str] | None = None
        self._corpus_node_count: int = 0

    def extract(
        self,
        question: str,
        answer: str,
        pulled_node_ids: list[str],
        domain: str | None = None,
    ) -> list[str]:
        existing_summaries = self._build_existing_summary(pulled_node_ids)

        prompt = _EXTRACTION_PROMPT.format(
            existing_summaries=existing_summaries,
            question=question,
            answer=answer,
        )

        raw_response = self.llm_fn(prompt)
        extractions = self._parse_response(raw_response)
        if not extractions:
            return []

        target_domain = domain or self.domain_default
        new_ids = []

        for entry in extractions:
            node_id = entry.get("id", "")
            if not node_id or node_id in self.graph.nodes:
                continue

            candidate_text = self._node_text(entry)
            duplicate_of = self._find_semantic_duplicate(candidate_text)
            if duplicate_of:
                continue

            edges = []
            for edge_def in entry.get("edges", []):
                if isinstance(edge_def, list) and len(edge_def) == 2:
                    rel_str, target_id = edge_def
                    if target_id in self.graph.nodes:
                        edges.append((rel_str, target_id))

            candidate_text_lower = candidate_text.lower()
            connected_targets = {e[1] for e in edges}
            for pulled_id in pulled_node_ids:
                if pulled_id in connected_targets or pulled_id == node_id:
                    continue
                pulled_node = self.graph.get_node(pulled_id)
                if pulled_node and (
                    pulled_node.label.lower() in candidate_text_lower
                    or any(
                        w in candidate_text_lower
                        for w in pulled_node.label.lower().split()
                        if len(w) > 3
                    )
                ):
                    edges.append(("RELATED_TO", pulled_id))

            self.loader.add_node(
                graph=self.graph,
                node_id=node_id,
                label=entry.get("label", node_id),
                content=entry.get("content", ""),
                domain=target_domain,
                node_type=entry.get("node_type", "fact"),
                summary=entry.get("summary", entry.get("label", node_id)),
                importance=float(entry.get("importance", 0.5)),
                edges=edges if edges else None,
            )
            new_ids.append(node_id)
            self._corpus_cache = None

        return new_ids

    def _build_existing_summary(self, node_ids: list[str]) -> str:
        lines = []
        for nid in node_ids:
            node = self.graph.get_node(nid)
            if node:
                lines.append(f"- [{nid}] {node.summary or node.label}")
        return "\n".join(lines) if lines else "(no existing nodes)"

    def _parse_response(self, raw: str) -> list[dict]:
        cleaned = raw.strip()
        cleaned = re.sub(r"^```(?:json)?\s*", "", cleaned)
        cleaned = re.sub(r"\s*```$", "", cleaned)
        cleaned = cleaned.strip()

        try:
            parsed = json.loads(cleaned)
        except json.JSONDecodeError:
            return []

        if not isinstance(parsed, list):
            return []

        valid = []
        for entry in parsed:
            if not isinstance(entry, dict):
                continue
            if "id" in entry and "content" in entry:
                valid.append(entry)
        return valid

    @staticmethod
    def _node_text(entry: dict) -> str:
        return f"{entry.get('summary', '')} {entry.get('content', '')}"

    def _get_corpus(self) -> dict[str, str]:
        """Return cached corpus, rebuilding only when node count changes."""
        if self._corpus_cache is not None and self._corpus_node_count == len(
            self.graph.nodes
        ):
            return self._corpus_cache
        self._corpus_cache = {
            nid: f"{node.summary} {node.content}"
            for nid, node in self.graph.nodes.items()
        }
        self._corpus_node_count = len(self.graph.nodes)
        return self._corpus_cache

    def _find_semantic_duplicate(self, candidate_text: str) -> str | None:
        if not self.graph.nodes:
            return None

        corpus = self._get_corpus()
        results = tfidf_most_similar(candidate_text, corpus, top_k=1)
        if results and results[0][1] >= self.dedup_threshold:
            return results[0][0]
        return None
