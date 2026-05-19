from __future__ import annotations

from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from .knowledge import ResearchKnowledgeStore

CONFIDENCE_THRESHOLD = 0.7


def query_hebbian(
    knowledge: ResearchKnowledgeStore | None,
    capabilities: list[str],
) -> tuple[str | None, float]:
    """Query Hebbian memory for a recommended agent handle.

    Searches for capability-related nodes. If any result came from an agent
    (source starts with "agent:"), extract the handle and return it with confidence.
    Returns (handle, confidence) if confidence >= 0.7, else (None, 0.0).
    """
    if knowledge is None:
        return None, 0.0

    query = " ".join(capabilities)
    try:
        results = knowledge.search(query, top_k=10)
    except Exception:
        return None, 0.0

    if not results:
        return None, 0.0

    agent_results = [r for r in results if r.get("source", "").startswith("agent:")]

    if agent_results:
        best = max(agent_results, key=lambda r: r.get("confidence", 0.0))
        confidence = best.get("confidence", 0.0)
        if confidence >= CONFIDENCE_THRESHOLD:
            handle = best["source"].replace("agent:", "")
            return handle, confidence

    best = max(results, key=lambda r: r.get("confidence", 0.0))
    confidence = best.get("confidence", 0.0)
    if confidence >= CONFIDENCE_THRESHOLD:
        return best.get("label") or best.get("id"), confidence

    return None, 0.0


def reinforce_hebbian(
    knowledge: ResearchKnowledgeStore | None,
    capability: str,
    handle: str,
) -> None:
    """Strengthen the association between a capability pattern and agent handle."""
    if knowledge is None:
        return
    try:
        knowledge.ingest(question=capability, answer=handle)
    except Exception:
        pass


def store_output(
    knowledge: ResearchKnowledgeStore | None,
    task: str,
    handle: str,
    output: str,
    capabilities: list[str],
) -> str | None:
    """Ingest a compliant agent output into the Hebbian knowledge graph.

    Uses knowledge.ingest() to extract structured facts via LLM,
    then stores a capability→handle association as a findable node.
    Returns the association node_id, or None.
    """
    if knowledge is None or not output:
        return None
    try:
        cap_key = " ".join(capabilities)

        knowledge.ingest(
            question=f"{cap_key}: {task}",
            answer=output,
        )

        node_id = knowledge.store_finding(
            content=f"{handle} agent completed {cap_key} task: {task[:100]}",
            source_url=f"agent:{handle}",
            importance=0.85,
            domain=cap_key,
        )

        return node_id
    except Exception:
        return None


def get_personality_prompt(knowledge: ResearchKnowledgeStore | None) -> str:
    """Generate a personality-aware system prompt section from the knowledge graph.

    Uses IntrospectionEngine (zero LLM calls) to analyze graph topology,
    then PersonalitySynthesizer to produce a natural language personality.
    Returns empty string if knowledge is unavailable or graph is too small.
    """
    if knowledge is None:
        return ""
    try:
        from .hebbian.introspection import IntrospectionEngine
        from .hebbian.personality import (
            PersonalitySynthesizer,
            build_personality_prompt,
        )

        graph = knowledge.graph
        if len(graph.nodes) < 5:
            return ""  # Not enough data for meaningful personality

        engine = IntrospectionEngine(graph)
        model = engine.compute_self_model()

        synth = PersonalitySynthesizer()
        snapshot = synth.synthesize(model)

        return build_personality_prompt(snapshot)
    except Exception:
        return ""
