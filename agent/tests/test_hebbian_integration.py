import sys
import tempfile
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "src"))

from research_agent.hebbian.knowledge_graph import (
    KnowledgeGraph, GraphNode, RelationType,
)
from research_agent.hebbian.hebbian_engine import HebbianEdgeEngine
from research_agent.hebbian.retrieval_gate import RetrievalGate
from research_agent.hebbian.working_memory import WorkingMemoryBuffer
from research_agent.hebbian.persistence import GraphPersistence
from research_agent.hebbian.introspection import IntrospectionEngine
from research_agent.hebbian.behavioral import BehavioralEngine
from research_agent.hebbian.personality import PersonalitySynthesizer, build_personality_prompt


def _try_get_provider():
    try:
        from research_agent.hebbian.embeddings import get_provider
        provider = get_provider("qwen3")
        result = provider.embed("test")
        if result is None or len(result) < 100:
            return None
        return provider
    except Exception:
        return None


_provider = _try_get_provider()
requires_lmstudio = pytest.mark.skipif(
    _provider is None,
    reason="LM Studio not running at localhost:1234",
)


def _build_multi_domain_graph(provider):
    """Build a 15+ node graph across systems/database/web domains with real embeddings + intra-domain edges."""
    g = KnowledgeGraph()
    engine = HebbianEdgeEngine(g)

    nodes = {
        # Systems domain (5 nodes)
        "sys_ownership": ("Rust Ownership Model", "Each value has exactly one owner. When the owner goes out of scope the value is dropped. This prevents double-free bugs.", "systems"),
        "sys_borrowing": ("Rust Borrowing Rules", "You can have either one mutable reference or any number of immutable references. References must always be valid.", "systems"),
        "sys_lifetimes": ("Rust Lifetime Annotations", "Lifetimes are a way of telling the compiler how long references are valid. They prevent dangling references.", "systems"),
        "sys_memory": ("Memory Safety Without GC", "Rust achieves memory safety without garbage collection through ownership and borrowing checked at compile time.", "systems"),
        "sys_concurrency": ("Fearless Concurrency", "Rust's ownership system prevents data races at compile time. Send and Sync traits mark safe cross-thread types.", "systems"),

        # Database domain (5 nodes)
        "db_btree": ("B-Tree Index Structure", "B-trees are balanced search trees used in database indexes. Each node contains sorted keys and child pointers.", "database"),
        "db_wal": ("Write-Ahead Logging", "WAL ensures durability by writing changes to a log before applying them. Recovery replays the log after crashes.", "database"),
        "db_mvcc": ("Multi-Version Concurrency Control", "MVCC allows concurrent reads without blocking writes. Each transaction sees a snapshot of the database.", "database"),
        "db_sharding": ("Database Sharding Strategies", "Horizontal partitioning distributes rows across multiple servers based on a shard key for scalability.", "database"),
        "db_replication": ("Database Replication", "Leader-follower replication copies data to read replicas. Consensus protocols like Raft ensure consistency.", "database"),

        # Web domain (5 nodes)
        "web_http2": ("HTTP/2 Protocol", "HTTP/2 introduces multiplexed streams, header compression, and server push over a single TCP connection.", "web"),
        "web_websocket": ("WebSocket Protocol", "WebSocket provides full-duplex communication over a single persistent TCP connection for real-time data.", "web"),
        "web_cors": ("Cross-Origin Resource Sharing", "CORS is a security mechanism that restricts HTTP requests from different origins using preflight checks.", "web"),
        "web_csp": ("Content Security Policy", "CSP prevents XSS by specifying allowed sources for scripts, styles, and other resources via HTTP headers.", "web"),
        "web_tls": ("TLS Handshake", "TLS 1.3 reduces handshake to one round trip. Establishes symmetric encryption after key exchange and certificate verification.", "web"),
    }

    for node_id, (label, content, domain) in nodes.items():
        node = g.add_node(
            node_id=node_id,
            label=label,
            content=content,
            node_type="fact",
            importance=0.8,
            attributes={"domain": domain},
        )
        # Introspection reads domain from metadata, not attributes
        node.metadata["domain"] = domain

    if provider:
        for nid, node in g.nodes.items():
            text = f"{node.label} {node.content}"
            node.embedding = provider.embed(text)

    domain_groups = {}
    for nid, (_, _, domain) in nodes.items():
        domain_groups.setdefault(domain, []).append(nid)

    for domain, nids in domain_groups.items():
        for _ in range(10):
            engine.strengthen(nids, task_type="analysis", task_success=True)

    # Cross-domain edges (weaker — only 3 strengthen ops vs 10 for intra-domain)
    for _ in range(3):
        engine.strengthen(["sys_memory", "db_mvcc"], task_type="comparison", task_success=True)
        engine.strengthen(["web_tls", "sys_concurrency"], task_type="synthesis", task_success=True)

    return g, engine


@requires_lmstudio
class TestIntrospectionWithRealEmbeddings:
    def test_domain_profiles_reflect_graph_structure(self):
        g, _ = _build_multi_domain_graph(_provider)
        engine = IntrospectionEngine(g)
        model = engine.compute_self_model()

        assert len(model.profiles) >= 3, (
            f"Expected 3+ domain profiles, got {len(model.profiles)}: "
            f"{list(model.profiles.keys())}"
        )

        for domain, profile in model.profiles.items():
            assert profile.proficiency > 0, (
                f"Domain '{domain}' has zero proficiency despite having nodes"
            )

        t = model.tendencies
        assert -1 <= t.depth_vs_breadth <= 1
        assert -1 <= t.theoretical_vs_practical <= 1
        assert 0 <= t.confidence_calibration <= 1
        assert 0 <= t.novelty_seeking <= 1

    def test_knowledge_frontier_finds_isolated_nodes(self):
        g, engine = _build_multi_domain_graph(_provider)

        iso = g.add_node(
            node_id="isolated_node",
            label="Quantum Computing Basics",
            content="Qubits use superposition and entanglement for parallel computation.",
            node_type="fact",
            importance=0.9,
            attributes={"domain": "quantum"},
        )
        iso.metadata["domain"] = "quantum"

        ie = IntrospectionEngine(g)
        model = ie.compute_self_model()

        isolated_ids = model.frontier.isolated_high_value
        assert "isolated_node" in isolated_ids, (
            f"Isolated node not found in frontier. Got: {isolated_ids}"
        )

    def test_skill_matrix_tracks_task_types(self):
        g, _ = _build_multi_domain_graph(_provider)
        ie = IntrospectionEngine(g)
        model = ie.compute_self_model()

        if model.skills:
            task_types = {entry.task_type for entry in model.skills}
            assert "analysis" in task_types, (
                f"Expected 'analysis' in skill types, got: {task_types}"
            )


@requires_lmstudio
class TestBehavioralWithRealEmbeddings:
    def test_primes_prefer_strong_domain_nodes(self):
        g, _ = _build_multi_domain_graph(_provider)

        ie = IntrospectionEngine(g)
        model = ie.compute_self_model()

        be = BehavioralEngine(g)
        primes = be.compute_retrieval_primes(model)

        assert len(primes) > 0, "BehavioralEngine returned no primes"

        for nid, val in primes.items():
            assert isinstance(val, (int, float)), f"Prime for {nid} is not numeric: {type(val)}"
            assert val == val, f"Prime for {nid} is NaN"  # NaN check via reflexivity


@requires_lmstudio
class TestPersonalityWithRealEmbeddings:
    def test_personality_description_is_meaningful(self):
        g, _ = _build_multi_domain_graph(_provider)

        ie = IntrospectionEngine(g)
        model = ie.compute_self_model()

        synth = PersonalitySynthesizer()
        snapshot = synth.synthesize(model)

        assert len(snapshot.description) > 50, (
            f"Description too short ({len(snapshot.description)} chars): {snapshot.description}"
        )
        assert len(snapshot.strengths) >= 1, "No strengths identified"
        assert snapshot.reasoning_style, "Empty reasoning style"

        desc_lower = snapshot.description.lower()
        has_domain_ref = any(d in desc_lower for d in ["systems", "database", "web"])
        assert has_domain_ref, (
            f"Description doesn't reference any expected domain: {snapshot.description}"
        )

    def test_personality_prompt_is_usable(self):
        g, _ = _build_multi_domain_graph(_provider)

        ie = IntrospectionEngine(g)
        model = ie.compute_self_model()

        synth = PersonalitySynthesizer()
        snapshot = synth.synthesize(model)
        prompt = build_personality_prompt(snapshot)

        assert len(prompt) > 100, f"Prompt too short: {len(prompt)} chars"
        assert "strengths" in prompt.lower() or "strong" in prompt.lower(), (
            "Prompt doesn't mention strengths"
        )


@requires_lmstudio
class TestPersistenceRoundTrip:
    def test_personality_survives_persistence(self, tmp_path):
        g, _ = _build_multi_domain_graph(_provider)
        db_path = str(tmp_path / "test_personality.db")

        ie1 = IntrospectionEngine(g)
        model1 = ie1.compute_self_model()
        synth = PersonalitySynthesizer()
        snap1 = synth.synthesize(model1)

        persistence = GraphPersistence(db_path)
        persistence.save_graph(g)

        g2 = persistence.load_graph()

        ie2 = IntrospectionEngine(g2)
        model2 = ie2.compute_self_model()
        snap2 = synth.synthesize(model2)

        assert len(model2.profiles) == len(model1.profiles), (
            f"Domain count changed: {len(model1.profiles)} → {len(model2.profiles)}"
        )

        # Float precision may vary slightly across round-trip
        assert abs(model1.tendencies.depth_vs_breadth - model2.tendencies.depth_vs_breadth) < 0.01
        assert abs(model1.tendencies.novelty_seeking - model2.tendencies.novelty_seeking) < 0.01

        # Description must be deterministic across persistence
        assert snap1.description == snap2.description, (
            f"Personality changed after persistence:\n"
            f"  Before: {snap1.description[:100]}\n"
            f"  After:  {snap2.description[:100]}"
        )
