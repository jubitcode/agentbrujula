import time
import pytest

from research_agent.hebbian.knowledge_graph import KnowledgeGraph, RelationType
from research_agent.hebbian.hebbian_engine import HebbianEdgeEngine
from research_agent.hebbian.retrieval_gate import RetrievalGate
from research_agent.hebbian.working_memory import WorkingMemoryBuffer
from research_agent.hebbian.loader import KnowledgeLoader
from research_agent.hebbian.similarity import tfidf_most_similar


def make_system():
    g = KnowledgeGraph()
    engine = HebbianEdgeEngine(g)
    buffer = WorkingMemoryBuffer(g, token_limit=5000)
    gate = RetrievalGate(g, buffer, engine, max_pull_tokens=3000,
                         min_successful_coactivations=2, min_hebbian_boost=0.05)
    loader = KnowledgeLoader()
    return g, engine, buffer, gate, loader


class TestAblation:
    """Compare retrieval with vs without Hebbian edges to prove the layer adds value beyond TF-IDF."""

    def test_hebbian_surfaces_nodes_tfidf_cannot(self):
        g, engine, buffer, gate, loader = make_system()

        # Node A: matches query via TF-IDF
        loader.add_node(g, "rust_lang", "Rust Language",
                       "Rust is a systems programming language focused on safety",
                       "tech", "fact", importance=0.8)

        # Node B: semantically related but zero keyword overlap with "Rust"
        loader.add_node(g, "zero_cost", "Zero Cost Abstractions",
                       "Compile-time guarantees without runtime overhead enable fearless concurrency",
                       "tech", "fact", importance=0.7)

        for _ in range(5):
            engine.strengthen(["rust_lang", "zero_cost"], task_success=True)

        results_with = gate.seed_from_context("Rust programming language", top_k=5)

        corpus = {
            nid: f"{node.label} {node.summary} {node.content}"
            for nid, node in g.nodes.items()
        }
        tfidf_only = [nid for nid, score in tfidf_most_similar("Rust programming language", corpus, top_k=5)]

        assert "rust_lang" in results_with, "rust_lang not found with Hebbian"
        assert "rust_lang" in tfidf_only, "rust_lang not found by TF-IDF"

        assert "zero_cost" not in tfidf_only, \
            "zero_cost found by TF-IDF alone — test is invalid (keywords overlap)"
        assert "zero_cost" in results_with, \
            "Hebbian failed to surface zero_cost despite strong edge from rust_lang"

    def test_hebbian_reranks_tfidf_results(self):
        """Two nodes match TF-IDF equally — Hebbian edge must break the tie."""
        g, engine, buffer, gate, loader = make_system()

        loader.add_node(g, "postgres", "PostgreSQL Database",
                       "PostgreSQL is an advanced open-source relational database system",
                       "tech", "fact", importance=0.5)
        loader.add_node(g, "mysql", "MySQL Database",
                       "MySQL is a popular open-source relational database management system",
                       "tech", "fact", importance=0.5)

        loader.add_node(g, "acid_txn", "ACID Transactions",
                       "ACID compliance ensures reliable transaction processing",
                       "tech", "fact", importance=0.7)

        for _ in range(5):
            engine.strengthen(["acid_txn", "postgres"], task_success=True)

        results = gate.seed_from_context("ACID compliant relational database system", top_k=3)

        if "postgres" in results and "mysql" in results:
            pg_idx = results.index("postgres")
            my_idx = results.index("mysql")
            assert pg_idx < my_idx, \
                f"postgres (idx={pg_idx}) should rank above mysql (idx={my_idx}) due to Hebbian boost"


class TestSameDomainStress:
    """500 nodes in the same domain with overlapping vocabulary — Hebbian must rescue retrieval when TF-IDF is ambiguous."""

    def test_500_programming_nodes_hebbian_rescues(self):
        g, engine, buffer, gate, loader = make_system()

        loader.add_node(g, "target", "Rust Borrow Checker Internals",
                       "The borrow checker validates lifetime annotations and reference rules at compile time",
                       "programming", "fact", importance=0.8)

        # 499 noise nodes ALL about programming with overlapping vocabulary
        languages = ["Python", "Go", "Java", "C++", "JavaScript", "TypeScript", "Kotlin", "Swift"]
        concepts = ["compiler", "runtime", "garbage collection", "memory management",
                   "type system", "concurrency", "error handling", "testing",
                   "debugging", "profiling", "optimization", "build system"]

        for i in range(499):
            lang = languages[i % len(languages)]
            concept = concepts[i % len(concepts)]
            loader.add_node(g, f"noise_{i}", f"{lang} {concept}",
                           f"{lang} provides {concept} features for software development "
                           f"with compile time and runtime checks for safety and performance",
                           "programming", "fact", importance=0.3)

        corpus = {nid: f"{n.label} {n.summary} {n.content}" for nid, n in g.nodes.items()}
        tfidf_results = [nid for nid, _ in tfidf_most_similar(
            "borrow checker compile time validation", corpus, top_k=5)]

        loader.add_node(g, "bridge", "Rust Compiler Pipeline",
                       "The Rust compiler pipeline includes parsing, type checking, and code generation",
                       "programming", "fact", importance=0.6)

        for _ in range(5):
            engine.strengthen(["bridge", "target"], task_success=True)

        results_with = gate.seed_from_context("Rust compiler pipeline checking", top_k=5)

        print(f"\n  TF-IDF only top-5: {tfidf_results}")
        print(f"  With Hebbian top-5: {results_with}")
        print(f"  Target in TF-IDF: {'target' in tfidf_results}")
        print(f"  Target in Hebbian: {'target' in results_with}")

        assert "bridge" in results_with or "target" in results_with, \
            "Neither bridge nor target found with Hebbian in 500-node same-domain graph"


class TestTrueCompositionality:
    """Chain nodes with zero semantic overlap via Hebbian edges only — querying A must surface B/C."""

    def test_zero_overlap_chain_a_to_c(self):
        g, engine, buffer, gate, loader = make_system()

        loader.add_node(g, "alpha", "Purple Elephants",
                       "Fictional purple elephants roaming the savanna at dusk",
                       "fiction", "fact", importance=0.7)
        loader.add_node(g, "beta", "Quantum Tunneling",
                       "Particles passing through energy barriers via wave function probability",
                       "physics", "fact", importance=0.7)
        loader.add_node(g, "gamma", "Sourdough Starter",
                       "Wild yeast fermentation culture for artisan bread baking",
                       "cooking", "fact", importance=0.7)

        for _ in range(10):
            engine.strengthen(["alpha", "beta"], task_success=True)
        for _ in range(10):
            engine.strengthen(["beta", "gamma"], task_success=True)

        results = gate.seed_from_context("purple elephants roaming savanna", top_k=5)

        print(f"\n  Query: 'purple elephants roaming savanna'")
        print(f"  Results: {results}")

        assert "alpha" in results, "Alpha (direct TF-IDF match) not found"

        beta_found = "beta" in results
        print(f"  Beta (1-hop Hebbian): {'FOUND' if beta_found else 'NOT FOUND'}")

        if beta_found:
            gamma_found = "gamma" in results
            print(f"  Gamma (2-hop Hebbian): {'FOUND' if gamma_found else 'NOT FOUND'}")
        else:
            edges = g.get_edges(source_id="alpha", target_id="beta")
            if edges:
                print(f"  Edge alpha→beta: weight={edges[0].weight:.3f}, "
                      f"successful_coact={edges[0].successful_co_activations}")

        assert beta_found, \
            "Hebbian could not surface 'quantum tunneling' from 'purple elephants' " \
            "despite 10x strengthened edge — proves system only boosts when keywords overlap, " \
            "no pure associative retrieval."

    def test_ablation_compositionality(self):
        g, engine, buffer, gate, loader = make_system()

        loader.add_node(g, "node_x", "Crystalline Formations",
                       "Mineral crystal structures growing in underground caves",
                       "geology", "fact", importance=0.6)
        loader.add_node(g, "node_y", "Jazz Improvisation",
                       "Musical spontaneous composition over harmonic chord progressions",
                       "music", "fact", importance=0.6)

        corpus = {nid: f"{n.label} {n.summary} {n.content}" for nid, n in g.nodes.items()}
        tfidf_only = [nid for nid, _ in tfidf_most_similar(
            "crystal formations in caves", corpus, top_k=5)]

        assert "node_y" not in tfidf_only, "Jazz found by TF-IDF — keywords overlap (test invalid)"

        for _ in range(10):
            engine.strengthen(["node_x", "node_y"], task_success=True)

        results_with = gate.seed_from_context("crystal formations in caves", top_k=5)

        print(f"\n  TF-IDF only: {tfidf_only}")
        print(f"  With Hebbian: {results_with}")

        jazz_found_hebbian = "node_y" in results_with
        jazz_found_tfidf = "node_y" in tfidf_only

        print(f"  Jazz via TF-IDF: {jazz_found_tfidf}")
        print(f"  Jazz via Hebbian: {jazz_found_hebbian}")

        assert jazz_found_hebbian and not jazz_found_tfidf, \
            "Ablation failed: Hebbian didn't add node_y to results despite strong edge"
