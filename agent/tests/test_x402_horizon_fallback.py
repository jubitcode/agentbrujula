"""Horizon fallback must reject Soroban `invoke_contract` ops.

Soroban payments must go through the facilitator. If the facilitator is down,
the gate returns 402 and the client retries until recovery (fail-closed).
The fallback only accepts classic Stellar `payment` ops."""

import sys
from pathlib import Path
from unittest.mock import patch, AsyncMock

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "src"))

from research_agent.x402_middleware import _verify_tx_on_horizon


def _mock_horizon(tx_success: bool, ops: list[dict]):
    """Fake Horizon AsyncClient returning canned tx + ops payloads."""
    class FakeResp:
        def __init__(self, status_code, payload):
            self.status_code = status_code
            self._payload = payload
        def json(self):
            return self._payload

    async def fake_get(url, *args, **kwargs):
        if "/operations" in url:
            return FakeResp(200, {"_embedded": {"records": ops}})
        else:
            return FakeResp(200, {"successful": tx_success, "source_account": "Gattacker"})

    fake_client = AsyncMock()
    fake_client.__aenter__.return_value = fake_client
    fake_client.__aexit__.return_value = None
    fake_client.get = fake_get
    return fake_client


@pytest.mark.asyncio
async def test_classic_payment_to_correct_recipient_accepted():
    pay_to = "GESCROW00000000000000000000000000000000000000000000000000"
    ops = [{
        "type": "payment",
        "to": pay_to,
        "amount": "0.10",
        "asset_code": "USDC",
    }]
    with patch("research_agent.x402_middleware.httpx.AsyncClient",
               return_value=_mock_horizon(True, ops)):
        result = await _verify_tx_on_horizon(
            "tx-hash-1", pay_to=pay_to,
            expected_amount="0.10", expected_asset="USDC",
        )
    assert result["success"] is True
    assert result["txHash"] == "tx-hash-1"


@pytest.mark.asyncio
async def test_invoke_contract_op_rejected_even_if_successful():
    """invoke_contract on the EXPECTED escrow address must be rejected — Horizon JSON args aren't trustworthy without ScVal decoding."""
    pay_to = "GESCROW00000000000000000000000000000000000000000000000000"
    ops = [{
        "type": "invoke_contract",
        "address": pay_to,
        "function_name": "create_escrow",
    }]
    with patch("research_agent.x402_middleware.httpx.AsyncClient",
               return_value=_mock_horizon(True, ops)):
        result = await _verify_tx_on_horizon(
            "tx-hash-2", pay_to=pay_to,
            expected_amount="0.10", expected_asset="USDC",
        )
    assert result["success"] is False, (
        "Soroban invoke_contract ops must NOT be accepted by the fallback. "
        "Facilitator must validate contract args; fail-closed if facilitator down."
    )


@pytest.mark.asyncio
async def test_invoke_contract_on_attacker_contract_rejected():
    """Attacker deploys a no-op contract, invokes it, submits the tx hash as payment — must be rejected."""
    pay_to = "GESCROW00000000000000000000000000000000000000000000000000"
    attacker_contract = "CATTACKER000000000000000000000000000000000000000000000000"
    ops = [{
        "type": "invoke_contract",
        "address": attacker_contract,
        "function_name": "noop",
    }]
    with patch("research_agent.x402_middleware.httpx.AsyncClient",
               return_value=_mock_horizon(True, ops)):
        result = await _verify_tx_on_horizon(
            "tx-hash-3", pay_to=pay_to,
            expected_amount="0.10", expected_asset="USDC",
        )
    assert result["success"] is False


@pytest.mark.asyncio
async def test_invoke_host_function_op_also_rejected():
    """Some Horizon versions surface Soroban as invoke_host_function — the fallback must skip it too (only payment ops pass)."""
    pay_to = "GESCROW00000000000000000000000000000000000000000000000000"
    ops = [{"type": "invoke_host_function", "function": "HostFunctionTypeInvokeContract"}]
    with patch("research_agent.x402_middleware.httpx.AsyncClient",
               return_value=_mock_horizon(True, ops)):
        result = await _verify_tx_on_horizon(
            "tx-hash-4", pay_to=pay_to,
            expected_amount="0.10", expected_asset="USDC",
        )
    assert result["success"] is False


@pytest.mark.asyncio
async def test_failed_tx_rejected():
    pay_to = "GESCROW00000000000000000000000000000000000000000000000000"
    ops = [{"type": "payment", "to": pay_to, "amount": "0.10", "asset_code": "USDC"}]
    with patch("research_agent.x402_middleware.httpx.AsyncClient",
               return_value=_mock_horizon(False, ops)):
        result = await _verify_tx_on_horizon(
            "tx-hash-5", pay_to=pay_to,
            expected_amount="0.10", expected_asset="USDC",
        )
    assert result["success"] is False
