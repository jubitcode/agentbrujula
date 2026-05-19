import base64
import json
import sys
from pathlib import Path
from unittest.mock import patch, MagicMock

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "src"))

from fastapi.testclient import TestClient
from api_test_app import app


@pytest.fixture
def client():
    return TestClient(app)


class TestDeactivateEndpointSecurity:
    def test_old_deactivate_rejects_secret_key_body(self, client):
        resp = client.post("/api/deactivate", json={
            "agent_secret": "SXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
            "handle": "test-agent",
        })
        # Endpoint removed + AuthGateMiddleware blocks unauthenticated requests
        assert resp.status_code in (401, 404, 422, 405), (
            f"Old /api/deactivate still accepts raw secrets! Got {resp.status_code}"
        )

    def test_no_secret_key_in_any_request_model(self):
        """No Pydantic request model may carry an agent_secret field — secret keys must never be transmitted."""
        import server
        for name in dir(server):
            obj = getattr(server, name)
            if hasattr(obj, "model_fields"):
                assert "agent_secret" not in obj.model_fields, (
                    f"Model {name} still has 'agent_secret' field — secret keys must never be in request bodies"
                )


class TestBuildDeactivateEndpoint:
    def test_build_deactivate_requires_wallet_auth(self, client):
        resp = client.post("/api/build-deactivate", json={"handle": "test-agent"})
        assert resp.status_code == 401

    @patch("server.list_active_full")
    @patch("research_agent.soroban_client.build_deactivate_tx")
    def test_build_deactivate_returns_unsigned_xdr(self, mock_build, mock_list, client):
        from stellar_sdk import Keypair
        import uuid

        kp = Keypair.random()

        mock_list.return_value = [{"handle": "test-agent", "agent": kp.public_key}]
        mock_build.return_value = {"unsigned_xdr": "AAAA_FAKE_XDR", "handle": "test-agent"}

        # Unique challenge to avoid nonce replay rejection
        challenge = f"deactivate-{uuid.uuid4().hex}"
        signature = kp.sign(challenge.encode()).hex()
        auth_data = base64.b64encode(json.dumps({
            "publicKey": kp.public_key,
            "challenge": challenge,
            "signature": signature,
        }).encode()).decode()

        resp = client.post(
            "/api/build-deactivate",
            json={"handle": "test-agent"},
            headers={"X-Wallet-Auth": auth_data},
        )
        assert resp.status_code == 200, f"Expected 200, got {resp.status_code}: {resp.text}"
        data = resp.json()
        assert "unsigned_xdr" in data
        assert data["handle"] == "test-agent"


class TestSubmitDeactivateEndpoint:
    def test_submit_deactivate_accepts_signed_xdr(self, client):
        resp = client.post("/api/submit-deactivate", json={
            "signed_xdr": "FAKE_XDR",
            "handle": "test-agent",
        })
        assert resp.status_code != 404, "Endpoint /api/submit-deactivate must exist"

    def test_submit_deactivate_does_not_accept_secret(self, client):
        """Even if extra agent_secret field is sent, server must never use it (signed_xdr is the only accepted form)."""
        resp = client.post("/api/submit-deactivate", json={
            "agent_secret": "SXXXXXXX",
            "handle": "test-agent",
        })
