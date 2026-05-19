import base64
import importlib
import json
import os
import sys
import time
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "src"))


@pytest.fixture(autouse=True)
def stable_secret(monkeypatch):
    monkeypatch.setenv("SESSION_TOKEN_SECRET", "a" * 64)
    # session_tokens reads the secret at import — force reload
    if "research_agent.session_tokens" in sys.modules:
        importlib.reload(sys.modules["research_agent.session_tokens"])


@pytest.fixture(autouse=True)
def reset_limiter():
    import server as srv
    srv.limiter.reset()
    yield


@pytest.fixture
def client():
    from fastapi.testclient import TestClient
    from api_test_app import app
    return TestClient(app)


def _real_jwt(sub: str = "GABCD" + "X" * 51, ttl: int = 300) -> str:
    from research_agent.session_tokens import issue
    token, _ = issue(sub, ttl_seconds=ttl)
    return token


class TestBearerJWT:
    def test_valid_jwt_accepted(self, client):
        """Auth passes → 404 (task not found), not 401."""
        token = _real_jwt(sub="GTESTWALLET" + "X" * 45)
        resp = client.get(
            "/api/result/nonexistent-task/auth",
            headers={"Authorization": f"Bearer {token}"},
        )
        assert resp.status_code == 404, (
            f"Valid JWT should pass auth; got {resp.status_code}: {resp.text}"
        )

    def test_expired_jwt_rejected(self, client):
        token = _real_jwt(ttl=1)
        time.sleep(2)
        resp = client.get(
            "/api/result/nonexistent-task/auth",
            headers={"Authorization": f"Bearer {token}"},
        )
        assert resp.status_code == 401
        assert "expired" in resp.text.lower()

    def test_malformed_jwt_rejected(self, client):
        resp = client.get(
            "/api/result/nonexistent-task/auth",
            headers={"Authorization": "Bearer not.a.valid.jwt"},
        )
        assert resp.status_code == 401

    def test_unsigned_alg_none_jwt_rejected(self, client):
        """CVE-2015-9235: alg=none must NOT pass — PyJWT 2.x defaults to reject."""
        header = base64.urlsafe_b64encode(
            json.dumps({"alg": "none", "typ": "JWT"}).encode()
        ).decode().rstrip("=")
        payload = base64.urlsafe_b64encode(
            json.dumps({"sub": "attacker", "exp": int(time.time()) + 300}).encode()
        ).decode().rstrip("=")
        unsigned = f"{header}.{payload}."
        resp = client.get(
            "/api/result/nonexistent-task/auth",
            headers={"Authorization": f"Bearer {unsigned}"},
        )
        assert resp.status_code == 401

    def test_jwt_signed_with_other_secret_rejected(self, client, monkeypatch):
        """Token forged under a different HMAC key must be rejected."""
        monkeypatch.setenv("SESSION_TOKEN_SECRET", "a" * 64)
        importlib.reload(sys.modules["research_agent.session_tokens"])
        from research_agent.session_tokens import issue
        token, _ = issue("GTEST" + "X" * 51)

        monkeypatch.setenv("SESSION_TOKEN_SECRET", "b" * 64)
        importlib.reload(sys.modules["research_agent.session_tokens"])
        # Reload server so its _wallet_auth import rebinds to reloaded session_tokens
        if "server" in sys.modules:
            importlib.reload(sys.modules["server"])
        from fastapi.testclient import TestClient
        from api_test_app import app
        client2 = TestClient(app)
        resp = client2.get(
            "/api/result/nonexistent-task/auth",
            headers={"Authorization": f"Bearer {token}"},
        )
        assert resp.status_code == 401


class TestNoAuth:
    def test_missing_auth_returns_401(self, client):
        resp = client.get("/api/result/nonexistent-task/auth")
        assert resp.status_code == 401

    def test_bearer_prefix_with_empty_token_rejected(self, client):
        resp = client.get(
            "/api/result/nonexistent-task/auth",
            headers={"Authorization": "Bearer "},
        )
        assert resp.status_code == 401


class TestLegacyWalletAuthCoexists:
    """X-Wallet-Auth (legacy single-use header) MUST still work alongside Bearer JWT."""

    def test_x_wallet_auth_still_accepted(self, client):
        from stellar_sdk import Keypair
        kp = Keypair.random()
        challenge = "test-coexist-challenge-" + os.urandom(8).hex()
        # Format 1 branch (raw signature) in _validate_signed_challenge
        signature = kp.sign(challenge.encode()).hex()
        payload = json.dumps({
            "publicKey": kp.public_key,
            "challenge": challenge,
            "signature": signature,
        })
        header = base64.b64encode(payload.encode()).decode()
        resp = client.get(
            "/api/result/nonexistent-task/auth",
            headers={"X-Wallet-Auth": header},
        )
        # 404 = auth passed, task missing; anything else is a regression
        assert resp.status_code == 404, (
            f"X-Wallet-Auth must still work; got {resp.status_code}: {resp.text}"
        )
