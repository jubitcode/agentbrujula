from __future__ import annotations

import os
from dataclasses import dataclass, field


@dataclass
class AgentInfo:
    handle: str
    public_key: str
    capabilities: list[str]
    rate_stroops: int
    rep_score: int = 0


@dataclass
class SubTask:
    id: str
    agent_handle: str
    action: str
    depends_on: list[str] = field(default_factory=list)
    estimated_calls: int = 1


def compose_team(
    required_capabilities: list[str],
    registry_agents: list[dict],
) -> list[AgentInfo]:
    """Pick the best agent per required capability based on REP score.

    Args:
        required_capabilities: e.g. ["web_search", "pdf_read", "synthesis"]
        registry_agents: list of dicts from list_active_full() with rep_score added

    Returns:
        One AgentInfo per capability (deduped — same agent can cover multiple caps).
    """
    team: dict[str, AgentInfo] = {}

    for cap in required_capabilities:
        candidates = [
            a
            for a in registry_agents
            if cap.upper() in [c.upper() for c in a.get("capabilities", [])]
        ]
        if not candidates:
            continue

        # Pick highest REP
        best = max(candidates, key=lambda a: a.get("rep_score", 0))
        handle = best["handle"]
        if handle not in team:
            team[handle] = AgentInfo(
                handle=handle,
                public_key=best.get("agent", ""),
                capabilities=best.get("capabilities", []),
                rate_stroops=best.get("rate_stroops", 0),
                rep_score=best.get("rep_score", 0),
            )

    return list(team.values())


def decompose_task(
    task_description: str,
    team: list[AgentInfo],
) -> list[SubTask]:
    """Decompose a task into sub-tasks matched to team agent capabilities.

    Each agent gets a role-specific action derived from its capabilities,
    not the raw task description with a capability prefix.
    Agents with identical capability sets are deduplicated.
    """
    sub_tasks = []
    seen_cap_sets: set[tuple[str, ...]] = set()

    for i, agent in enumerate(team):
        cap_key = tuple(sorted(agent.capabilities))
        if cap_key in seen_cap_sets:
            continue
        seen_cap_sets.add(cap_key)

        action = _build_agent_action(task_description, agent.capabilities)

        sub_tasks.append(
            SubTask(
                id=f"subtask-{i}",
                agent_handle=agent.handle,
                action=action,
                depends_on=[],
                estimated_calls=max(1, len(agent.capabilities)),
            )
        )

    if len(sub_tasks) > 1:
        sub_tasks[-1].depends_on = [st.id for st in sub_tasks[:-1]]

    return sub_tasks


def _build_agent_action(task: str, capabilities: list[str]) -> str:
    """Build a clean action string for an agent based on its capabilities."""
    cap_lower = [c.lower() for c in capabilities]

    if any(k in cap_lower for k in ("web_search", "search", "web_read")):
        return f"Search the web for: {task}"
    if any(k in cap_lower for k in ("pdf", "ocr", "extraction")):
        return f"Extract and analyze documents about: {task}"
    if any(k in cap_lower for k in ("synthesis", "summarize", "summarization")):
        return f"Synthesize findings about: {task}"
    if any(k in cap_lower for k in ("write", "content", "blog")):
        return f"Write a report on: {task}"
    if any(k in cap_lower for k in ("review", "critique")):
        return f"Review and critique: {task}"
    if any(k in cap_lower for k in ("research", "analysis")):
        return f"Research: {task}"
    if any(k in cap_lower for k in ("trend", "monitoring")):
        return f"Analyze trends in: {task}"

    return task


def estimate_team_cost(
    sub_tasks: list[SubTask],
    team: list[AgentInfo],
) -> tuple[float, float]:
    """Estimate total cost and budget ceiling for a team.

    Returns:
        (estimated_cost_usdc, budget_ceiling_usdc)
    """
    agent_rates = {a.handle: a.rate_stroops for a in team}
    margin = float(os.getenv("BUDGET_MARGIN", "1.5"))

    total_stroops = 0
    for st in sub_tasks:
        rate = agent_rates.get(st.agent_handle, 0)
        total_stroops += rate * st.estimated_calls

    estimated = total_stroops / 10_000_000  # USDC has 7 decimals
    ceiling = round(estimated * margin, 7)
    return estimated, ceiling


def build_execution_plan(sub_tasks: list[SubTask]) -> list[list[SubTask]]:
    """Build execution rounds from sub-tasks using topological sort.

    Returns:
        List of rounds. Each round is a list of sub-tasks that can execute in parallel.
    """
    task_map = {st.id: st for st in sub_tasks}
    completed: set[str] = set()
    rounds: list[list[SubTask]] = []

    remaining = set(task_map.keys())

    while remaining:
        ready = [
            tid
            for tid in remaining
            if all(dep in completed for dep in task_map[tid].depends_on)
        ]
        if not ready:
            raise RuntimeError(
                f"Circular dependency detected in sub-tasks: {remaining}"
            )

        round_tasks = [task_map[tid] for tid in ready]
        rounds.append(round_tasks)

        for tid in ready:
            completed.add(tid)
            remaining.remove(tid)

    return rounds
