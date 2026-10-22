import base64
import json
import os
import sys
from unittest.mock import patch, MagicMock, AsyncMock

import pytest
from stellar_sdk import Keypair


class TestExtractWalletFromHorizonTx:
    @pytest.mark.asyncio
    async def test_extracts_source_account(self):
        from research_agent.x402_middleware import _verify_tx_on_horizon

        tx_resp = MagicMock()
        tx_resp.status_code = 200
        tx_resp.json.return_value = {
            "successful": True,
            "source_account": "GABCDEFG12345",
            "hash": "tx_abc",
        }

        # A Soroban escrow creation as Horizon actually reports it: the
        # operation type is `invoke_host_function` (not "invoke_contract"), and
        # what moved is in `asset_balance_changes`. Verified against a real
        # testnet transaction.
        ops_resp = MagicMock()
        ops_resp.status_code = 200
        ops_resp.json.return_value = {
            "_embedded": {
                "records": [
                    {
                        "type": "invoke_host_function",
                        "type_i": 24,
                        "asset_balance_changes": [
                            {
                                "asset_type": "credit_alphanum4",
                                "asset_code": "USDC",
                                "type": "transfer",
                                "from": "GPAYER",
                                "to": "GESCROW",
                                "amount": "1.0000000",
                            }
                        ],
                    },
                ],
            },
        }

        mock_client = AsyncMock()
        mock_client.get.side_effect = [tx_resp, ops_resp]
        mock_client.__aenter__ = AsyncMock(return_value=mock_client)
        mock_client.__aexit__ = AsyncMock(return_value=False)

        with patch("research_agent.x402_middleware.httpx.AsyncClient", return_value=mock_client):
            result = await _verify_tx_on_horizon("tx_abc", pay_to="GESCROW", expected_amount="1.00")

        assert result["success"] is True
        # The account bound to the session is the one that was DEBITED, not the
        # one that submitted the transaction. They differ whenever a third party
        # pays fees or batches operations, and the payer is the party whose
        # money is at stake.
        assert result["wallet_address"] == "GPAYER"
        assert result["txHash"] == "tx_abc"
        # The amount comes back from Horizon's record of the transfer, so the
        # caller can see what was actually paid rather than what was claimed.
        assert result["amount_paid"] == "1.0000000"

    @pytest.mark.asyncio
    async def test_failed_tx_returns_no_wallet(self):
        from research_agent.x402_middleware import _verify_tx_on_horizon

        mock_resp = MagicMock()
        mock_resp.status_code = 200
        mock_resp.json.return_value = {"successful": False}

        mock_client = AsyncMock()
        mock_client.get.return_value = mock_resp
        mock_client.__aenter__ = AsyncMock(return_value=mock_client)
        mock_client.__aexit__ = AsyncMock(return_value=False)

        with patch("research_agent.x402_middleware.httpx.AsyncClient", return_value=mock_client):
            result = await _verify_tx_on_horizon("tx_fail", pay_to="GESCROW", expected_amount="1.00")

        assert result["success"] is False

    @pytest.mark.asyncio
    async def test_404_returns_not_found(self):
        from research_agent.x402_middleware import _verify_tx_on_horizon

        mock_resp = MagicMock()
        mock_resp.status_code = 404

        mock_client = AsyncMock()
        mock_client.get.return_value = mock_resp
        mock_client.__aenter__ = AsyncMock(return_value=mock_client)
        mock_client.__aexit__ = AsyncMock(return_value=False)

        with patch("research_agent.x402_middleware.httpx.AsyncClient", return_value=mock_client):
            result = await _verify_tx_on_horizon("nonexistent", pay_to="GESCROW", expected_amount="1.00")

        assert result["success"] is False


class TestWalletSignatureVerification:
    """Sign-in signatures are verified under SEP-53, not over raw bytes.

    `sep53_sign` below is what a wallet does internally, so these tests exercise
    the same path a real Freighter / xBull / LOBSTR signature takes.
    """

    @staticmethod
    def sep53_sign(kp: Keypair, message: str) -> bytes:
        from research_agent.wallet_auth import sep53_digest

        return kp.sign(sep53_digest(message))

    def test_valid_sep53_signature_accepted(self):
        from research_agent.wallet_auth import verify_wallet_signature

        kp = Keypair.random()
        challenge = "test-challenge-123"
        signature = self.sep53_sign(kp, challenge).hex()

        assert verify_wallet_signature(kp.public_key, challenge, signature) is True

    def test_base64_signature_accepted(self):
        # SEP-43 does not pin an encoding for signMessage and wallets differ,
        # so a correctly signed message must not be rejected over formatting.
        import base64

        from research_agent.wallet_auth import verify_wallet_signature

        kp = Keypair.random()
        challenge = "test-challenge-b64"
        signature = base64.b64encode(self.sep53_sign(kp, challenge)).decode()

        assert verify_wallet_signature(kp.public_key, challenge, signature) is True

    def test_raw_signature_over_the_challenge_is_rejected(self):
        # This is the behaviour SEP-53 exists to prevent. A signature over the
        # bare challenge carries no domain separation: nothing distinguishes it
        # from the same key signing some other payload of the attacker's
        # choosing. Accepting it would defeat the point of adopting SEP-53.
        from research_agent.wallet_auth import verify_wallet_signature

        kp = Keypair.random()
        challenge = "test-challenge-raw"
        unframed = kp.sign(challenge.encode()).hex()

        assert verify_wallet_signature(kp.public_key, challenge, unframed) is False

    def test_wrong_key_rejects(self):
        from research_agent.wallet_auth import verify_wallet_signature

        kp = Keypair.random()
        wrong_kp = Keypair.random()
        challenge = "test-challenge-456"
        signature = self.sep53_sign(kp, challenge).hex()

        assert verify_wallet_signature(wrong_kp.public_key, challenge, signature) is False

    def test_signature_for_another_challenge_rejects(self):
        # Replaying a signature from one login attempt onto another must fail.
        from research_agent.wallet_auth import verify_wallet_signature

        kp = Keypair.random()
        signature = self.sep53_sign(kp, "challenge-one").hex()

        assert verify_wallet_signature(kp.public_key, "challenge-two", signature) is False

    def test_tampered_signature_rejects(self):
        from research_agent.wallet_auth import verify_wallet_signature

        kp = Keypair.random()
        challenge = "test-challenge-789"
        signature = "deadbeef" * 16  # hex garbage, right length

        assert verify_wallet_signature(kp.public_key, challenge, signature) is False

    def test_empty_challenge_rejects(self):
        from research_agent.wallet_auth import verify_wallet_signature

        kp = Keypair.random()
        assert verify_wallet_signature(kp.public_key, "", "aabb") is False

    def test_malformed_inputs_return_false_rather_than_raising(self):
        # Every one of these reaches the endpoint from the network, so none of
        # them may turn into a 500.
        from research_agent.wallet_auth import verify_wallet_signature

        kp = Keypair.random()
        good = self.sep53_sign(kp, "c").hex()

        assert verify_wallet_signature("not-a-key", "c", good) is False
        assert verify_wallet_signature(kp.public_key, "c", "zz zz") is False
        assert verify_wallet_signature(kp.public_key, "c", "aabb") is False  # too short
        assert verify_wallet_signature("", "c", good) is False


class TestSep53Digest:
    def test_prefix_is_the_one_the_spec_fixes(self):
        from research_agent.wallet_auth import SEP53_PREFIX

        assert SEP53_PREFIX == b"Stellar Signed Message:\n"

    def test_digest_matches_the_spec_construction(self):
        import hashlib

        from research_agent.wallet_auth import SEP53_PREFIX, sep53_digest

        expected = hashlib.sha256(SEP53_PREFIX + b"hello").digest()
        assert sep53_digest("hello") == expected
        assert sep53_digest(b"hello") == expected
        assert len(expected) == 32
