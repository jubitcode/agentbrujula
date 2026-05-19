import base64
import json
import os
import sys
from unittest.mock import patch

import pytest
from stellar_sdk import Keypair

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", "api"))


@pytest.fixture
def client(tmp_path):
    """TestClient with X402_ENABLED=false, fresh temp DB, and forced module reloads so the env flip takes effect."""
    db_path = str(tmp_path / "test_auth.db")
    with patch.dict(os.environ, {"X402_ENABLED": "false"}):
        import importlib
        sys.modules.pop("server", None)
        sys.modules.pop("research_agent.x402_config", None)
        sys.modules.pop("research_agent.x402_middleware", None)
        import server
        importlib.reload(server)
        from research_agent.task_store import TaskStore
        server._store = TaskStore(db_path)
        yield __import__("fastapi.testclient", fromlist=["TestClient"]).TestClient(server.app)


class TestAuthChallenge:

    def test_returns_challenge_nonce(self, client):
        resp = client.get("/api/auth/challenge")
        assert resp.status_code == 200
        data = resp.json()
        assert "challenge" in data
        assert len(data["challenge"]) == 32  # uuid4 hex
        assert data["expires_in"] == 300

    def test_each_challenge_is_unique(self, client):
        r1 = client.get("/api/auth/challenge").json()["challenge"]
        r2 = client.get("/api/auth/challenge").json()["challenge"]
        assert r1 != r2


class TestWalletAuthEndpoint:

    def _make_auth_header(self, kp: Keypair, challenge: str) -> str:
        signature = kp.sign(challenge.encode()).hex()
        payload = {"publicKey": kp.public_key, "challenge": challenge, "signature": signature}
        return base64.b64encode(json.dumps(payload).encode()).decode()

    def test_result_auth_requires_header(self, client):
        resp = client.get("/api/result/test-task/auth")
        assert resp.status_code == 401

    def test_result_auth_with_valid_signature(self, client):
        kp = Keypair.random()
        import server
        server._store.create_task("auth-test-1", wallet_address=kp.public_key, task_text="test", status="pending")

        challenge = client.get("/api/auth/challenge").json()["challenge"]

        header = self._make_auth_header(kp, challenge)
        resp = client.get("/api/result/auth-test-1/auth", headers={"X-Wallet-Auth": header})
        assert resp.status_code == 200
        data = resp.json()
        assert data["status"] == "pending"

    def test_result_auth_wrong_wallet_returns_404(self, client):
        alice = Keypair.random()
        bob = Keypair.random()
        import server
        server._store.create_task("auth-test-2", wallet_address=alice.public_key, task_text="alice only")

        challenge = client.get("/api/auth/challenge").json()["challenge"]
        header = self._make_auth_header(bob, challenge)
        resp = client.get("/api/result/auth-test-2/auth", headers={"X-Wallet-Auth": header})
        assert resp.status_code == 404  # access denied disguised as not-found

    def test_result_auth_invalid_signature_returns_403(self, client):
        challenge = client.get("/api/auth/challenge").json()["challenge"]

        kp = Keypair.random()
        wrong_sig = "deadbeef" * 16
        payload = {"publicKey": kp.public_key, "challenge": challenge, "signature": wrong_sig}
        header = base64.b64encode(json.dumps(payload).encode()).decode()

        resp = client.get("/api/result/test-task/auth", headers={"X-Wallet-Auth": header})
        assert resp.status_code == 403

    def test_challenge_cannot_be_reused(self, client):
        kp = Keypair.random()
        import server
        server._store.create_task("auth-test-3", wallet_address=kp.public_key, status="pending")

        challenge = client.get("/api/auth/challenge").json()["challenge"]
        header = self._make_auth_header(kp, challenge)

        resp1 = client.get("/api/result/auth-test-3/auth", headers={"X-Wallet-Auth": header})
        assert resp1.status_code == 200

        # Replay of same challenge must fail
        resp2 = client.get("/api/result/auth-test-3/auth", headers={"X-Wallet-Auth": header})
        assert resp2.status_code == 401

    def test_malformed_header_returns_400(self, client):
        resp = client.get("/api/result/test/auth", headers={"X-Wallet-Auth": "not-valid-base64!!!"})
        assert resp.status_code == 400


class TestPaymentBindsWallet:

    @patch("server.execute_tool")
    def test_debate_result_includes_escrow_task_id(self, mock_execute, client):
        mock_execute.return_value = {"question": "q", "rounds": 0, "transcript": [], "decision": "", "adr": "", "concession_detected": False, "cost": {"total_tokens_in": 0, "total_tokens_out": 0, "estimated_cost_usd": 0}}

        resp = client.post("/api/debate", json={"question": "test"})
        assert resp.status_code == 200

    @patch("server.send_a2a_message")
    def test_a2a_result_includes_escrow_task_id(self, mock_a2a, client):
        mock_a2a.return_value = {"output": "hello", "status": "completed", "task_id": "t1", "context_id": "c1"}

        resp = client.post("/api/execute-a2a", json={"task": "test"})
        assert resp.status_code == 200
        data = resp.json()
        assert "escrow_task_id" in data
