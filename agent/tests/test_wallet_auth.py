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

        ops_resp = MagicMock()
        ops_resp.status_code = 200
        ops_resp.json.return_value = {
            "_embedded": {
                "records": [
                    {"type": "invoke_contract"},
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
        assert result["wallet_address"] == "GABCDEFG12345"
        assert result["txHash"] == "tx_abc"

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
    def test_valid_signature_returns_public_key(self):
        from research_agent.wallet_auth import verify_wallet_signature

        kp = Keypair.random()
        challenge = "test-challenge-123"
        signature = kp.sign(challenge.encode()).hex()

        result = verify_wallet_signature(kp.public_key, challenge, signature)
        assert result is True

    def test_wrong_key_rejects(self):
        from research_agent.wallet_auth import verify_wallet_signature

        kp = Keypair.random()
        wrong_kp = Keypair.random()
        challenge = "test-challenge-456"
        signature = kp.sign(challenge.encode()).hex()

        result = verify_wallet_signature(wrong_kp.public_key, challenge, signature)
        assert result is False

    def test_tampered_signature_rejects(self):
        from research_agent.wallet_auth import verify_wallet_signature

        kp = Keypair.random()
        challenge = "test-challenge-789"
        signature = "deadbeef" * 16  # hex garbage

        result = verify_wallet_signature(kp.public_key, challenge, signature)
        assert result is False

    def test_empty_challenge_rejects(self):
        from research_agent.wallet_auth import verify_wallet_signature

        kp = Keypair.random()
        result = verify_wallet_signature(kp.public_key, "", "aabb")
        assert result is False
