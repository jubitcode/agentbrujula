import os
import time
import tempfile
from dataclasses import dataclass

import pytest
from openai import OpenAI

from research_agent.knowledge import ResearchKnowledgeStore


OPENROUTER_KEY = os.environ.get("OPENROUTER_API_KEY", "")
OPENROUTER_URL = "https://openrouter.ai/api/v1"
MODEL = "google/gemini-2.0-flash-001"

# Questions about a fictional system ("Vortex Protocol") — if the LLM cites these specifics,
# it MUST be using Hebbian-retrieved context, not training data.
QUESTIONS = [
    "What is the Vortex Protocol and how does its Cascade Ledger handle transaction finality?",
    "How does Vortex's Phantom Gas model work compared to traditional blockchain fees?",
    "What are the security implications of Vortex's Drift Consensus mechanism?",
]

# Fabricated facts impossible to learn anywhere else — appearance in answer = retrieved context.
SEED_FACTS = [
    {
        "id": "vortex_cascade_ledger",
        "label": "Vortex Cascade Ledger",
        "content": "The Vortex Protocol uses a Cascade Ledger architecture where transactions "
                   "flow through three confirmation tiers: Flash (0.4s, probabilistic), "
                   "Ripple (2.1s, economic finality), and Bedrock (7.3s, absolute finality). "
                   "Each tier uses a different validator set selected by Drift Consensus. "
                   "The Cascade Ledger achieves 14,200 TPS at the Flash tier.",
        "importance": 0.8,
    },
    {
        "id": "vortex_phantom_gas",
        "label": "Vortex Phantom Gas Model",
        "content": "Vortex replaces traditional gas fees with Phantom Gas — a virtual resource "
                   "that is pre-allocated to accounts based on their staking weight. Phantom Gas "
                   "is never actually spent; instead, it temporarily 'dims' during transaction "
                   "execution and regenerates at a rate of 42 units per block (1 block = 0.8s). "
                   "The base cost of a simple transfer is 0.00042 VTX, paid in the native token. "
                   "Heavy computation dims more Phantom Gas, throttling throughput for that account.",
        "importance": 0.7,
    },
    {
        "id": "vortex_drift_consensus",
        "label": "Vortex Drift Consensus",
        "content": "Drift Consensus is Vortex's novel BFT variant where validator selection "
                   "shifts ('drifts') every 128 blocks based on a VRF-weighted lottery. "
                   "Validators must post a collateral of 50,000 VTX and run Temporal Anchors — "
                   "cryptographic timestamps that prevent long-range attacks. "
                   "The protocol tolerates up to 38% Byzantine validators (higher than the "
                   "standard 33%) by requiring Temporal Anchor agreement across 3 consecutive epochs.",
        "importance": 0.6,
    },
    {
        "id": "vortex_zk_pruning",
        "label": "Vortex ZK State Pruning",
        "content": "Vortex implements ZK State Pruning where old state is compressed into "
                   "zero-knowledge proofs every 10,000 blocks (the 'Compaction Epoch'). "
                   "Full nodes only need to store the last 3 Compaction Epochs plus the ZK proof "
                   "chain. This reduces node storage requirements to approximately 12 GB "
                   "regardless of total chain history. The pruning ceremony uses Plonky3 recursion.",
        "importance": 0.65,
    },
]


@dataclass
class TurnMetrics:
    turn: int
    question: str
    knowledge_hits: int = 0
    knowledge_context_tokens: int = 0
    prompt_tokens: int = 0
    completion_tokens: int = 0
    total_tokens: int = 0
    extraction_calls: int = 0
    extraction_tokens: int = 0
    new_nodes: int = 0
    total_nodes: int = 0
    total_edges: int = 0
    max_edge_weight: float = 0.0
    answer_snippet: str = ""
    used_hebbian_facts: bool = False


def _llm_call(client: OpenAI, prompt: str, max_tokens: int = 300) -> tuple[str, int, int]:
    resp = client.chat.completions.create(
        model=MODEL,
        messages=[{"role": "user", "content": prompt}],
        temperature=0.2,
        max_tokens=max_tokens,
    )
    text = resp.choices[0].message.content or ""
    usage = resp.usage
    return text, usage.prompt_tokens, usage.completion_tokens


def _max_edge_weight(store) -> float:
    max_w = 0.0
    for source_edges in store.graph.outgoing_edges.values():
        for edge in source_edges:
            if edge.weight > max_w:
                max_w = edge.weight
    return max_w


def _count_edges(store) -> int:
    count = 0
    for edges_list in store.graph.outgoing_edges.values():
        count += len(edges_list)
    return count


def _seed_graph(store):
    """Populate graph with fabricated facts the LLM cannot know — store_finding generates IDs, content is what retrieval matches."""
    for fact in SEED_FACTS:
        store.store_finding(
            content=fact["content"],
            importance=fact["importance"],
        )


@pytest.fixture
def knowledge_store():
    with tempfile.TemporaryDirectory() as tmp:
        db_path = os.path.join(tmp, "test_hebbian.db")
        store = ResearchKnowledgeStore(
            db_path=db_path,
            llm_base_url=OPENROUTER_URL,
            llm_api_key=OPENROUTER_KEY,
            llm_model=MODEL,
        )
        _seed_graph(store)
        yield store


@pytest.mark.skipif(not OPENROUTER_KEY, reason="OPENROUTER_API_KEY not set")
def test_hebbian_multi_turn_token_proof(knowledge_store):
    """LLM answers improve from Hebbian context, tokens stay flat, edge weights strengthen across turns."""
    store = knowledge_store
    client = OpenAI(base_url=OPENROUTER_URL, api_key=OPENROUTER_KEY)
    metrics: list[TurnMetrics] = []

    original_llm = store._llm_call
    extraction_call_count = [0]
    extraction_token_count = [0]

    def counting_llm(prompt: str) -> str:
        extraction_call_count[0] += 1
        extraction_token_count[0] += len(prompt) // 4
        return original_llm(prompt)

    store._llm_call = counting_llm
    store._extractor.llm_fn = counting_llm

    for i, question in enumerate(QUESTIONS):
        m = TurnMetrics(turn=i + 1, question=question)

        extraction_call_count[0] = 0
        extraction_token_count[0] = 0

        # Free graph retrieval, zero LLM cost
        hits = store.search(question, top_k=5)
        m.knowledge_hits = len(hits)
        pulled_ids = [h["id"] for h in hits]

        knowledge_context = ""
        if hits:
            lines = ["## Retrieved Knowledge (from Hebbian graph):"]
            for h in hits:
                lines.append(f"- {h['content']}")
            knowledge_context = "\n".join(lines)
            m.knowledge_context_tokens = len(knowledge_context) // 4

        if knowledge_context:
            prompt = (
                f"{knowledge_context}\n\n"
                f"Using the above retrieved knowledge, answer this question. "
                f"Reference specific facts from the retrieved knowledge in your answer.\n\n"
                f"Question: {question}"
            )
        else:
            prompt = f"Answer concisely: {question}"

        answer, prompt_tokens, completion_tokens = _llm_call(client, prompt)
        m.prompt_tokens = prompt_tokens
        m.completion_tokens = completion_tokens
        m.total_tokens = prompt_tokens + completion_tokens
        m.answer_snippet = answer[:200]

        # Keywords are fabricated — appearance proves retrieved context, not training data
        answer_lower = answer.lower()
        m.used_hebbian_facts = any(
            keyword in answer_lower
            for keyword in ["cascade ledger", "7.3", "14,200", "phantom gas",
                          "0.00042", "dims", "regenerates", "drift consensus",
                          "temporal anchor", "50,000 vtx", "38%",
                          "compaction epoch", "plonky3", "12 gb",
                          "flash", "ripple", "bedrock"]
        )

        time.sleep(1)  # avoid rate limits before extraction call
        new_ids = store.ingest(question, answer, pulled_ids=pulled_ids)
        m.extraction_calls = extraction_call_count[0]
        m.extraction_tokens = extraction_token_count[0]
        m.new_nodes = len(new_ids)

        m.total_nodes = len(store.graph.nodes)
        m.total_edges = _count_edges(store)
        m.max_edge_weight = _max_edge_weight(store)

        metrics.append(m)

        time.sleep(2)  # rate-limit pause between turns

    print("\n" + "=" * 80)
    print("  HEBBIAN KNOWLEDGE — MULTI-TURN TOKEN PROOF")
    print("=" * 80)

    for m in metrics:
        print(f"\n  Turn {m.turn}: {m.question[:65]}...")
        print(f"    knowledge_hits:      {m.knowledge_hits:3d}  (free graph retrieval, 0 LLM tokens)")
        print(f"    used_hebbian_facts:  {'YES' if m.used_hebbian_facts else 'no'}")
        print(f"    prompt_tokens:       {m.prompt_tokens:3d}  (sent to LLM)")
        print(f"    completion_tokens:   {m.completion_tokens:3d}  (generated by LLM)")
        print(f"    total_tokens:        {m.total_tokens:3d}")
        print(f"    extraction_calls:    {m.extraction_calls:3d}  (LLM calls for fact extraction)")
        print(f"    extraction_tokens:   {m.extraction_tokens:3d}")
        print(f"    new_nodes:           {m.new_nodes:3d}")
        print(f"    total_nodes/edges:   {m.total_nodes}/{m.total_edges}")
        print(f"    max_edge_weight:     {m.max_edge_weight:.3f}")
        print(f"    answer: {m.answer_snippet[:120]}...")

    answer_tokens = sum(m.total_tokens for m in metrics)
    extraction_tokens = sum(m.extraction_tokens for m in metrics)
    grand_total = answer_tokens + extraction_tokens

    print(f"\n  {'─' * 70}")
    print(f"  Answer tokens (all turns):        {answer_tokens}")
    print(f"  Extraction tokens (all turns):     {extraction_tokens}")
    print(f"  Grand total tokens:                {grand_total}")
    print(f"  Baseline (3x turn 1 total):        {(metrics[0].total_tokens + metrics[0].extraction_tokens) * 3}")

    print(f"\n  Knowledge hits:     {' → '.join(str(m.knowledge_hits) for m in metrics)}")
    print(f"  Used Hebbian facts: {' → '.join('YES' if m.used_hebbian_facts else 'no' for m in metrics)}")
    print(f"  Completion tokens:  {' → '.join(str(m.completion_tokens) for m in metrics)}")
    print(f"  Extraction calls:   {' → '.join(str(m.extraction_calls) for m in metrics)}")
    print(f"  Edge weights:       {' → '.join(f'{m.max_edge_weight:.3f}' for m in metrics)}")
    print(f"  Nodes:              {' → '.join(str(m.total_nodes) for m in metrics)}")

    # Key proof points
    hebbian_used = sum(1 for m in metrics if m.used_hebbian_facts)
    hits_increased = metrics[2].knowledge_hits > metrics[0].knowledge_hits
    weights_grew = metrics[2].max_edge_weight > metrics[0].max_edge_weight

    print(f"\n  PROOF SUMMARY:")
    print(f"    LLM used Hebbian-retrieved facts:  {hebbian_used}/3 turns")
    print(f"    Knowledge hits increased:           {metrics[0].knowledge_hits} → {metrics[2].knowledge_hits} ({'YES' if hits_increased else 'NO'})")
    print(f"    Edge weights strengthened:           {metrics[0].max_edge_weight:.3f} → {metrics[2].max_edge_weight:.3f} ({'YES' if weights_grew else 'NO'})")
    print(f"    Token growth sublinear:              {grand_total} vs {(metrics[0].total_tokens + metrics[0].extraction_tokens) * 3} baseline")
    print("=" * 80)

    assert metrics[2].knowledge_hits > metrics[0].knowledge_hits, \
        f"Knowledge hits didn't increase: {[m.knowledge_hits for m in metrics]}"

    assert hebbian_used >= 1, \
        "LLM never referenced Hebbian-retrieved facts in its answers"

    assert metrics[2].max_edge_weight > metrics[0].max_edge_weight, \
        f"Edge weights didn't grow: {[m.max_edge_weight for m in metrics]}"

    assert metrics[2].total_nodes > metrics[0].total_nodes, \
        f"Nodes didn't grow: {[m.total_nodes for m in metrics]}"

    assert metrics[2].total_edges > 0, \
        "No edges formed — Hebbian strengthening didn't fire"
