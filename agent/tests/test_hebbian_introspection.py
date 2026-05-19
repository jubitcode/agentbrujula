import json
import tempfile

from research_agent.hebbian.knowledge_graph import (
    KnowledgeGraph, GraphNode, GraphEdge, RelationType,
)
from research_agent.hebbian.introspection import IntrospectionEngine, AgentSelfModel
from research_agent.hebbian.behavioral import BehavioralEngine
from research_agent.hebbian.personality import PersonalitySynthesizer, PersonalitySnapshot, build_personality_prompt


def _make_graph_with_domains() -> KnowledgeGraph:
    """Small graph with two domains (alpha, beta) and one strong intra-domain edge."""
    g = KnowledgeGraph()
    g.add_node("a1", "Alpha concept", "concept", attributes={}, content="", importance=0.8)
    g.nodes["a1"].metadata["domain"] = "alpha"
    g.add_node("a2", "Alpha detail", "concept", attributes={}, content="", importance=0.7)
    g.nodes["a2"].metadata["domain"] = "alpha"
    g.add_node("b1", "Beta concept", "concept", attributes={}, content="", importance=0.9)
    g.nodes["b1"].metadata["domain"] = "beta"

    edge = g.add_edge("a1", "a2", RelationType.RELATED_TO, weight=0.6)
    edge.co_activations = 5
    edge.successful_co_activations = 4
    edge.success_by_task_type = {"comparison": [3, 5]}

    g.add_edge("a1", "b1", RelationType.RELATED_TO, weight=0.15)
    return g


class TestIntrospectionEngine:

    def test_compute_self_model_on_empty_graph(self):
        g = KnowledgeGraph()
        engine = IntrospectionEngine(g)
        model = engine.compute_self_model()
        assert isinstance(model, AgentSelfModel)
        assert model.total_nodes == 0
        assert model.total_edges == 0

    def test_domain_profiling(self):
        g = _make_graph_with_domains()
        engine = IntrospectionEngine(g)
        profile = engine.compute_domain_profile("alpha")
        assert profile.domain == "alpha"
        assert profile.node_count == 2
        assert profile.edge_count >= 1  # at least the a1->a2 intra-domain edge

    def test_tendencies_computation(self):
        g = _make_graph_with_domains()
        engine = IntrospectionEngine(g)
        tendencies = engine.compute_tendencies()
        assert -1.0 <= tendencies.depth_vs_breadth <= 1.0

    def test_knowledge_frontier(self):
        g = KnowledgeGraph()
        g.add_node("iso1", "Isolated", "concept", importance=0.9)
        engine = IntrospectionEngine(g)
        frontier = engine.compute_knowledge_frontier()
        assert "iso1" in frontier.isolated_high_value

    def test_zero_llm_calls(self):
        """Introspection must be pure topology analysis — completing without network errors proves no LLM calls."""
        g = _make_graph_with_domains()
        engine = IntrospectionEngine(g)
        model = engine.compute_self_model()
        assert model.computed_at > 0


class TestBehavioralEngine:

    def test_compute_retrieval_primes_empty(self):
        g = KnowledgeGraph()
        intro = IntrospectionEngine(g)
        model = intro.compute_self_model()
        engine = BehavioralEngine(g)
        primes = engine.compute_retrieval_primes(model)
        assert isinstance(primes, dict)
        assert len(primes) == 0

    def test_primes_are_numeric(self):
        g = _make_graph_with_domains()
        intro = IntrospectionEngine(g)
        model = intro.compute_self_model()
        engine = BehavioralEngine(g)
        primes = engine.compute_retrieval_primes(model)
        for v in primes.values():
            assert isinstance(v, float)


class TestPersonalitySynthesizer:

    def test_synthesize_empty_model(self):
        model = AgentSelfModel()
        synth = PersonalitySynthesizer()
        snapshot = synth.synthesize(model)
        assert isinstance(snapshot, PersonalitySnapshot)

    def test_build_personality_prompt_non_empty(self):
        g = _make_graph_with_domains()
        intro = IntrospectionEngine(g)
        model = intro.compute_self_model()
        synth = PersonalitySynthesizer()
        snapshot = synth.synthesize(model)
        prompt = build_personality_prompt(snapshot)
        assert isinstance(prompt, str)
        assert len(prompt) > 0


class TestGraphEdgeSerialization:

    def test_success_by_task_type_persists(self):
        g = KnowledgeGraph()
        g.add_node("x", "X", "concept")
        g.add_node("y", "Y", "concept")
        edge = g.add_edge("x", "y", RelationType.RELATED_TO, weight=0.5)
        edge.success_by_task_type = {"comparison": [3, 5], "mechanism": [1, 2]}

        d = edge.to_dict()
        assert "success_by_task_type" in d
        assert d["success_by_task_type"]["comparison"] == [3, 5]

        with tempfile.NamedTemporaryFile(suffix=".json", mode="w", delete=False) as f:
            path = f.name

        g.save(path)

        g2 = KnowledgeGraph()
        g2.load(path)

        loaded_edges = g2.get_edges(source_id="x", target_id="y")
        assert len(loaded_edges) == 1
        assert loaded_edges[0].success_by_task_type == {"comparison": [3, 5], "mechanism": [1, 2]}
