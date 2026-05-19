"""`_estimate_team_cost` must cap capabilities count, team size, and estimated USDC.

Sibling to the A2A cap — without these guards a growing registry lets /api/hire-team generate
unbounded cost estimates that the operator pays."""

import sys
from pathlib import Path
from unittest.mock import patch

import pytest
from fastapi import HTTPException

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "src"))


def _estimator():
    import server as srv
    return srv._estimate_team_cost


class TestCapabilitiesCountCap:
    def test_reasonable_capability_count_accepted(self):
        import server as srv
        with patch.object(srv, "list_active_full", return_value=[]):
            try:
                result = _estimator()({
                    "task": "x", "capabilities": ["research"], "budget_xlm": 1,
                })
                # Empty-registry fallback returns $0.30
                assert float(result["estimated_cost"]) <= 5.0
            except HTTPException as e:
                pytest.fail(f"Should not raise on single capability: {e}")

    def test_oversized_capability_list_rejected(self):
        oversized = [f"cap-{i}" for i in range(11)]
        with pytest.raises(HTTPException) as exc:
            _estimator()({"task": "x", "capabilities": oversized})
        assert exc.value.status_code == 413
        assert "too many capabilities" in exc.value.detail.lower()


class TestTeamSizeCap:
    def test_team_size_capped(self, monkeypatch):
        """compose_team returning 11 agents (one above cap) must yield 413."""
        import server as srv
        # 11 distinct capabilities, each maps to a distinct fake agent
        capabilities = [f"skill-{i}" for i in range(10)]

        class FakeAgent:
            def __init__(self, handle):
                self.handle = handle
                self.capabilities = []
                self.rate_stroops = 1_000_000

        fake_team = [FakeAgent(f"agent-{i}") for i in range(11)]

        with patch.object(srv, "list_active_full", return_value=[]):
            with patch.object(srv, "compose_team", return_value=fake_team):
                with pytest.raises(HTTPException) as exc:
                    srv._estimate_team_cost({
                        "task": "x", "capabilities": capabilities,
                    })
                assert exc.value.status_code == 413
                assert "team too large" in exc.value.detail.lower()


class TestEstimateCeiling:
    def test_estimate_clamped_at_ceiling(self, monkeypatch):
        import server as srv

        class FakeAgent:
            def __init__(self, handle):
                self.handle = handle
                self.capabilities = []
                self.rate_stroops = 999_999_999  # deliberately absurd

        fake_team = [FakeAgent("a"), FakeAgent("b")]

        with patch.object(srv, "list_active_full", return_value=[]):
            with patch.object(srv, "compose_team", return_value=fake_team):
                with patch.object(srv, "decompose_task", return_value=[]):
                    with patch.object(srv, "estimate_team_cost", return_value=(999.0, 1500.0)):
                        result = srv._estimate_team_cost({
                            "task": "x", "capabilities": ["a", "b"],
                        })
        assert float(result["estimated_cost"]) <= 5.0


class TestEnvOverride:
    def test_capabilities_override(self, monkeypatch):
        import server as srv
        monkeypatch.setattr(srv, "_MAX_TEAM_CAPABILITIES", 3)
        with pytest.raises(HTTPException) as exc:
            srv._estimate_team_cost({
                "task": "x", "capabilities": ["a", "b", "c", "d"],
            })
        assert exc.value.status_code == 413

    def test_ceiling_override(self, monkeypatch):
        import server as srv

        class FakeAgent:
            def __init__(self, h):
                self.handle = h
                self.capabilities = []
                self.rate_stroops = 1_000_000

        monkeypatch.setattr(srv, "_MAX_TEAM_ESTIMATED_USDC", 0.50)
        with patch.object(srv, "list_active_full", return_value=[]):
            with patch.object(srv, "compose_team", return_value=[FakeAgent("a")]):
                with patch.object(srv, "decompose_task", return_value=[]):
                    with patch.object(srv, "estimate_team_cost", return_value=(10.0, 15.0)):
                        result = srv._estimate_team_cost({
                            "task": "x", "capabilities": ["a"],
                        })
        assert float(result["estimated_cost"]) <= 0.50
