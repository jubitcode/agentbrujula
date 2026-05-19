"""Adapter layer — wraps hebbian modules for the research agent."""

from __future__ import annotations

import time
from pathlib import Path

from openai import OpenAI

from .hebbian.knowledge_graph import KnowledgeGraph
from .hebbian.hebbian_engine import HebbianEdgeEngine
from .hebbian.retrieval_gate import RetrievalGate
from .hebbian.working_memory import WorkingMemoryBuffer
from .hebbian.extractor import KnowledgeExtractor
from .hebbian.persistence import GraphPersistence
from .hebbian.loader import KnowledgeLoader


DEFAULT_DB = str(Path.home() / ".research_agent" / "knowledge.db")


class ResearchKnowledgeStore:
    def __init__(
        self,
        db_path: str = DEFAULT_DB,
        llm_base_url: str | None = None,
        llm_api_key: str | None = None,
        llm_model: str | None = None,
        import_path: str | None = None,
    ):
        Path(db_path).parent.mkdir(parents=True, exist_ok=True)

        self._db_path = db_path
        self._persistence = GraphPersistence(db_path)

        if import_path:
            self.graph, self._import_metadata = GraphPersistence.import_graph(
                import_path
            )
        else:
            self._import_metadata = {}
            try:
                self.graph = self._persistence.load_graph()
            except Exception:
                self.graph = KnowledgeGraph()

        self._engine = HebbianEdgeEngine(self.graph)
        self._buffer = WorkingMemoryBuffer(self.graph, token_limit=2000)
        self._gate = RetrievalGate(
            self.graph,
            self._buffer,
            self._engine,
            max_pull_tokens=1500,
        )
        self._loader = KnowledgeLoader()

        self._llm_client: OpenAI | None = None
        self._llm_model = llm_model
        if llm_base_url and llm_api_key:
            self._llm_client = OpenAI(base_url=llm_base_url, api_key=llm_api_key)

        self._extractor = KnowledgeExtractor(
            self.graph,
            self._loader,
            llm_fn=self._llm_call,
        )

    def _llm_call(self, prompt: str) -> str:
        """LLM callable for KnowledgeExtractor."""
        if not self._llm_client:
            return "[]"
        try:
            resp = self._llm_client.chat.completions.create(
                model=self._llm_model or "llama-3.1-8b-instruct",
                messages=[{"role": "user", "content": prompt}],
                temperature=0.0,
                max_tokens=512,
            )
            return resp.choices[0].message.content or "[]"
        except Exception:
            return "[]"

    def search(self, query: str, top_k: int = 5) -> list[dict]:
        """TF-IDF + Hebbian boost retrieval."""
        pulled_ids = self._gate.seed_from_context(query, top_k=top_k)

        results = []
        for node_id in pulled_ids:
            node = self.graph.get_node(node_id)
            if not node:
                continue
            results.append(
                {
                    "id": node.id,
                    "content": node.content or node.summary or node.label,
                    "source": node.metadata.get("source_url", "knowledge_graph"),
                    "confidence": node.importance,
                    "label": node.label,
                }
            )

        return results

    def ingest(
        self,
        question: str,
        answer: str,
        pulled_ids: list[str] | None = None,
    ) -> list[str]:
        """Extract new facts, strengthen co-activated edges, persist."""
        pulled_ids = pulled_ids or []

        new_ids = self._extractor.extract(question, answer, pulled_ids)

        all_active = pulled_ids + new_ids
        if len(all_active) >= 2:
            self._engine.strengthen(all_active, task_success=True)

        self._persistence.save_graph(self.graph)

        return new_ids

    def store_finding(
        self,
        content: str,
        source_url: str = "",
        importance: float = 0.5,
        domain: str = "Research",
    ) -> str:
        """Explicitly store a finding as a knowledge node."""
        node_id = f"finding_{int(time.time())}_{hash(content) & 0xFFFF:04x}"
        self._loader.add_node(
            self.graph,
            node_id=node_id,
            label=content[:80],
            content=content,
            domain=domain,
            node_type="finding",
            importance=importance,
        )
        node = self.graph.get_node(node_id)
        if node:
            node.metadata["source_url"] = source_url

        self._persistence.save_graph(self.graph)
        return node_id

    def decay_and_prune(self) -> dict:
        """Called at session start — decay unused, prune dead nodes."""
        decay_stats = self._engine.decay_all()
        pruned_count = self._engine.prune()
        self._persistence.save_graph(self.graph)
        return {
            "edges_decayed": decay_stats.get("edges_decayed", 0),
            "edges_pruned": pruned_count,
        }
