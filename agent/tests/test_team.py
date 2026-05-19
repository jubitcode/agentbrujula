import os
import sys

import pytest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "src"))

from research_agent.team import (
    AgentInfo,
    SubTask,
    compose_team,
    decompose_task,
    estimate_team_cost,
    build_execution_plan,
)


MOCK_AGENTS = [
    {
        "handle": "search-v2",
        "agent": "GSEARCH...",
        "capabilities": ["WEB_SEARCH", "WEB_READ"],
        "rate_stroops": 500_000,
        "rep_score": 8,
    },
    {
        "handle": "pdf-reader-pro",
        "agent": "GPDF...",
        "capabilities": ["PDF", "OCR"],
        "rate_stroops": 1_000_000,
        "rep_score": 10,
    },
    {
        "handle": "pdf-basic",
        "agent": "GPDF2...",
        "capabilities": ["PDF"],
        "rate_stroops": 500_000,
        "rep_score": 3,
    },
    {
        "handle": "synth-agent",
        "agent": "GSYNTH...",
        "capabilities": ["SYNTHESIS"],
        "rate_stroops": 2_000_000,
        "rep_score": 6,
    },
]


class TestComposeTeam:
    def test_picks_one_agent_per_capability(self):
        team = compose_team(["WEB_SEARCH", "PDF"], MOCK_AGENTS)
        handles = {a.handle for a in team}
        assert "search-v2" in handles
        # pdf-reader-pro (rep 10) wins over pdf-basic (rep 3)
        assert "pdf-reader-pro" in handles
        assert "pdf-basic" not in handles

    def test_deduplicates_agents(self):
        # WEB_SEARCH + WEB_READ both map to search-v2 — only one team slot
        team = compose_team(["WEB_SEARCH", "WEB_READ"], MOCK_AGENTS)
        assert len(team) == 1
        assert team[0].handle == "search-v2"

    def test_picks_highest_rep(self):
        team = compose_team(["PDF"], MOCK_AGENTS)
        assert len(team) == 1
        assert team[0].handle == "pdf-reader-pro"
        assert team[0].rep_score == 10

    def test_empty_capabilities_returns_empty(self):
        team = compose_team([], MOCK_AGENTS)
        assert team == []

    def test_no_matching_agents_returns_empty(self):
        team = compose_team(["QUANTUM_COMPUTING"], MOCK_AGENTS)
        assert team == []

    def test_case_insensitive_capability_match(self):
        team = compose_team(["web_search"], MOCK_AGENTS)
        assert len(team) == 1
        assert team[0].handle == "search-v2"


class TestDecomposeTask:
    def test_one_subtask_per_agent(self):
        team = [
            AgentInfo("search", "G1", ["SEARCH"], 500_000),
            AgentInfo("pdf", "G2", ["PDF"], 1_000_000),
        ]
        sub_tasks = decompose_task("research topic X", team)
        assert len(sub_tasks) == 2
        assert sub_tasks[0].agent_handle == "search"
        assert sub_tasks[1].agent_handle == "pdf"

    def test_last_subtask_depends_on_others(self):
        team = [
            AgentInfo("a", "G1", ["A"], 100),
            AgentInfo("b", "G2", ["B"], 100),
            AgentInfo("c", "G3", ["C"], 100),
        ]
        sub_tasks = decompose_task("task", team)
        assert sub_tasks[0].depends_on == []
        assert sub_tasks[1].depends_on == []
        assert set(sub_tasks[2].depends_on) == {"subtask-0", "subtask-1"}

    def test_single_agent_no_dependencies(self):
        team = [AgentInfo("solo", "G1", ["X"], 100)]
        sub_tasks = decompose_task("task", team)
        assert len(sub_tasks) == 1
        assert sub_tasks[0].depends_on == []


class TestEstimateTeamCost:
    def test_basic_estimate(self):
        team = [
            AgentInfo("a", "G1", ["X"], 1_000_000),
            AgentInfo("b", "G2", ["Y"], 2_000_000),
        ]
        sub_tasks = [
            SubTask("s1", "a", "do X", estimated_calls=2),
            SubTask("s2", "b", "do Y", estimated_calls=1),
        ]
        estimated, ceiling = estimate_team_cost(sub_tasks, team)
        # a: 1M*2 + b: 2M*1 = 4M stroops = 0.4 USDC; ceiling = 0.4 * 1.5
        assert estimated == pytest.approx(0.4)
        assert ceiling == pytest.approx(0.6)

    def test_zero_cost(self):
        team = [AgentInfo("free", "G1", ["X"], 0)]
        sub_tasks = [SubTask("s1", "free", "do X")]
        estimated, ceiling = estimate_team_cost(sub_tasks, team)
        assert estimated == 0
        assert ceiling == 0


class TestBuildExecutionPlan:
    def test_parallel_tasks(self):
        sub_tasks = [
            SubTask("a", "agent-a", "task A"),
            SubTask("b", "agent-b", "task B"),
        ]
        plan = build_execution_plan(sub_tasks)
        assert len(plan) == 1
        assert len(plan[0]) == 2

    def test_fan_in_creates_two_rounds(self):
        sub_tasks = [
            SubTask("a", "agent-a", "task A"),
            SubTask("b", "agent-b", "task B"),
            SubTask("c", "agent-c", "synthesize", depends_on=["a", "b"]),
        ]
        plan = build_execution_plan(sub_tasks)
        assert len(plan) == 2
        assert len(plan[0]) == 2  # a, b parallel
        assert len(plan[1]) == 1  # c after
        assert plan[1][0].id == "c"

    def test_sequential_chain(self):
        sub_tasks = [
            SubTask("a", "agent-a", "first"),
            SubTask("b", "agent-b", "second", depends_on=["a"]),
            SubTask("c", "agent-c", "third", depends_on=["b"]),
        ]
        plan = build_execution_plan(sub_tasks)
        assert len(plan) == 3
        assert plan[0][0].id == "a"
        assert plan[1][0].id == "b"
        assert plan[2][0].id == "c"

    def test_circular_dependency_raises(self):
        sub_tasks = [
            SubTask("a", "agent-a", "task A", depends_on=["b"]),
            SubTask("b", "agent-b", "task B", depends_on=["a"]),
        ]
        with pytest.raises(RuntimeError, match="Circular dependency"):
            build_execution_plan(sub_tasks)
