from __future__ import annotations

import time

import pytest
from stellar_sdk import Keypair, Server

from research_agent.soroban_client import verify_compression
from research_agent.stellar_identity import fund_testnet, HORIZON_TESTNET
from research_agent.escrow import create_escrow, claim_escrow, reclaim_escrow


class TestCompressionCompliance:
    def test_compliant_output(self):
        result = verify_compression(
            task_id="test-comply-1",
            input_tokens=1000,
            output_tokens=200,
            max_ratio_pct=50,
        )
        assert result is True  # 20% <= 50%

    def test_bloated_output_rejected(self):
        result = verify_compression(
            task_id="test-bloat-1",
            input_tokens=1000,
            output_tokens=900,
            max_ratio_pct=50,
        )
        assert result is False  # 90% > 50%

    def test_exact_boundary_passes(self):
        result = verify_compression(
            task_id="test-boundary-1",
            input_tokens=1000,
            output_tokens=500,
            max_ratio_pct=50,
        )
        assert result is True  # 50% <= 50%

    def test_zero_input_rejected(self):
        result = verify_compression(
            task_id="test-zero-1",
            input_tokens=0,
            output_tokens=100,
            max_ratio_pct=50,
        )
        assert result is False

    def test_small_ratio_strict(self):
        result = verify_compression(
            task_id="test-strict-1",
            input_tokens=1000,
            output_tokens=150,
            max_ratio_pct=10,
        )
        assert result is False  # 15% > 10%

    def test_large_ratio_lenient(self):
        result = verify_compression(
            task_id="test-lenient-1",
            input_tokens=100,
            output_tokens=95,
            max_ratio_pct=100,
        )
        assert result is True  # 95% <= 100%


class TestComplianceGatesEscrow:
    """Compression compliance gates escrow release: non-compliant output → reclaim, agent gets nothing."""

    def test_bloated_output_blocks_escrow_release(self):
        orch_kp = Keypair.random()
        agent_kp = Keypair.random()
        assert fund_testnet(orch_kp.public_key)
        assert fund_testnet(agent_kp.public_key)
        time.sleep(3)

        deadline = int(time.time()) + 10
        balance_id, _ = create_escrow(
            orchestrator_kp=orch_kp,
            agent_public=agent_kp.public_key,
            amount_xlm="0.5",
            deadline_unix=deadline,
        )
        time.sleep(2)

        compliant = verify_compression(
            task_id=f"gate-test-{int(time.time())}",
            input_tokens=100,
            output_tokens=900,
            max_ratio_pct=50,
        )
        assert compliant is False  # 900% > 50%

        time.sleep(15)  # wait past deadline before reclaim
        tx_hash = reclaim_escrow(orch_kp, balance_id)
        assert tx_hash is not None

        with pytest.raises(Exception):
            claim_escrow(agent_kp, balance_id)

    def test_compliant_output_allows_agent_claim(self):
        orch_kp = Keypair.random()
        agent_kp = Keypair.random()
        assert fund_testnet(orch_kp.public_key)
        assert fund_testnet(agent_kp.public_key)
        time.sleep(3)

        deadline = int(time.time()) + 300
        balance_id, _ = create_escrow(
            orchestrator_kp=orch_kp,
            agent_public=agent_kp.public_key,
            amount_xlm="0.5",
            deadline_unix=deadline,
        )
        time.sleep(2)

        compliant = verify_compression(
            task_id=f"pass-test-{int(time.time())}",
            input_tokens=1000,
            output_tokens=200,
            max_ratio_pct=50,
        )
        assert compliant is True

        tx_hash = claim_escrow(agent_kp, balance_id)
        assert tx_hash is not None
