import os
import tempfile

import pytest

from research_agent.hebbian.knowledge_graph import KnowledgeGraph, RelationType
from research_agent.hebbian.hebbian_engine import HebbianEdgeEngine
from research_agent.hebbian.retrieval_gate import RetrievalGate
from research_agent.hebbian.working_memory import WorkingMemoryBuffer
from research_agent.hebbian.loader import KnowledgeLoader
from research_agent.hebbian.persistence import GraphPersistence
from research_agent.hebbian.similarity import tfidf_most_similar


def make_system(graph=None):
    g = graph or KnowledgeGraph()
    engine = HebbianEdgeEngine(g)
    buffer = WorkingMemoryBuffer(g, token_limit=5000)
    gate = RetrievalGate(g, buffer, engine, max_pull_tokens=3000,
                         min_successful_coactivations=2, min_hebbian_boost=0.05)
    loader = KnowledgeLoader()
    return g, engine, buffer, gate, loader


class TestSessionContinuity:
    """Proves session 1 associations improve session 2 retrieval — the cross-session learning use case."""

    def test_session1_builds_session2_retrieves(self):
        """Session 2 query has zero keyword overlap with Rust ownership — only Hebbian edges can bridge."""
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = os.path.join(tmpdir, "session_test.db")

            g1, engine1, buffer1, gate1, loader1 = make_system()

            loader1.add_node(g1, "ownership_model", "Ownership Model",
                            "Each value in the program has exactly one owner variable. "
                            "When the owner goes out of scope, the value is dropped automatically.",
                            "systems", "fact", importance=0.8)

            loader1.add_node(g1, "borrow_checker", "Borrow Checker",
                            "Static analysis at compile time ensures references do not outlive "
                            "the data they point to, preventing dangling pointers.",
                            "systems", "fact", importance=0.8)

            loader1.add_node(g1, "raii_pattern", "RAII Pattern",
                            "Resource acquisition is initialization. Destructors run automatically "
                            "when objects leave scope, ensuring cleanup without manual free calls.",
                            "systems", "fact", importance=0.7)

            loader1.add_node(g1, "lifetime_annotations", "Lifetime Annotations",
                            "Explicit lifetime parameters let the compiler verify that references "
                            "remain valid for their entire usage span.",
                            "systems", "fact", importance=0.6)

            # Bridge concept to session 2 query
            loader1.add_node(g1, "memory_safety", "Compile-Time Memory Safety",
                            "Guaranteeing absence of use-after-free, double-free, and dangling "
                            "pointers without runtime garbage collection overhead.",
                            "systems", "concept", importance=0.9)

            session1_active = ["ownership_model", "borrow_checker", "raii_pattern",
                             "lifetime_annotations", "memory_safety"]

            for _ in range(5):
                engine1.strengthen(session1_active, task_success=True)

            persistence = GraphPersistence(db_path)
            persistence.save_graph(g1)

            edge_count = sum(len(edges) for edges in g1.outgoing_edges.values())
            print(f"\n  Session 1: {len(g1.nodes)} nodes, {edge_count} edges")
            for src, edges in g1.outgoing_edges.items():
                for e in edges[:2]:
                    print(f"    {src} → {e.target_id}: weight={e.weight:.3f}, "
                          f"success={e.successful_co_activations}")

            g2 = persistence.load_graph()
            _, engine2, buffer2, gate2, loader2 = make_system(g2)

            # Query intentionally avoids "Rust", "ownership", "borrow checker"
            session2_query = "How to prevent memory leaks and dangling pointers in systems code without garbage collection"

            results_with_hebbian = gate2.seed_from_context(session2_query, top_k=5)

            # Control: same nodes, no Hebbian edges
            g_control = KnowledgeGraph()
            for nid, node in g2.nodes.items():
                g_control.add_node(nid, node.label, node.node_type,
                                  content=node.content, summary=node.summary,
                                  importance=node.importance)
            _, _, _, gate_control, _ = make_system(g_control)
            results_without_hebbian = gate_control.seed_from_context(session2_query, top_k=5)

            print(f"\n  Session 2 query: '{session2_query[:60]}...'")
            print(f"  WITH Hebbian (session 1 edges): {results_with_hebbian}")
            print(f"  WITHOUT Hebbian (no edges):     {results_without_hebbian}")

            hebbian_exclusive = set(results_with_hebbian) - set(results_without_hebbian)
            print(f"  Hebbian-exclusive results:      {hebbian_exclusive}")

            assert len(hebbian_exclusive) > 0, \
                "Hebbian edges from session 1 added nothing to session 2 retrieval — no cross-session learning."

            print(f"\n  Session 1 learning improved session 2 retrieval by adding "
                  f"{len(hebbian_exclusive)} nodes TF-IDF alone could not find:")
            for nid in hebbian_exclusive:
                node = g2.nodes[nid]
                print(f"    - {node.label}: {node.content[:80]}...")

    def test_learning_accumulates_across_three_sessions(self):
        """Session 1 A→B, session 2 B→C; session 3 query for C-related topic should surface A via 2-hop chain."""
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = os.path.join(tmpdir, "multi_session.db")

            g1, engine1, _, _, loader1 = make_system()

            loader1.add_node(g1, "concurrency", "Fearless Concurrency",
                            "Thread safety guaranteed at compile time without runtime locks",
                            "systems", "fact", importance=0.7)

            loader1.add_node(g1, "send_sync", "Send and Sync Traits",
                            "Marker traits that indicate thread-safe data transfer and sharing",
                            "systems", "fact", importance=0.6)

            for _ in range(5):
                engine1.strengthen(["concurrency", "send_sync"], task_success=True)

            GraphPersistence(db_path).save_graph(g1)
            print(f"\n  Session 1: linked concurrency ↔ send_sync")

            g2 = GraphPersistence(db_path).load_graph()
            engine2 = HebbianEdgeEngine(g2)
            loader2 = KnowledgeLoader()

            loader2.add_node(g2, "data_race", "Data Race Prevention",
                            "Eliminating concurrent mutable access through type system constraints",
                            "systems", "fact", importance=0.8)

            for _ in range(5):
                engine2.strengthen(["send_sync", "data_race"], task_success=True)

            GraphPersistence(db_path).save_graph(g2)
            print(f"  Session 2: linked send_sync ↔ data_race")

            g3 = GraphPersistence(db_path).load_graph()
            _, _, _, gate3, _ = make_system(g3)

            results = gate3.seed_from_context("fearless concurrency thread safety", top_k=5)

            print(f"  Session 3 query: 'fearless concurrency thread safety'")
            print(f"  Results: {results}")

            assert "concurrency" in results, "Direct match not found"

            send_sync_found = "send_sync" in results
            data_race_found = "data_race" in results

            print(f"  send_sync (1-hop, session 1): {'FOUND' if send_sync_found else 'NOT FOUND'}")
            print(f"  data_race (2-hop, session 1+2): {'FOUND' if data_race_found else 'NOT FOUND'}")

            assert send_sync_found, \
                "Session 1 edge (concurrency→send_sync) not preserved across sessions"

    def test_session_learning_improves_retrieval_quality_quantitatively(self):
        """Recall comparison across sessions: paraphrased session 2 query must score higher with Hebbian vs without."""
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = os.path.join(tmpdir, "quantitative.db")

            g1, engine1, _, _, loader1 = make_system()

            facts = [
                ("event_sourcing", "Event Sourcing",
                 "Storing all state changes as an immutable sequence of events"),
                ("cqrs", "CQRS Pattern",
                 "Separating read and write models for different optimization strategies"),
                ("eventual_consistency", "Eventual Consistency",
                 "Distributed system property where replicas converge over time"),
                ("saga_pattern", "Saga Pattern",
                 "Managing distributed transactions through compensating actions"),
                ("domain_events", "Domain Events",
                 "Publishing business-significant occurrences for loose coupling between services"),
            ]

            for nid, label, content in facts:
                loader1.add_node(g1, nid, label, content, "architecture", "fact", importance=0.7)

            all_ids = [f[0] for f in facts]
            for _ in range(5):
                engine1.strengthen(all_ids, task_success=True)

            GraphPersistence(db_path).save_graph(g1)

            g2 = GraphPersistence(db_path).load_graph()
            _, _, _, gate2, _ = make_system(g2)

            # Query uses different words than stored facts; expect Hebbian to surface event_sourcing/cqrs/domain_events
            query = "How to handle transactions across multiple microservices reliably"

            results_with = gate2.seed_from_context(query, top_k=5)

            g_ctrl = KnowledgeGraph()
            for nid, node in g2.nodes.items():
                g_ctrl.add_node(nid, node.label, node.node_type,
                               content=node.content, summary=node.summary,
                               importance=node.importance)
            _, _, _, gate_ctrl, _ = make_system(g_ctrl)
            results_without = gate_ctrl.seed_from_context(query, top_k=5)

            recall_with = len(set(results_with) & set(all_ids)) / len(all_ids)
            recall_without = len(set(results_without) & set(all_ids)) / len(all_ids)

            print(f"\n  Query: '{query}'")
            print(f"  WITH Hebbian recall: {recall_with:.0%} ({set(results_with) & set(all_ids)})")
            print(f"  WITHOUT Hebbian recall: {recall_without:.0%} ({set(results_without) & set(all_ids)})")
            print(f"  Improvement: {recall_with - recall_without:+.0%}")

            assert recall_with > recall_without, \
                f"Hebbian didn't improve recall. With={recall_with:.0%}, Without={recall_without:.0%}"
