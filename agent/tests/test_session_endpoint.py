import importlib
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "src"))


@pytest.fixture(autouse=True)
def stable_secret(monkeypatch):
    monkeypatch.setenv("SESSION_TOKEN_SECRET", "a" * 64)
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


def _sign_challenge_tx(client, public_key: str, secret_key: str) -> tuple[str, str]:
    from stellar_sdk import Keypair, TransactionEnvelope
    import server as srv

    resp = client.get(f"/api/auth/challenge-tx?public_key={public_key}")
    assert resp.status_code == 200, resp.text
    body = resp.json()
    challenge = body["challenge"]
    unsigned_xdr = body["unsigned_xdr"]

    te = TransactionEnvelope.from_xdr(unsigned_xdr, srv.NETWORK_PASSPHRASE)
    te.sign(Keypair.from_secret(secret_key))
    return challenge, te.to_xdr()


class TestSessionEndpoint:
    def test_endpoint_is_public(self, client):
        """Empty POST returns 422 (Pydantic validation), proving AuthGate didn't block /api/auth/session."""
        resp = client.post("/api/auth/session", json={})
        assert resp.status_code == 422, (
            f"Expected 422 (validation), got {resp.status_code} — "
            f"AuthGate may be blocking /api/auth/session"
        )

    def test_valid_signed_challenge_returns_jwt(self, client):
        from stellar_sdk import Keypair
        kp = Keypair.random()
        challenge, signed_xdr = _sign_challenge_tx(client, kp.public_key, kp.secret)
        resp = client.post("/api/auth/session", json={
            "publicKey": kp.public_key,
            "challenge": challenge,
            "signedXdr": signed_xdr,
        })
        assert resp.status_code == 200, resp.text
        body = resp.json()
        assert body["token_type"] == "Bearer"
        assert body["expires_in"] == 3600
        assert isinstance(body["token"], str) and len(body["token"]) > 50

    def test_issued_jwt_authenticates_subsequent_requests(self, client):
        """Exchange → use JWT → no second Freighter prompt; 404 (task missing) means auth passed."""
        from stellar_sdk import Keypair
        kp = Keypair.random()
        challenge, signed_xdr = _sign_challenge_tx(client, kp.public_key, kp.secret)
        exchange = client.post("/api/auth/session", json={
            "publicKey": kp.public_key,
            "challenge": challenge,
            "signedXdr": signed_xdr,
        })
        token = exchange.json()["token"]

        resp = client.get(
            "/api/result/nonexistent-task/auth",
            headers={"Authorization": f"Bearer {token}"},
        )
        assert resp.status_code == 404

    def test_replayed_challenge_rejected_after_session_issued(self, client):
        """Session exchange is strict single-use — one signed challenge = one JWT, no 2-slot allowance like legacy X-Wallet-Auth."""
        from stellar_sdk import Keypair
        kp = Keypair.random()
        challenge, signed_xdr = _sign_challenge_tx(client, kp.public_key, kp.secret)

        first = client.post("/api/auth/session", json={
            "publicKey": kp.public_key,
            "challenge": challenge,
            "signedXdr": signed_xdr,
        })
        assert first.status_code == 200

        second = client.post("/api/auth/session", json={
            "publicKey": kp.public_key,
            "challenge": challenge,
            "signedXdr": signed_xdr,
        })
        assert second.status_code == 401, (
            f"Session replay must fail; got {second.status_code}: {second.text}"
        )

    def test_signature_for_wrong_pubkey_rejected(self, client):
        """Signing with secret X but claiming pubkey Y → 403 (pubkey-signer binding)."""
        from stellar_sdk import Keypair
        signer = Keypair.random()
        claimed = Keypair.random()
        challenge, signed_xdr = _sign_challenge_tx(client, signer.public_key, signer.secret)
        resp = client.post("/api/auth/session", json={
            "publicKey": claimed.public_key,
            "challenge": challenge,
            "signedXdr": signed_xdr,
        })
        assert resp.status_code == 403

    def test_missing_signature_and_signedxdr_rejected(self, client):
        resp = client.post("/api/auth/session", json={
            "publicKey": "GTEST" + "X" * 51,
            "challenge": "nonce",
        })
        assert resp.status_code == 400
