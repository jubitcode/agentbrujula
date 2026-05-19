"""hire_team handler must enforce the same caps as the estimator —
the x402 gate only verifies signature authenticity, not request-body consistency,
so an attacker could replay a small-payment signature with a body containing 100 capabilities."""

import sys
from pathlib import Path
from unittest.mock import patch

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "src"))


@pytest.fixture(autouse=True)
def reset_limiter():
    """Reset and disable the limiter — slowapi needs a real starlette.Request, but we bypass routing
    entirely to isolate the handler's own cap-check logic."""
    import server as srv
    srv.limiter.reset()
    was_enabled = srv.limiter.enabled
    srv.limiter.enabled = False
    yield
    srv.limiter.enabled = was_enabled


@pytest.fixture
def hire_team_call():
    """Invoke hire_team directly so the x402_gate Depends doesn't run, isolating the handler's own cap check."""
    from server import hire_team, HireTeamRequest
    return hire_team, HireTeamRequest


@pytest.fixture
def mock_request():
    """Minimal stub satisfying slowapi's introspection."""
    from unittest.mock import MagicMock
    req = MagicMock()
    req.client.host = "127.0.0.1"
    req.scope = {"type": "http"}
    req.headers = {}
    req.state = MagicMock()
    return req


class TestCapabilitiesCapInHandler:
    def test_oversized_capabilities_rejected_in_handler(self, hire_team_call, mock_request):
        from fastapi import HTTPException
        hire_team, HireTeamRequest = hire_team_call
        oversized = HireTeamRequest(
            task="x",
            capabilities=[f"cap-{i}" for i in range(11)],
            budget_xlm=1000.0,
        )
        with pytest.raises(HTTPException) as exc:
            hire_team(oversized, mock_request)
        assert exc.value.status_code == 413
        assert "too many capabilities" in exc.value.detail.lower()

    def test_at_cap_boundary_accepted(self, hire_team_call, mock_request):
        """Exactly MAX_TEAM_CAPABILITIES (10) is allowed."""
        import server as srv
        from server import HireTeamRequest

        class FakeAgent:
            def __init__(self, h):
                self.handle = h
                self.public_key = "G" + h
                self.capabilities = []
                self.rate_stroops = 100

        with patch.object(srv, "list_active_full", return_value=[]):
            with patch.object(srv, "compose_team", return_value=[FakeAgent("a")]):
                with patch.object(srv, "decompose_task", return_value=[]):
                    with patch.object(srv, "estimate_team_cost", return_value=(0.30, 0.45)):
                        with patch.object(srv, "build_execution_plan", return_value=[]):
                            with patch.object(srv, "_load_orchestrator_kp", return_value=None):
                                req = HireTeamRequest(
                                    task="x",
                                    capabilities=[f"cap-{i}" for i in range(10)],
                                    budget_xlm=10.0,
                                )
                                try:
                                    result = srv.hire_team(req, mock_request)
                                    assert "task_id" in result or "team" in result
                                except Exception as e:
                                    # Downstream failure from mocks is OK — just not a 413 from the cap check
                                    if hasattr(e, "status_code"):
                                        assert e.status_code != 413


class TestTeamSizeCapInHandler:
    def test_team_size_over_cap_rejected_in_handler(self, hire_team_call, mock_request):
        """compose_team returning 11 agents must yield 413 from the handler."""
        from fastapi import HTTPException
        import server as srv
        from server import HireTeamRequest

        class FakeAgent:
            def __init__(self, h):
                self.handle = h
                self.public_key = "G" + h
                self.capabilities = []
                self.rate_stroops = 100

        fake_team = [FakeAgent(f"a{i}") for i in range(11)]

        with patch.object(srv, "list_active_full", return_value=[]):
            with patch.object(srv, "compose_team", return_value=fake_team):
                req = HireTeamRequest(
                    task="x",
                    capabilities=["cap-1", "cap-2"],
                    budget_xlm=10.0,
                )
                with pytest.raises(HTTPException) as exc:
                    srv.hire_team(req, mock_request)
                assert exc.value.status_code == 413
                assert "team too large" in exc.value.detail.lower()
