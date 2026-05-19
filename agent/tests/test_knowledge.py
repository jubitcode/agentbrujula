import tempfile
from pathlib import Path

import pytest

from research_agent.knowledge import ResearchKnowledgeStore


@pytest.fixture
def store(tmp_path):
    """Store with temp DB and no LLM extractor."""
    db = str(tmp_path / "test_knowledge.db")
    return ResearchKnowledgeStore(db_path=db)


class TestResearchKnowledgeStore:
    def test_store_finding(self, store):
        node_id = store.store_finding(
            content="Python was created by Guido van Rossum",
            source_url="https://python.org",
            importance=0.8,
        )
        assert node_id.startswith("finding_")
        node = store.graph.get_node(node_id)
        assert node is not None
        assert "Guido" in node.content

    def test_search_empty_graph(self, store):
        results = store.search("anything", top_k=5)
        assert results == []

    def test_search_finds_stored(self, store):
        store.store_finding("Python is a programming language", importance=0.7)
        store.store_finding("Rust is a systems programming language", importance=0.8)
        store.store_finding("Tokyo is the capital of Japan", importance=0.5)

        results = store.search("programming language", top_k=5)
        assert len(results) >= 1
        contents = " ".join(r["content"] for r in results)
        assert "programming" in contents.lower()

    def test_ingest_without_llm(self, store):
        """Without an LLM extractor, extraction returns '[]' and no new nodes — must not crash."""
        new_ids = store.ingest(
            question="What is Python?",
            answer="Python is a high-level programming language.",
            pulled_ids=[],
        )
        assert isinstance(new_ids, list)

    def test_decay_and_prune_empty(self, store):
        result = store.decay_and_prune()
        assert "edges_decayed" in result
        assert "edges_pruned" in result

    def test_decay_and_prune_with_data(self, store):
        id1 = store.store_finding("Fact A")
        id2 = store.store_finding("Fact B")
        from research_agent.hebbian.knowledge_graph import RelationType
        store.graph.add_edge(id1, id2, RelationType.RELATED_TO, weight=0.3)

        result = store.decay_and_prune()
        assert isinstance(result["edges_decayed"], int)

    def test_persistence_across_instances(self, tmp_path):
        db = str(tmp_path / "persist_test.db")

        store1 = ResearchKnowledgeStore(db_path=db)
        # TF-IDF needs 2+ nodes for non-zero scores
        store1.store_finding("Python is a programming language created by Guido", importance=0.9)
        store1.store_finding("Tokyo is the capital city of Japan", importance=0.5)

        store2 = ResearchKnowledgeStore(db_path=db)
        assert len(store2.graph.nodes) == 2
        results = store2.search("programming language python", top_k=5)
        assert len(results) >= 1

    def test_store_finding_metadata(self, store):
        node_id = store.store_finding(
            content="Test content",
            source_url="https://example.com",
            domain="Testing",
        )
        node = store.graph.get_node(node_id)
        assert node.metadata.get("source_url") == "https://example.com"
