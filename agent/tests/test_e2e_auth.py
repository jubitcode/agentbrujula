import base64
import json

import pytest
from stellar_sdk import Keypair

from research_agent.wallet_auth import verify_wallet_signature


class TestE2EWalletAuth:
    def test_full_challenge_response_flow(self):
        import uuid
        challenge = uuid.uuid4().hex

        user_kp = Keypair.random()
        signature = user_kp.sign(challenge.encode())

        assert verify_wallet_signature(
            user_kp.public_key,
            challenge,
            signature.hex(),
        )

    def test_different_users_cannot_impersonate(self):
        import uuid
        challenge = uuid.uuid4().hex

        alice = Keypair.random()
        bob = Keypair.random()

        alice_sig = alice.sign(challenge.encode())

        assert verify_wallet_signature(alice.public_key, challenge, alice_sig.hex())
        assert not verify_wallet_signature(bob.public_key, challenge, alice_sig.hex())

    def test_auth_header_format(self):
        """Mirrors the exact header format the frontend sends (base64-encoded JSON)."""
        user_kp = Keypair.random()
        challenge = "test-challenge-abc123"
        signature = user_kp.sign(challenge.encode()).hex()

        header_payload = {
            "publicKey": user_kp.public_key,
            "challenge": challenge,
            "signature": signature,
        }
        header_value = base64.b64encode(json.dumps(header_payload).encode()).decode()

        decoded = json.loads(base64.b64decode(header_value))
        assert verify_wallet_signature(
            decoded["publicKey"],
            decoded["challenge"],
            decoded["signature"],
        )

    def test_challenge_replay_with_different_signature_fails(self):
        challenge = "replay-test-123"

        kp1 = Keypair.random()
        kp2 = Keypair.random()

        sig1 = kp1.sign(challenge.encode()).hex()
        assert verify_wallet_signature(kp1.public_key, challenge, sig1)

        sig2 = kp2.sign(challenge.encode()).hex()
        assert sig1 != sig2

        assert verify_wallet_signature(kp2.public_key, challenge, sig2)
        assert not verify_wallet_signature(kp1.public_key, challenge, sig2)
