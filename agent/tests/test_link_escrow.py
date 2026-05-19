import os
import sys
from pathlib import Path
from unittest.mock import patch, MagicMock

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "src"))
os.environ["X402_ENABLED"] = "false"

from fastapi.testclient import TestClient
from api_test_app import app


@pytest.fixture
def client():
    return TestClient(app)


class TestLinkTaskId:
    """submit-payment with link_task_id updates the existing task instead of creating a new one."""

    @patch("research_agent.x402_config.X402_ENABLED", False)
    @patch("server.list_active_full")
    @patch("server._load_orchestrator_kp", return_value=None)
    def test_link_task_id_updates_existing_task(self, mock_kp, mock_list, client):
        from stellar_sdk import Keypair
        import uuid

        mock_list.return_value = [{"handle": "test-agent", "agent": "GTEST", "capabilities": ["research"], "rate_stroops": 1000000}]

        # Pre-create a team task (simulates the hire-team flow)
        import server
        team_task_id = uuid.uuid4().hex
        server._save_task(team_task_id, {
            "task_id": team_task_id,
            "wallet_address": "",
            "agent_handle": "test-agent",
            "task": "team test",
            "escrow_tx": None,
            "deadline_unix": 9999999999,
            "status": "pending",
            "is_team": True,
        })

        task = server._load_task(team_task_id)
        assert task is not None
        assert task.get("escrow_tx") is None or task.get("escrow_tx") == ""

        mock_server = MagicMock()
        mock_server.send_transaction.return_value = MagicMock(hash="fake_tx_hash", status="PENDING")
        mock_result = MagicMock()
        mock_result.status = "SUCCESS"
        mock_server.get_transaction.return_value = mock_result

        with patch("server.SorobanServer", return_value=mock_server):
            with patch("server.stellar_sdk") as mock_sdk:
                mock_sdk.TransactionEnvelope.from_xdr.return_value = MagicMock()

                # Build JWT auth
                import base64, json, time
                h = base64.urlsafe_b64encode(json.dumps({"alg": "none", "typ": "JWT"}).encode()).decode().rstrip("=")
                p = base64.urlsafe_b64encode(json.dumps({"sub": "testuser", "exp": int(time.time()) + 300, "iss": "http://localhost:8180/realms/mcp-meta"}).encode()).decode().rstrip("=")
                jwt = f"{h}.{p}."

                resp = client.post("/api/submit-payment", json={
                    "signed_xdr": "FAKE_XDR",
                    "agent_handle": "test-agent",
                    "task": "team test",
                    "budget_xlm": 10.0,
                    "task_id": "payment-task-id",
                    "link_task_id": team_task_id,
                }, headers={"Authorization": f"Bearer {jwt}"})

        assert resp.status_code == 200, f"Expected 200, got {resp.status_code}: {resp.text}"
        data = resp.json()

        # Response must return the TEAM task_id, not the payment's task_id
        assert data["task_id"] == team_task_id
        assert data["escrow_tx"] == "fake_tx_hash"

        updated = server._load_task(team_task_id)
        assert updated["escrow_tx"] == "fake_tx_hash"
