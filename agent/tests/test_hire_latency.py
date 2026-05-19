import os
import sys
import time
from pathlib import Path
from unittest.mock import patch, MagicMock

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "src"))

# X402_ENABLED may already be set by sibling modules; mock x402_gate directly instead of relying on env
os.environ.setdefault("X402_ENABLED", "false")

from fastapi.testclient import TestClient
from api_test_app import app

MOCK_TOOLS = [{
    "handle": "latency-agent",
    "agent": "GABCDEF123456",
    "capabilities": ["research"],
    "model_tier": "SONNET",
    "rate_stroops": 1_000_000,
    "max_tokens": 4096,
    "active": True,
    "url": "http://localhost:3001/invoke",
    "regis": 1000,
}]


@pytest.fixture
def client():
    return TestClient(app)


def _slow_create_escrow(*args, **kwargs):
    time.sleep(0.2)  # simulate 2 RPC calls
    return ("mock_balance_id", "mock_escrow_tx")


def _slow_claim_task(*args, **kwargs):
    time.sleep(0.4)  # simulate 4 RPC calls
    return "mock_claim_hash"


class TestHireLatencyGuards:
    @patch("research_agent.x402_config.X402_ENABLED", False)
    @patch("server.submit_claim_task", side_effect=_slow_claim_task)
    @patch("server.create_escrow", side_effect=_slow_create_escrow)
    @patch("server._load_orchestrator_kp")
    @patch("server.list_active_full", return_value=MOCK_TOOLS)
    def test_hire_responds_under_2_seconds(self, mock_list, mock_kp, mock_escrow, mock_claim, client):
        from stellar_sdk import Keypair
        mock_kp.return_value = Keypair.random()

        start = time.monotonic()
        resp = client.post("/api/hire", json={
            "agent_handle": "latency-agent",
            "task": "test task",
            "budget_xlm": 100.0,
        })
        elapsed = time.monotonic() - start

        assert resp.status_code == 200
        assert elapsed < 2.0, f"Hire took {elapsed:.2f}s — exceeds 2s threshold"

    @patch("research_agent.x402_config.X402_ENABLED", False)
    @patch("server.submit_claim_task", side_effect=RuntimeError("RPC timeout"))
    @patch("server.create_escrow", side_effect=_slow_create_escrow)
    @patch("server._load_orchestrator_kp")
    @patch("server.list_active_full", return_value=MOCK_TOOLS)
    def test_claim_task_failure_doesnt_block_response(self, mock_list, mock_kp, mock_escrow, mock_claim, client):
        from stellar_sdk import Keypair
        mock_kp.return_value = Keypair.random()

        start = time.monotonic()
        resp = client.post("/api/hire", json={
            "agent_handle": "latency-agent",
            "task": "test task",
            "budget_xlm": 100.0,
        })
        elapsed = time.monotonic() - start

        assert resp.status_code == 200, f"Hire should succeed even when claim_task fails: {resp.text}"
        # Faster since claim_task fails immediately (no 400ms delay)
        assert elapsed < 1.0, f"Hire took {elapsed:.2f}s — claim_task failure shouldn't add delay"

    @patch("research_agent.x402_config.X402_ENABLED", False)
    @patch("server.submit_claim_task")
    @patch("server.create_escrow", return_value=("bid", "tx"))
    @patch("server._load_orchestrator_kp")
    @patch("server.list_active_full", return_value=MOCK_TOOLS)
    def test_hire_returns_valid_response_shape(self, mock_list, mock_kp, mock_escrow, mock_claim, client):
        from stellar_sdk import Keypair
        mock_kp.return_value = Keypair.random()

        resp = client.post("/api/hire", json={
            "agent_handle": "latency-agent",
            "task": "test task",
            "budget_xlm": 100.0,
        })

        assert resp.status_code == 200
        data = resp.json()
        assert "task_id" in data
        assert len(data["task_id"]) == 32  # uuid4 hex
        assert "deadline_unix" in data

    @patch("research_agent.x402_config.X402_ENABLED", False)
    @patch("server.submit_claim_task")
    @patch("server.create_escrow", return_value=(None, None))
    @patch("server._load_orchestrator_kp", return_value=None)
    @patch("server.list_active_full", return_value=MOCK_TOOLS)
    def test_hire_without_orchestrator_is_fast(self, mock_list, mock_kp, mock_escrow, mock_claim, client):
        start = time.monotonic()
        resp = client.post("/api/hire", json={
            "agent_handle": "latency-agent",
            "task": "test task",
            "budget_xlm": 100.0,
        })
        elapsed = time.monotonic() - start

        assert resp.status_code == 200
        assert elapsed < 0.5, f"No-key hire took {elapsed:.2f}s — should be <500ms"
        mock_escrow.assert_not_called()
        mock_claim.assert_not_called()
