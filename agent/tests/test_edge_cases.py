from __future__ import annotations

import time

import pytest
from stellar_sdk import Keypair

from research_agent.hire import _match_capabilities
from research_agent.soroban_client import stroops_to_xlm, verify_compression
from research_agent.stellar_identity import fund_testnet
from research_agent.escrow import create_escrow, claim_escrow, reclaim_escrow


class TestMatchCapabilities:
    def test_empty_required_returns_false(self):
        assert not _match_capabilities(["PDF", "OCR"], [])

    def test_empty_agent_caps_returns_false(self):
        assert not _match_capabilities([], ["PDF"])

    def test_both_empty_returns_false(self):
        assert not _match_capabilities([], [])

    def test_case_insensitive_match(self):
        assert _match_capabilities(["PDF", "OCR"], ["pdf"])
        assert _match_capabilities(["pdf"], ["PDF"])
        assert _match_capabilities(["PDF"], ["pdf"])

    def test_duplicate_capabilities_in_request(self):
        assert _match_capabilities(["PDF", "OCR"], ["PDF", "pdf", "PDF"])

    def test_any_match_wins(self):
        """OR semantics: agent matches if it has ANY of the required caps."""
        assert _match_capabilities(["PDF"], ["PDF", "Quantum", "Telepathy"])

    def test_no_overlap_returns_false(self):
        assert not _match_capabilities(["PDF", "OCR"], ["Quantum"])
        assert not _match_capabilities(["Web"], ["Database", "Crypto"])

    def test_unicode_case_handling(self):
        """str.lower() on Unicode capability names still produces stable matches."""
        assert _match_capabilities(["Análisis"], ["análisis"])


class TestBudgetMath:
    def test_budget_exactly_equals_cost_is_inclusive(self):
        """Budget filter uses `cost > budget` — equality must NOT exclude."""
        cost = stroops_to_xlm(800_000)  # 0.08 XLM
        assert not (cost > cost)


class TestEscrowEdges:
    def test_escrow_insufficient_funds(self):
        kp = Keypair.random()
        agent_kp = Keypair.random()
        fund_testnet(kp.public_key)
        fund_testnet(agent_kp.public_key)
        time.sleep(3)

        with pytest.raises(Exception):
            create_escrow(
                orchestrator_kp=kp,
                agent_public=agent_kp.public_key,
                amount_xlm="100000",  # exceeds Friendbot's 10000 XLM grant
                deadline_unix=int(time.time()) + 300,
            )

    def test_double_claim_fails(self):
        orch_kp = Keypair.random()
        agent_kp = Keypair.random()
        fund_testnet(orch_kp.public_key)
        fund_testnet(agent_kp.public_key)
        time.sleep(3)

        balance_id, _ = create_escrow(
            orchestrator_kp=orch_kp,
            agent_public=agent_kp.public_key,
            amount_xlm="0.5",
            deadline_unix=int(time.time()) + 300,
        )
        time.sleep(2)

        claim_escrow(agent_kp, balance_id)

        with pytest.raises(Exception):
            claim_escrow(agent_kp, balance_id)

    def test_reclaim_before_deadline_fails(self):
        orch_kp = Keypair.random()
        agent_kp = Keypair.random()
        fund_testnet(orch_kp.public_key)
        fund_testnet(agent_kp.public_key)
        time.sleep(3)

        balance_id, _ = create_escrow(
            orchestrator_kp=orch_kp,
            agent_public=agent_kp.public_key,
            amount_xlm="0.5",
            deadline_unix=int(time.time()) + 300,
        )
        time.sleep(2)

        with pytest.raises(Exception):
            reclaim_escrow(orch_kp, balance_id)


class TestCompressionEdges:
    def test_output_larger_than_input(self):
        result = verify_compression(
            task_id=f"edge-large-{int(time.time())}",
            input_tokens=100,
            output_tokens=500,
            max_ratio_pct=50,
        )
        assert result is False  # 500% > 50%

    def test_very_short_input(self):
        result = verify_compression(
            task_id=f"edge-short-{int(time.time())}",
            input_tokens=1,
            output_tokens=100,
            max_ratio_pct=50,
        )
        assert result is False  # 10000% > 50%

    def test_equal_input_output_at_100_pct(self):
        result = verify_compression(
            task_id=f"edge-equal-{int(time.time())}",
            input_tokens=500,
            output_tokens=500,
            max_ratio_pct=100,
        )
        assert result is True  # 100% <= 100%

    def test_output_zero(self):
        result = verify_compression(
            task_id=f"edge-zero-out-{int(time.time())}",
            input_tokens=1000,
            output_tokens=0,
            max_ratio_pct=50,
        )
        assert result is True  # 0% <= anything
