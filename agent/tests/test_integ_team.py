import base64
import json
import time

import pytest

from conftest import requires_orchestrator


pytestmark = [requires_orchestrator]


class TestHireTeam:
    def _get_real_capabilities(self, api_client) -> list[str]:
        res = api_client.get("/api/registry")
        if res.status_code != 200:
            return []
        agents = res.json().get("agents", [])
        caps = set()
        for a in agents:
            caps.update(a.get("capabilities", []))
        return list(caps)

    def test_hire_team_returns_402_with_team_estimate(self, api_client):
        caps = self._get_real_capabilities(api_client)
        if len(caps) < 1:
            pytest.skip("No capabilities found in testnet registry")

        res = api_client.post("/api/hire-team", json={
            "task": "research and summarize",
            "capabilities": caps[:2],
            "budget_xlm": 10000.0,
        })
        assert res.status_code == 402
        assert "PAYMENT-REQUIRED" in res.headers

        payload = json.loads(base64.b64decode(res.headers["PAYMENT-REQUIRED"]))
        if "estimated_cost" in payload:
            assert "budget_ceiling" in payload
            assert float(payload["budget_ceiling"]) >= float(payload["estimated_cost"])

    def test_hire_team_with_ack_returns_team_composition(self, api_client):
        caps = self._get_real_capabilities(api_client)
        if len(caps) < 1:
            pytest.skip("No capabilities found")

        ack_sig = base64.b64encode(json.dumps({"ack": True}).encode()).decode()

        res = api_client.post(
            "/api/hire-team",
            json={
                "task": "research topic and extract data",
                "capabilities": caps[:2],
                "budget_xlm": 10000.0,
            },
            headers={"PAYMENT-SIGNATURE": ack_sig},
        )

        if res.status_code == 404:
            pytest.skip("No matching agents for these capabilities")

        assert res.status_code == 200
        data = res.json()

        assert "task_id" in data
        assert "team" in data
        assert len(data["team"]) >= 1
        assert "sub_tasks" in data
        assert len(data["sub_tasks"]) >= 1
        assert "execution_plan" in data
        assert len(data["execution_plan"]) >= 1
        assert "estimated_cost_usdc" in data
        assert "budget_ceiling_usdc" in data

        for agent in data["team"]:
            assert "handle" in agent
            assert "capabilities" in agent
            assert "rate_xlm" in agent
            assert agent["rate_xlm"] > 0  # rate must come from the live registry

    def test_hire_team_no_matching_capability_404(self, api_client):
        ack_sig = base64.b64encode(json.dumps({"ack": True}).encode()).decode()

        res = api_client.post(
            "/api/hire-team",
            json={
                "task": "do impossible thing",
                "capabilities": ["QUANTUM_TELEPORTATION", "TIME_TRAVEL"],
                "budget_xlm": 10000.0,
            },
            headers={"PAYMENT-SIGNATURE": ack_sig},
        )
        assert res.status_code == 404

    def test_team_execution_plan_respects_dependencies(self, api_client):
        caps = self._get_real_capabilities(api_client)
        if len(caps) < 2:
            pytest.skip("Need 2+ capabilities for team test")

        ack_sig = base64.b64encode(json.dumps({"ack": True}).encode()).decode()
        res = api_client.post(
            "/api/hire-team",
            json={
                "task": "multi-step research",
                "capabilities": caps[:3],
                "budget_xlm": 10000.0,
            },
            headers={"PAYMENT-SIGNATURE": ack_sig},
        )
        if res.status_code != 200:
            pytest.skip("No matching team")

        data = res.json()
        plan = data["execution_plan"]

        # decompose_task creates fan-in: round 1 = parallel tasks, round 2 = synthesis
        if len(data["team"]) >= 2:
            assert len(plan) >= 2, "Multi-agent team should have 2+ execution rounds"

        # Every sub-task ID appears exactly once across all rounds
        all_ids = [tid for round_ids in plan for tid in round_ids]
        sub_task_ids = [st["id"] for st in data["sub_tasks"]]
        assert sorted(all_ids) == sorted(sub_task_ids)
