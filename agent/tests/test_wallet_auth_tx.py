import base64
import json
import os
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "src"))

from fastapi.testclient import TestClient
from api_test_app import app


@pytest.fixture
def client():
    return TestClient(app)


class TestChallengeTxEndpoint:
    def test_returns_challenge_and_xdr(self, client):
        resp = client.get("/api/auth/challenge-tx")
        assert resp.status_code == 200
        data = resp.json()
        assert "challenge" in data
        assert "unsigned_xdr" in data
        assert "expires_in" in data
        assert len(data["challenge"]) == 28  # uuid4 hex truncated for Stellar memo limit

    def test_xdr_is_valid_stellar_transaction(self, client):
        from stellar_sdk import TransactionEnvelope
        from research_agent.stellar_config import NETWORK_PASSPHRASE

        resp = client.get("/api/auth/challenge-tx")
        data = resp.json()

        te = TransactionEnvelope.from_xdr(data["unsigned_xdr"], NETWORK_PASSPHRASE)
        memo = te.transaction.memo
        assert hasattr(memo, "memo_text")


class TestSignedXdrAuth:
    def test_signed_xdr_format_accepted(self, client):
        from stellar_sdk import Keypair, TransactionEnvelope
        from research_agent.stellar_config import NETWORK_PASSPHRASE
        import uuid

        kp = Keypair.random()

        # public_key required: source-account binding check rejects mismatches
        challenge_resp = client.get(
            f"/api/auth/challenge-tx?public_key={kp.public_key}"
        )
        challenge_data = challenge_resp.json()

        te = TransactionEnvelope.from_xdr(
            challenge_data["unsigned_xdr"], NETWORK_PASSPHRASE
        )
        te.sign(kp)
        signed_xdr = te.to_xdr()

        auth_data = {
            "publicKey": kp.public_key,
            "challenge": challenge_data["challenge"],
            "signedXdr": signed_xdr,
        }
        header = base64.b64encode(json.dumps(auth_data).encode()).decode()

        resp = client.get(
            "/api/execute/nonexistent-task",
            headers={"X-Wallet-Auth": header},
        )
        # 404 = auth passed, task missing
        assert resp.status_code == 404, (
            f"signedXdr auth should pass, got {resp.status_code}: {resp.text}"
        )

    def test_raw_signature_format_still_works(self, client):
        from stellar_sdk import Keypair
        import uuid

        kp = Keypair.random()
        challenge = uuid.uuid4().hex
        signature = kp.sign(challenge.encode()).hex()

        auth_data = {
            "publicKey": kp.public_key,
            "challenge": challenge,
            "signature": signature,
        }
        header = base64.b64encode(json.dumps(auth_data).encode()).decode()

        resp = client.get(
            "/api/execute/nonexistent-task",
            headers={"X-Wallet-Auth": header},
        )
        assert resp.status_code == 404, (
            f"Raw signature auth should still pass, got {resp.status_code}: {resp.text}"
        )

    def test_wrong_signer_rejected(self, client):
        from stellar_sdk import Keypair, TransactionEnvelope
        from research_agent.stellar_config import NETWORK_PASSPHRASE

        real_kp = Keypair.random()
        imposter_kp = Keypair.random()

        challenge_resp = client.get(
            f"/api/auth/challenge-tx?public_key={real_kp.public_key}"
        )
        challenge_data = challenge_resp.json()

        te = TransactionEnvelope.from_xdr(
            challenge_data["unsigned_xdr"], NETWORK_PASSPHRASE
        )
        te.sign(imposter_kp)
        signed_xdr = te.to_xdr()

        auth_data = {
            "publicKey": real_kp.public_key,
            "challenge": challenge_data["challenge"],
            "signedXdr": signed_xdr,
        }
        header = base64.b64encode(json.dumps(auth_data).encode()).decode()

        resp = client.get(
            "/api/execute/nonexistent-task",
            headers={"X-Wallet-Auth": header},
        )
        assert resp.status_code == 403, (
            f"Wrong signer should be rejected, got {resp.status_code}"
        )


class TestDEV72ChallengeBinding:
    """Signed XDR must be bound to the claimed challenge nonce — verifier checks memo + auth_challenge manage_data op."""

    def test_stolen_past_tx_replay_rejected(self, client):
        """Attacker submits a real V-signed past tx (memo != challenge) with a fresh challenge nonce."""
        from stellar_sdk import (
            Keypair, TransactionEnvelope, TransactionBuilder, Account,
        )
        from research_agent.stellar_config import NETWORK_PASSPHRASE

        v_kp = Keypair.random()

        # Past payment with unrelated memo — represents a real tx V signed before
        source = Account(v_kp.public_key, 0)
        b = TransactionBuilder(
            source_account=source,
            network_passphrase=NETWORK_PASSPHRASE,
            base_fee=100,
        )
        b.append_manage_data_op("user_pref", "dark_mode")
        b.add_text_memo("Coffee shop payment")
        b.set_timeout(300)
        past_tx = b.build()
        past_tx.sign(v_kp)
        stolen_xdr = past_tx.to_xdr()

        challenge_resp = client.get(
            f"/api/auth/challenge-tx?public_key={v_kp.public_key}"
        )
        fresh_challenge = challenge_resp.json()["challenge"]

        auth_data = {
            "publicKey": v_kp.public_key,
            "challenge": fresh_challenge,
            "signedXdr": stolen_xdr,
        }
        header = base64.b64encode(json.dumps(auth_data).encode()).decode()

        resp = client.get(
            "/api/execute/nonexistent-task",
            headers={"X-Wallet-Auth": header},
        )
        # Stolen XDR has wrong memo + no auth_challenge op
        assert resp.status_code == 403, (
            f"Stolen past-tx replay must be rejected, got {resp.status_code}: {resp.text}"
        )
        assert "memo" in resp.json()["detail"].lower() or \
               "auth_challenge" in resp.json()["detail"].lower() or \
               "manage_data" in resp.json()["detail"].lower()

    def test_memo_mismatch_rejected(self, client):
        """XDR signed for challenge A but submitted with challenge B must be rejected."""
        from stellar_sdk import Keypair, TransactionEnvelope
        from research_agent.stellar_config import NETWORK_PASSPHRASE

        kp = Keypair.random()

        chal_a_resp = client.get(
            f"/api/auth/challenge-tx?public_key={kp.public_key}"
        )
        chal_a = chal_a_resp.json()
        te = TransactionEnvelope.from_xdr(chal_a["unsigned_xdr"], NETWORK_PASSPHRASE)
        te.sign(kp)
        signed_xdr_a = te.to_xdr()

        chal_b_resp = client.get(
            f"/api/auth/challenge-tx?public_key={kp.public_key}"
        )
        chal_b = chal_b_resp.json()["challenge"]

        auth_data = {
            "publicKey": kp.public_key,
            "challenge": chal_b,  # claimed challenge doesn't match what XDR signed
            "signedXdr": signed_xdr_a,
        }
        header = base64.b64encode(json.dumps(auth_data).encode()).decode()

        resp = client.get(
            "/api/execute/nonexistent-task",
            headers={"X-Wallet-Auth": header},
        )
        assert resp.status_code == 403, (
            f"Challenge mismatch must be rejected, got {resp.status_code}"
        )

    def test_source_account_mismatch_rejected(self, client):
        """Signed XDR's source must equal the claimed publicKey — source-account check fires before signature check."""
        from stellar_sdk import Keypair, TransactionEnvelope
        from research_agent.stellar_config import NETWORK_PASSPHRASE

        kp_a = Keypair.random()
        kp_b = Keypair.random()

        chal_resp = client.get(
            f"/api/auth/challenge-tx?public_key={kp_a.public_key}"
        )
        chal = chal_resp.json()
        te = TransactionEnvelope.from_xdr(chal["unsigned_xdr"], NETWORK_PASSPHRASE)
        te.sign(kp_a)
        signed_xdr = te.to_xdr()

        auth_data = {
            "publicKey": kp_b.public_key,
            "challenge": chal["challenge"],
            "signedXdr": signed_xdr,
        }
        header = base64.b64encode(json.dumps(auth_data).encode()).decode()

        resp = client.get(
            "/api/execute/nonexistent-task",
            headers={"X-Wallet-Auth": header},
        )
        assert resp.status_code == 403, (
            f"Source mismatch must be rejected, got {resp.status_code}"
        )
