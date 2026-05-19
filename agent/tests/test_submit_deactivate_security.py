"""Security tests for /api/submit-deactivate.

Endpoint requires real wallet auth + caller must own the agent in the registry
+ signed XDR's source must match the caller (mirrors /api/build-deactivate)."""

import base64
import json
import sys
import uuid
from pathlib import Path

import pytest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "src"))

from fastapi.testclient import TestClient


def _auth_header(public_key: str, secret_key: str, challenge: str) -> str:
    from stellar_sdk import Keypair
    kp = Keypair.from_secret(secret_key)
    sig = kp.sign(challenge.encode()).hex()
    payload = {"publicKey": public_key, "challenge": challenge, "signature": sig}
    return base64.b64encode(json.dumps(payload).encode()).decode()


@pytest.fixture
def client():
    from api_test_app import app
    return TestClient(app)


@pytest.fixture
def fresh_wallet():
    from stellar_sdk import Keypair
    kp = Keypair.random()
    return {"public_key": kp.public_key, "secret_key": kp.secret}


class TestSubmitDeactivateAuth:
    def test_no_auth_returns_401(self, client):
        resp = client.post(
            "/api/submit-deactivate",
            json={"signed_xdr": "garbage", "handle": "adr-moderator"},
        )
        assert resp.status_code == 401

    def test_garbage_auth_no_longer_accepted(self, client):
        """Depends(_wallet_auth) parses + validates — 1-byte garbage no longer passes the AuthGate presence check."""
        resp = client.post(
            "/api/submit-deactivate",
            json={"signed_xdr": "garbage", "handle": "adr-moderator"},
            headers={"X-Wallet-Auth": "x"},
        )
        assert resp.status_code in (400, 401, 403), (
            f"Garbage auth must NOT proceed to handler, got {resp.status_code}: {resp.text}"
        )

    def test_agent_not_in_registry_returns_404(self, client, fresh_wallet):
        import server as srv
        with patch.object(srv, "list_active_full", return_value=[]):
            challenge = uuid.uuid4().hex
            header = _auth_header(
                fresh_wallet["public_key"], fresh_wallet["secret_key"], challenge,
            )
            resp = client.post(
                "/api/submit-deactivate",
                json={"signed_xdr": "x", "handle": "ghost-agent"},
                headers={"X-Wallet-Auth": header},
            )
            assert resp.status_code == 404
            assert "not found" in resp.json()["detail"].lower()

    def test_caller_not_agent_owner_returns_403(self, client, fresh_wallet):
        """Authenticated A cannot deactivate an agent owned by B."""
        import server as srv
        from stellar_sdk import Keypair
        other_owner = Keypair.random().public_key
        fake_tool = {
            "handle": "adr-moderator",
            "agent": other_owner,
        }
        with patch.object(srv, "list_active_full", return_value=[fake_tool]):
            challenge = uuid.uuid4().hex
            header = _auth_header(
                fresh_wallet["public_key"], fresh_wallet["secret_key"], challenge,
            )
            resp = client.post(
                "/api/submit-deactivate",
                json={"signed_xdr": "x", "handle": "adr-moderator"},
                headers={"X-Wallet-Auth": header},
            )
            assert resp.status_code == 403
            assert "owner" in resp.json()["detail"].lower()

    def test_signed_xdr_source_mismatch_returns_403(self, client, fresh_wallet):
        """Defense-in-depth: even if A owns the registry entry, the XDR's source account must also be A."""
        import server as srv
        from stellar_sdk import Keypair, TransactionBuilder, Account
        from research_agent.stellar_config import NETWORK_PASSPHRASE

        other = Keypair.random()
        b_account = Account(other.public_key, 0)
        builder = TransactionBuilder(
            source_account=b_account,
            network_passphrase=NETWORK_PASSPHRASE,
            base_fee=100,
        )
        builder.append_manage_data_op("anything", "x")
        builder.set_timeout(300)
        tx = builder.build()
        tx.sign(other)
        b_signed_xdr = tx.to_xdr()

        a_owned_tool = {
            "handle": "adr-moderator",
            "agent": fresh_wallet["public_key"],
        }
        with patch.object(srv, "list_active_full", return_value=[a_owned_tool]):
            challenge = uuid.uuid4().hex
            header = _auth_header(
                fresh_wallet["public_key"], fresh_wallet["secret_key"], challenge,
            )
            resp = client.post(
                "/api/submit-deactivate",
                json={"signed_xdr": b_signed_xdr, "handle": "adr-moderator"},
                headers={"X-Wallet-Auth": header},
            )
            assert resp.status_code == 403
            assert "source account" in resp.json()["detail"].lower()
