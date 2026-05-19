"""/api/prepare-wallet security guards: X-Wallet-Auth, caller==target, per-IP rate limit,
per-wallet cooldown, mainnet hard-disable, and structured mint_error responses."""

import base64
import json
import sys
import uuid
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "src"))

from fastapi.testclient import TestClient


def _fresh_auth_header(public_key: str, secret_key: str, challenge: str) -> str:
    """Build a Format 1 X-Wallet-Auth header (raw challenge signature)."""
    from stellar_sdk import Keypair
    kp = Keypair.from_secret(secret_key)
    sig = kp.sign(challenge.encode()).hex()
    payload = {"publicKey": public_key, "challenge": challenge, "signature": sig}
    return base64.b64encode(json.dumps(payload).encode()).decode()


@pytest.fixture(autouse=True)
def reset_rate_limiter():
    """The @limiter.limit("3/hour") on /api/prepare-wallet accumulates across tests (shared testclient IP) — reset per-test."""
    import server as srv
    srv.limiter.reset()
    yield


@pytest.fixture
def client():
    from api_test_app import app
    return TestClient(app)


@pytest.fixture
def fresh_wallet():
    from stellar_sdk import Keypair
    kp = Keypair.random()
    return {"public_key": kp.public_key, "secret_key": kp.secret}


class TestAuthRequired:
    def test_no_auth_returns_401(self, client, fresh_wallet):
        resp = client.post(
            "/api/prepare-wallet",
            json={"public_key": fresh_wallet["public_key"]},
        )
        assert resp.status_code == 401

    def test_mismatched_wallet_returns_403(self, client, fresh_wallet):
        """Caller authed as wallet A tries to mint to wallet B → 403."""
        from stellar_sdk import Keypair
        other = Keypair.random()
        challenge = uuid.uuid4().hex
        header = _fresh_auth_header(
            fresh_wallet["public_key"], fresh_wallet["secret_key"], challenge,
        )
        resp = client.post(
            "/api/prepare-wallet",
            json={"public_key": other.public_key},
            headers={"X-Wallet-Auth": header},
        )
        assert resp.status_code == 403
        assert "does not match" in resp.json()["detail"]


class TestMainnetDisable:
    def test_mainnet_env_rejects(self, monkeypatch, client, fresh_wallet):
        # api_test_app loads server.py as `server` via sys.path hack — patch `server` not `api.server`
        import server as srv
        monkeypatch.setattr(srv, "STELLAR_NETWORK", "public")

        challenge = uuid.uuid4().hex
        header = _fresh_auth_header(
            fresh_wallet["public_key"], fresh_wallet["secret_key"], challenge,
        )
        resp = client.post(
            "/api/prepare-wallet",
            json={"public_key": fresh_wallet["public_key"]},
            headers={"X-Wallet-Auth": header},
        )
        assert resp.status_code == 403
        assert "Mainnet" in resp.json()["detail"]


class TestPerWalletCooldown:
    def test_cooldown_blocks_second_mint(self, client, fresh_wallet):
        # Can't make a real mint succeed in test (no live RPC) — prime the cooldown table directly
        from research_agent.task_store import TaskStore
        store = TaskStore()
        store.record_mint(fresh_wallet["public_key"])

        challenge = uuid.uuid4().hex
        header = _fresh_auth_header(
            fresh_wallet["public_key"], fresh_wallet["secret_key"], challenge,
        )
        resp = client.post(
            "/api/prepare-wallet",
            json={"public_key": fresh_wallet["public_key"]},
            headers={"X-Wallet-Auth": header},
        )
        assert resp.status_code == 429
        assert "cooldown" in resp.json()["detail"].lower()


class TestStructuredErrors:
    def test_response_shape_includes_mint_error_field(self, client, fresh_wallet, monkeypatch):
        """mint_error key must always be present (null on success, string on failure) — replaces the silent `mint_tx: null`."""
        # Simulate "orchestrator not configured"
        monkeypatch.delenv("ORCHESTRATOR_SECRET_KEY", raising=False)

        challenge = uuid.uuid4().hex
        header = _fresh_auth_header(
            fresh_wallet["public_key"], fresh_wallet["secret_key"], challenge,
        )
        resp = client.post(
            "/api/prepare-wallet",
            json={"public_key": fresh_wallet["public_key"]},
            headers={"X-Wallet-Auth": header},
        )
        assert resp.status_code == 200
        body = resp.json()
        assert "mint_error" in body  # may be None if trustline_xdr branch hit instead — key still required
