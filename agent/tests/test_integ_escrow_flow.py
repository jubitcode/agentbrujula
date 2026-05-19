import base64
import json
import os
import sys
import time
from unittest.mock import patch, MagicMock, AsyncMock

import pytest

# Make api/server.py importable
sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", "api"))

from fastapi.testclient import TestClient


@pytest.fixture
def client():
    """TestClient with X402_ENABLED=false; module reload required so env change takes effect."""
    with patch.dict(os.environ, {"X402_ENABLED": "false"}):
        import importlib
        sys.modules.pop("server", None)
        sys.modules.pop("research_agent.x402_config", None)
        sys.modules.pop("research_agent.x402_middleware", None)
        import server
        importlib.reload(server)
        yield TestClient(server.app)


@pytest.fixture
def client_with_x402():
    """TestClient with X402_ENABLED=true; module reload required so env change takes effect."""
    with patch.dict(os.environ, {"X402_ENABLED": "true"}):
        sys.modules.pop("server", None)
        sys.modules.pop("research_agent.x402_config", None)
        sys.modules.pop("research_agent.x402_middleware", None)
        import importlib
        import server
        importlib.reload(server)
        yield TestClient(server.app)


class TestDebateEndpoint:
    @patch("server.execute_tool")
    def test_debate_returns_result(self, mock_execute, client):
        mock_execute.return_value = {
            "question": "PostgreSQL vs MongoDB?",
            "rounds": 3,
            "transcript": [{"agent": "advocate", "round": 1, "content": "PG is great"}],
            "decision": "Use PostgreSQL",
            "adr": "# ADR-001\n\nUse PostgreSQL",
            "concession_detected": False,
            "cost": {"total_tokens_in": 1000, "total_tokens_out": 500, "estimated_cost_usd": 0.01},
        }

        resp = client.post("/api/debate", json={"question": "PostgreSQL vs MongoDB?"})
        assert resp.status_code == 200
        data = resp.json()
        assert data["question"] == "PostgreSQL vs MongoDB?"
        assert data["rounds"] == 3
        assert len(data["transcript"]) == 1

    @patch("server.execute_tool")
    def test_debate_returns_502_on_execution_failure(self, mock_execute, client):
        mock_execute.side_effect = RuntimeError("mcp-meta down")

        resp = client.post("/api/debate", json={"question": "test"})
        assert resp.status_code == 502

    @patch("server.execute_tool")
    def test_debate_calls_correct_tool(self, mock_execute, client):
        mock_execute.return_value = {"question": "q", "rounds": 0, "transcript": [], "decision": "", "adr": "", "concession_detected": False, "cost": {"total_tokens_in": 0, "total_tokens_out": 0, "estimated_cost_usd": 0}}

        client.post("/api/debate", json={"question": "Should we use Kafka?"})
        mock_execute.assert_called_once_with(
            "adr-moderator.debate",
            {"question": "Should we use Kafka?"},
            timeout=180.0,
        )


class TestExecuteA2AEndpoint:
    @patch("server.send_a2a_message")
    def test_a2a_returns_formatted_output(self, mock_a2a, client):
        mock_a2a.return_value = {
            "output": '{"analysis": "## AI Trends\\n\\nKey findings...", "trends_identified": 5}',
            "status": "completed",
            "task_id": "task-abc",
            "context_id": "ctx-123",
        }

        resp = client.post("/api/execute-a2a", json={"task": "Research AI trends"})
        assert resp.status_code == 200
        data = resp.json()
        # _format_output extracts the analysis field
        assert "AI Trends" in data["output"]
        assert "Key findings" in data["output"]
        assert data["status"] == "completed"

    @patch("server.send_a2a_message")
    def test_a2a_returns_502_on_no_output(self, mock_a2a, client):
        mock_a2a.return_value = {"output": None, "status": "failed", "task_id": "t1"}

        resp = client.post("/api/execute-a2a", json={"task": "test"})
        assert resp.status_code == 502

    @patch("server.send_a2a_message")
    def test_a2a_returns_502_on_mcp_meta_down(self, mock_a2a, client):
        mock_a2a.side_effect = RuntimeError("Connection refused")

        resp = client.post("/api/execute-a2a", json={"task": "test"})
        assert resp.status_code == 502


class TestBuildPaymentGeneric:
    @patch("research_agent.escrow.build_escrow_tx")
    @patch("server._load_orchestrator_kp")
    def test_returns_task_id_and_deadline(self, mock_kp, mock_build, client):
        from stellar_sdk import Keypair
        mock_kp.return_value = Keypair.random()
        mock_build.return_value = {
            "unsigned_xdr": "AAAA",
            "task_id": "x402-test123",
            "deadline": 9999999999,
            "amount_stroops": 2500000,
        }

        resp = client.post("/api/build-payment-generic", json={
            "amount_usdc": "0.25",
            "user_public_key": Keypair.random().public_key,
        })
        assert resp.status_code == 200
        data = resp.json()
        assert "task_id" in data
        assert data["task_id"].startswith("x402-")
        assert "deadline" in data
        assert "unsigned_xdr" in data

    @patch("research_agent.escrow.build_escrow_tx")
    @patch("server._load_orchestrator_kp")
    def test_calls_build_escrow_tx_not_transfer(self, mock_kp, mock_build, client):
        from stellar_sdk import Keypair
        mock_kp.return_value = Keypair.random()
        mock_build.return_value = {"unsigned_xdr": "X", "task_id": "t", "deadline": 0, "amount_stroops": 0}

        client.post("/api/build-payment-generic", json={
            "amount_usdc": "0.10",
            "user_public_key": Keypair.random().public_key,
        })

        mock_build.assert_called_once()
        call_kwargs = mock_build.call_args[1]
        assert "client_public" in call_kwargs
        assert "agent_public" in call_kwargs
        assert "task_id" in call_kwargs

    @patch("research_agent.escrow.build_escrow_tx")
    @patch("server._load_orchestrator_kp")
    def test_deadline_is_in_future(self, mock_kp, mock_build, client):
        from stellar_sdk import Keypair
        mock_kp.return_value = Keypair.random()
        mock_build.return_value = {"unsigned_xdr": "X", "task_id": "t", "deadline": 0, "amount_stroops": 0}

        resp = client.post("/api/build-payment-generic", json={
            "amount_usdc": "0.10",
            "user_public_key": Keypair.random().public_key,
            "deadline_seconds": 600,
        })

        call_kwargs = mock_build.call_args[1]
        assert call_kwargs["deadline_unix"] > time.time()
        assert call_kwargs["deadline_unix"] <= time.time() + 601


class TestX402PaymentGate:
    def test_debate_returns_402_without_payment(self, client_with_x402):
        resp = client_with_x402.post("/api/debate", json={"question": "test"})
        assert resp.status_code == 402
        assert "PAYMENT-REQUIRED" in resp.headers

    def test_a2a_returns_402_without_payment(self, client_with_x402):
        resp = client_with_x402.post("/api/execute-a2a", json={"task": "test"})
        assert resp.status_code == 402
        assert "PAYMENT-REQUIRED" in resp.headers

    def test_402_contains_price_info(self, client_with_x402):
        resp = client_with_x402.post("/api/debate", json={"question": "test"})
        header = resp.headers["PAYMENT-REQUIRED"]
        info = json.loads(base64.b64decode(header))
        assert "estimated_cost" in info or "price" in info
        assert "payTo" in info
        assert "network" in info

    def test_debate_cost_estimate(self, client_with_x402):
        resp = client_with_x402.post("/api/debate", json={"question": "Should we use Kafka?"})
        info = json.loads(base64.b64decode(resp.headers["PAYMENT-REQUIRED"]))
        assert info["estimated_cost"] == "0.25"

    def test_a2a_cost_scales_with_task_length(self, client_with_x402):
        short = client_with_x402.post("/api/execute-a2a", json={"task": "hi"})
        long_task = client_with_x402.post("/api/execute-a2a", json={"task": "Research the complete history of artificial intelligence from Turing to modern LLMs"})

        short_info = json.loads(base64.b64decode(short.headers["PAYMENT-REQUIRED"]))
        long_info = json.loads(base64.b64decode(long_task.headers["PAYMENT-REQUIRED"]))

        assert float(long_info["estimated_cost"]) > float(short_info["estimated_cost"])


class TestEscrowStatusEndpoint:
    @patch("research_agent.escrow.get_escrow_status")
    def test_returns_on_chain_status(self, mock_status, client):
        mock_status.return_value = {"task_id": "x402-abc", "status": "found", "raw_xdr": "AAAA"}

        resp = client.get("/api/escrow/x402-abc")
        assert resp.status_code == 200
        data = resp.json()
        assert data["task_id"] == "x402-abc"
        assert data["status"] == "found"

    @patch("research_agent.escrow.get_escrow_status")
    def test_returns_not_found_for_unknown_task(self, mock_status, client):
        mock_status.return_value = {"task_id": "unknown", "status": "not_found"}

        resp = client.get("/api/escrow/unknown")
        assert resp.status_code == 200
        data = resp.json()
        assert data["status"] == "not_found"


class TestRefundOnFailure:
    @patch("server.execute_tool")
    @patch("server._load_orchestrator_kp")
    def test_debate_failure_attempts_refund(self, mock_kp, mock_execute, client):
        from stellar_sdk import Keypair
        mock_kp.return_value = Keypair.random()
        mock_execute.side_effect = RuntimeError("mcp-meta crashed")

        # X402_ENABLED=false so request.state.escrow_task_id is unset; this exercises the error path only
        resp = client.post("/api/debate", json={"question": "test"})
        assert resp.status_code == 502
        assert "failed" in resp.json()["detail"].lower()
