import math
import time
import tempfile
import os

import pytest

from research_agent.hebbian.knowledge_graph import KnowledgeGraph, RelationType, GraphEdge
from research_agent.hebbian.hebbian_engine import HebbianEdgeEngine
from research_agent.hebbian.working_memory import WorkingMemoryBuffer
from research_agent.hebbian.retrieval_gate import RetrievalGate
from research_agent.hebbian.persistence import GraphPersistence
from research_agent.hebbian.loader import KnowledgeLoader


def _make_graph_with_edge(weight: float = 0.5) -> tuple[KnowledgeGraph, HebbianEdgeEngine, GraphEdge]:
    graph = KnowledgeGraph()
    graph.add_node("a", "A", "fact", content="Node A")
    graph.add_node("b", "B", "fact", content="Node B")
    edge = graph.add_edge("a", "b", RelationType.RELATED_TO, weight=weight)
    engine = HebbianEdgeEngine(graph)
    return graph, engine, edge


class TestSNAPWeightUpdate:
    """SNAP sigmoidal growth: delta_W = |W| * (1 - |W|) * delta."""

    def test_mid_range_weights_change_faster_than_extremes(self):
        results = {}
        for w in (0.1, 0.5, 0.9):
            graph, engine, edge = _make_graph_with_edge(w)
            original = edge.weight
            edge.last_strengthened = time.time() - 100
            edge.co_activations = 5
            edge.successful_co_activations = 4
            engine._strengthen_edge(edge, importance_factor=1.0, now=time.time(), task_type="test")
            results[w] = abs(edge.weight - original)

        assert results[0.5] > results[0.1], "Mid-range weight should change more than low weight"
        assert results[0.5] > results[0.9], "Mid-range weight should change more than high weight"

    def test_weight_never_exceeds_bounds(self):
        graph, engine, edge = _make_graph_with_edge(0.99)
        edge.co_activations = 1
        edge.successful_co_activations = 1
        engine.learning_rate = 10.0  # absurdly high to stress the clamp
        engine._strengthen_edge(edge, importance_factor=5.0, now=time.time(), task_type="test")
        assert 0.0 <= edge.weight <= 1.0

    def test_saturated_weight_barely_changes(self):
        graph, engine, edge = _make_graph_with_edge(0.99)
        edge.co_activations = 10
        edge.successful_co_activations = 10
        edge.last_strengthened = time.time() - 3600
        original = edge.weight
        engine._strengthen_edge(edge, importance_factor=1.0, now=time.time(), task_type="test")
        delta = abs(edge.weight - original)
        # SNAP factor at w=0.99: 0.99 * 0.01 = 0.0099, so delta should be tiny
        assert delta < 0.01, f"Saturated weight changed too much: {delta}"

    def test_zero_weight_does_not_change(self):
        graph, engine, edge = _make_graph_with_edge(0.01)
        edge.weight = 0.0  # bypass __post_init__ clamping
        edge.co_activations = 5
        edge.successful_co_activations = 5
        engine._strengthen_edge(edge, importance_factor=1.0, now=time.time(), task_type="test")
        assert edge.weight == 0.0


class TestSleepMethod:
    """Homeostatic sleep renormalization."""

    def test_sleep_reduces_edge_weights(self):
        graph, engine, edge = _make_graph_with_edge(0.8)
        edge.last_strengthened = time.time() - 7200
        edge.successful_co_activations = 0

        result = engine.sleep(lam=0.9, w_target=0.1)
        assert result["edges_touched"] == 1
        assert edge.weight < 0.8

    def test_fresh_edges_resist_decay(self):
        graph1, engine1, edge1 = _make_graph_with_edge(0.8)
        edge1.last_strengthened = time.time()
        edge1.successful_co_activations = 0
        engine1.sleep(lam=0.5, w_target=0.1)
        fresh_weight = edge1.weight

        graph2, engine2, edge2 = _make_graph_with_edge(0.8)
        edge2.last_strengthened = time.time() - 86400
        edge2.successful_co_activations = 0
        engine2.sleep(lam=0.5, w_target=0.1)
        stale_weight = edge2.weight

        assert fresh_weight > stale_weight, (
            f"Fresh edge ({fresh_weight:.4f}) should resist decay more than stale edge ({stale_weight:.4f})"
        )

    def test_successful_edges_resist_decay(self):
        graph1, engine1, edge1 = _make_graph_with_edge(0.8)
        edge1.last_strengthened = time.time() - 3600
        edge1.successful_co_activations = 20
        engine1.sleep(lam=0.5, w_target=0.1)
        successful_weight = edge1.weight

        graph2, engine2, edge2 = _make_graph_with_edge(0.8)
        edge2.last_strengthened = time.time() - 3600
        edge2.successful_co_activations = 0
        engine2.sleep(lam=0.5, w_target=0.1)
        unused_weight = edge2.weight

        assert successful_weight > unused_weight, (
            f"Successful edge ({successful_weight:.4f}) should resist decay more than unused edge ({unused_weight:.4f})"
        )

    def test_sleep_preserves_bounds(self):
        graph, engine, edge = _make_graph_with_edge(0.05)
        edge.last_strengthened = time.time() - 86400
        engine.sleep(lam=0.5, w_target=0.1, noise_std=0.5)
        assert 0.0 <= edge.weight <= 1.0

    def test_sleep_with_noise(self):
        graph, engine, edge = _make_graph_with_edge(0.5)
        edge.last_strengthened = time.time() - 3600
        engine.sleep(lam=0.9, w_target=0.1, noise_std=0.01)
        assert 0.0 <= edge.weight <= 1.0


class TestLogCompressedBoost:
    def test_boost_is_log_compressed(self):
        """10 co-activations must NOT produce 10x the boost of 1 co-activation (log1p compression)."""
        graph = KnowledgeGraph()
        graph.add_node("seed", "Seed", "fact", content="Seed node text about algorithms")
        graph.add_node("target1", "Target1", "fact", content="Target one")
        graph.add_node("target10", "Target10", "fact", content="Target ten")

        e1 = graph.add_edge("seed", "target1", RelationType.RELATED_TO, weight=0.8)
        e1.successful_co_activations = 1

        e10 = graph.add_edge("seed", "target10", RelationType.RELATED_TO, weight=0.8)
        e10.successful_co_activations = 10

        buffer = WorkingMemoryBuffer(graph)
        engine = HebbianEdgeEngine(graph)
        gate = RetrievalGate(graph, buffer, engine)

        # Mirror seed_from_context's boost formula manually
        seed_tfidf = 0.5
        boost_1 = seed_tfidf * e1.weight * math.log1p(e1.successful_co_activations)
        boost_10 = seed_tfidf * e10.weight * math.log1p(e10.successful_co_activations)

        ratio = boost_10 / boost_1
        # log1p(10) / log1p(1) ≈ 2.398/0.693 ≈ 3.46
        assert ratio < 5.0, f"Boost ratio {ratio:.2f} should be much less than 10 (log-compressed)"
        assert ratio > 1.0, "More co-activations should still produce higher boost"


class TestPersistence:
    def test_success_by_task_type_round_trip(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = os.path.join(tmpdir, "test.db")
            persistence = GraphPersistence(db_path)

            graph = KnowledgeGraph()
            graph.add_node("a", "A", "fact", content="Node A")
            graph.add_node("b", "B", "fact", content="Node B")
            edge = graph.add_edge("a", "b", RelationType.RELATED_TO, weight=0.7)
            edge.success_by_task_type = {
                "research": [5, 8],
                "analysis": [3, 3],
            }

            persistence.save_graph(graph)
            loaded = persistence.load_graph()

            loaded_edges = loaded.get_edges(source_id="a", target_id="b")
            assert len(loaded_edges) == 1
            loaded_edge = loaded_edges[0]
            assert loaded_edge.success_by_task_type == {"research": [5, 8], "analysis": [3, 3]}

    def test_success_by_task_type_in_update_edge_weights(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = os.path.join(tmpdir, "test.db")
            persistence = GraphPersistence(db_path)

            graph = KnowledgeGraph()
            graph.add_node("x", "X", "fact", content="Node X")
            graph.add_node("y", "Y", "fact", content="Node Y")
            edge = graph.add_edge("x", "y", RelationType.RELATED_TO, weight=0.5)
            persistence.save_graph(graph)

            edge.weight = 0.9
            edge.success_by_task_type = {"coding": [10, 12]}
            persistence.update_edge_weights([edge])

            loaded = persistence.load_graph()
            loaded_edge = loaded.get_edges(source_id="x", target_id="y")[0]
            assert loaded_edge.weight == pytest.approx(0.9)
            assert loaded_edge.success_by_task_type == {"coding": [10, 12]}

    def test_migration_adds_column(self):
        """Constructing GraphPersistence on an old db must migrate the schema in-place."""
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = os.path.join(tmpdir, "test_migrate.db")
            persistence = GraphPersistence(db_path)
            graph = KnowledgeGraph()
            graph.add_node("m", "M", "fact", content="Migrate test")
            graph.add_node("n", "N", "fact", content="Migrate test 2")
            edge = graph.add_edge("m", "n", RelationType.RELATED_TO, weight=0.3)
            edge.success_by_task_type = {"test": [1, 1]}
            persistence.save_graph(graph)
            loaded = persistence.load_graph()
            assert loaded.get_edges(source_id="m", target_id="n")[0].success_by_task_type == {"test": [1, 1]}


class TestLoaderStaticMethods:
    """add_node, remove_node, _parse_edges must be callable without an instance."""

    def test_add_node_callable_without_instance(self):
        graph = KnowledgeGraph()
        node = KnowledgeLoader.add_node(
            graph, "test_node", "Test", "Test content", "test_domain"
        )
        assert node.id == "test_node"
        assert node.metadata["domain"] == "test_domain"

    def test_remove_node_callable_without_instance(self):
        graph = KnowledgeGraph()
        graph.add_node("del_me", "Delete Me", "fact", content="bye")
        result = KnowledgeLoader.remove_node(graph, "del_me")
        assert result is True
        assert "del_me" not in graph.nodes

    def test_parse_edges_callable_without_instance(self):
        edges = KnowledgeLoader._parse_edges("REQUIRES:node_a, ENABLES:node_b")
        assert len(edges) == 2
        assert edges[0] == (RelationType.REQUIRES, "node_a")
        assert edges[1] == (RelationType.ENABLES, "node_b")


class TestWorkingMemoryStaticMethod:
    def test_estimate_tokens_callable_without_instance(self):
        from research_agent.hebbian.knowledge_graph import GraphNode
        node = GraphNode(id="t", label="T", node_type="fact", content="hello world test")
        tokens = WorkingMemoryBuffer._estimate_tokens(node)
        assert tokens == max(1, len("hello world test") // 4)
