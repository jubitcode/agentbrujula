"""On mainnet the orchestrator is a 2-of-3 multisig account, not a single hot key.
Operations like refund_escrow / settle_escrow must build an unsigned XDR for off-line
co-signing instead of `tx.sign(kp)` from a single env-var secret."""

from __future__ import annotations

import os
from unittest.mock import MagicMock, patch

import pytest
from stellar_sdk import Keypair, TransactionEnvelope

from research_agent.stellar_config import NETWORK_PASSPHRASE
from research_agent.escrow import (
    build_unsigned_refund,
    build_unsigned_settle,
    build_unsigned_settle_team,
    refund_escrow,
)


# Pubkey doesn't need to exist on-chain — load_account is mocked
_FAKE_ORCH_PUBKEY = Keypair.random().public_key
_FAKE_TASK_ID = "task-multisig-test"


@pytest.fixture
def mock_soroban_server():
    """Mock SorobanServer so build_unsigned_* doesn't hit the real network."""
    with patch("research_agent.escrow.SorobanServer") as MockServer:
        instance = MockServer.return_value

        from stellar_sdk import Account
        instance.load_account.return_value = Account(_FAKE_ORCH_PUBKEY, 1)

        sim = MagicMock()
        sim.error = None
        sim.results = []
        sim.min_resource_fee = 100
        instance.simulate_transaction.return_value = sim

        # Pass-through is sufficient to preserve XDR shape for these tests
        instance.prepare_transaction.side_effect = lambda tx, sim: tx

        yield instance


def test_unsigned_refund_has_no_signatures(mock_soroban_server):
    result = build_unsigned_refund(
        source_public=_FAKE_ORCH_PUBKEY,
        task_id=_FAKE_TASK_ID,
        escrow_contract="CAFERYYMP63F2TW7UZTOVG3RMKXSUSJBDWD3EK33UYX3PFK42H45272A",
    )
    assert "unsigned_xdr" in result
    envelope = TransactionEnvelope.from_xdr(result["unsigned_xdr"], NETWORK_PASSPHRASE)
    assert envelope.signatures == [], (
        f"unsigned XDR must have no signatures, got {len(envelope.signatures)}"
    )


def test_unsigned_settle_has_no_signatures(mock_soroban_server):
    result = build_unsigned_settle(
        source_public=_FAKE_ORCH_PUBKEY,
        task_id=_FAKE_TASK_ID,
        agent_amount_stroops=500_000,
        escrow_contract="CAFERYYMP63F2TW7UZTOVG3RMKXSUSJBDWD3EK33UYX3PFK42H45272A",
    )
    envelope = TransactionEnvelope.from_xdr(result["unsigned_xdr"], NETWORK_PASSPHRASE)
    assert envelope.signatures == []


def test_unsigned_settle_team_has_no_signatures(mock_soroban_server):
    splits = [
        (Keypair.random().public_key, 100_000),
        (Keypair.random().public_key, 200_000),
    ]
    result = build_unsigned_settle_team(
        source_public=_FAKE_ORCH_PUBKEY,
        task_id=_FAKE_TASK_ID,
        splits=splits,
        escrow_contract="CAFERYYMP63F2TW7UZTOVG3RMKXSUSJBDWD3EK33UYX3PFK42H45272A",
    )
    envelope = TransactionEnvelope.from_xdr(result["unsigned_xdr"], NETWORK_PASSPHRASE)
    assert envelope.signatures == []


def test_external_signing_mode_blocks_autosign(monkeypatch):
    """ORCHESTRATOR_SIGNING_MODE=external must make refund_escrow refuse to autosign."""
    monkeypatch.setenv("ORCHESTRATOR_SIGNING_MODE", "external")
    fake_kp = Keypair.random()

    with pytest.raises(RuntimeError, match="external"):
        refund_escrow(fake_kp, _FAKE_TASK_ID)


def test_default_signing_mode_allows_autosign(monkeypatch, mock_soroban_server):
    """Default 'single' mode still autosigns; wait_for_confirmation=False bypasses the poll-loop mock,
    leaving status='SUBMITTED' which is enough to prove auto-signing wasn't blocked."""
    monkeypatch.delenv("ORCHESTRATOR_SIGNING_MODE", raising=False)
    fake_kp = Keypair.random()

    sent = MagicMock()
    sent.hash = "fake-tx-hash"
    mock_soroban_server.send_transaction.return_value = sent

    from stellar_sdk import Account
    mock_soroban_server.load_account.return_value = Account(fake_kp.public_key, 1)

    result = refund_escrow(
        fake_kp,
        _FAKE_TASK_ID,
        escrow_contract="CAMPNA3TNE23UYM5WSTXHQWA5HGU7HL6M2T64R5VNQT5PJS6UBIXOCCO",
        wait_for_confirmation=False,
    )
    assert result == {"hash": "fake-tx-hash", "status": "SUBMITTED", "error": None}
