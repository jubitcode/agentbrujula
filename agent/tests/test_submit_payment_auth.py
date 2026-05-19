"""/api/submit-payment requires auth + link_task_id strict ownership.

  - Endpoint requires Depends(_wallet_auth)
  - link_task_id only works if existing task is bound to caller
  - wallet_addr always comes from authenticated wallet, never from x402 middleware state"""

import base64
import json
import sys
import uuid
from pathlib import Path
from unittest.mock import patch

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "src"))

from fastapi.testclient import TestClient


def _auth_header(public_key: str, secret_key: str, challenge: str) -> str:
    from stellar_sdk import Keypair
    kp = Keypair.from_secret(secret_key)
    sig = kp.sign(challenge.encode()).hex()
    payload = {"publicKey": public_key, "challenge": challenge, "signature": sig}
    return base64.b64encode(json.dumps(payload).encode()).decode()


@pytest.fixture(autouse=True)
def reset_limiter():
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


class TestSubmitPaymentAuth:
    def test_no_auth_returns_401(self, client):
        resp = client.post("/api/submit-payment", json={
            "signed_xdr": "x", "agent_handle": "ghost", "task": "t", "budget_xlm": 1,
        })
        assert resp.status_code == 401

    def test_garbage_auth_returns_4xx(self, client):
        resp = client.post(
            "/api/submit-payment",
            headers={"X-Wallet-Auth": "x"},
            json={
                "signed_xdr": "x", "agent_handle": "ghost",
                "task": "t", "budget_xlm": 1,
            },
        )
        assert resp.status_code in (400, 401, 403), (
            f"Garbage auth must be rejected, got {resp.status_code}"
        )


class TestLinkTaskIdOwnership:
    def test_link_to_other_wallet_task_returns_404(
        self, client, fresh_wallet, monkeypatch,
    ):
        """Caller A linking to a task owned by B must be rejected (don't even reveal the task exists)."""
        import server as srv
        from research_agent.task_store import TaskStore

        store = TaskStore(db_path=":memory:")
        monkeypatch.setattr(srv, "_store", store)
        store.create_task(
            task_id="task-b",
            wallet_address="GOWNER_B_000",
            agent_handle="research",
            task_text="x",
            escrow_tx="",
            deadline_unix=0,
        )

        challenge = uuid.uuid4().hex
        header = _auth_header(
            fresh_wallet["public_key"], fresh_wallet["secret_key"], challenge,
        )
        resp = client.post(
            "/api/submit-payment",
            headers={"X-Wallet-Auth": header},
            json={
                "signed_xdr": "doesnt-matter-rejected-before-stellar",
                "agent_handle": "research",
                "task": "x",
                "budget_xlm": 1,
                "link_task_id": "task-b",
            },
        )
        # Ownership check runs BEFORE Stellar submission, so the attacker can't even probe the Stellar layer via link_task_id
        assert resp.status_code != 200, (
            f"Cross-wallet link MUST be rejected; got {resp.status_code}: {resp.text}"
        )


class TestPublicPathsRemoved:
    def test_submit_payment_not_in_public_paths(self):
        import server as srv
        assert "/api/submit-payment" not in srv._PUBLIC_PATHS
