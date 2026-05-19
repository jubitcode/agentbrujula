from __future__ import annotations

import time

import pytest

from stellar_sdk import Keypair, Server

from research_agent.stellar_identity import fund_testnet, HORIZON_TESTNET
from research_agent.escrow import create_escrow, claim_escrow, reclaim_escrow
from research_agent.soroban_client import submit_claim_task


@pytest.fixture(scope="module")
def horizon():
    return Server(HORIZON_TESTNET)


@pytest.fixture(scope="module")
def orchestrator_kp():
    kp = Keypair.random()
    assert fund_testnet(kp.public_key)
    time.sleep(2)
    return kp


@pytest.fixture(scope="module")
def agent_kp():
    kp = Keypair.random()
    assert fund_testnet(kp.public_key)
    time.sleep(2)
    return kp


class TestEscrowCreation:
    def test_create_escrow(self, orchestrator_kp, agent_kp):
        deadline = int(time.time()) + 300
        balance_id, _ = create_escrow(
            orchestrator_kp=orchestrator_kp,
            agent_public=agent_kp.public_key,
            amount_xlm="1.0",
            deadline_unix=deadline,
        )
        assert balance_id is not None
        assert len(balance_id) > 0

    def test_escrow_visible_on_horizon(self, orchestrator_kp, horizon):
        time.sleep(2)
        resp = horizon.claimable_balances().for_sponsor(orchestrator_kp.public_key).call()
        records = resp["_embedded"]["records"]
        assert len(records) >= 1


class TestAgentClaim:
    def test_agent_claims_escrow(self, orchestrator_kp, agent_kp):
        deadline = int(time.time()) + 300
        balance_id, _ = create_escrow(
            orchestrator_kp=orchestrator_kp,
            agent_public=agent_kp.public_key,
            amount_xlm="0.5",
            deadline_unix=deadline,
        )
        time.sleep(2)

        tx_hash = claim_escrow(agent_kp, balance_id)
        assert tx_hash is not None
        assert len(tx_hash) == 64

    def test_agent_balance_increased(self, agent_kp, horizon):
        time.sleep(2)
        account = horizon.accounts().account_id(agent_kp.public_key).call()
        xlm = next(b for b in account["balances"] if b["asset_type"] == "native")
        assert float(xlm["balance"]) > 9999


class TestTimeoutReclaim:
    def test_orchestrator_reclaims_after_timeout(self):
        orch_kp = Keypair.random()
        agent_kp = Keypair.random()
        assert fund_testnet(orch_kp.public_key)
        assert fund_testnet(agent_kp.public_key)
        time.sleep(3)

        deadline = int(time.time()) + 5
        balance_id, _ = create_escrow(
            orchestrator_kp=orch_kp,
            agent_public=agent_kp.public_key,
            amount_xlm="0.5",
            deadline_unix=deadline,
        )
        time.sleep(2)
        time.sleep(10)  # wait past deadline

        tx_hash = reclaim_escrow(orch_kp, balance_id)
        assert tx_hash is not None
        assert len(tx_hash) == 64


class TestAgentCannotClaimAfterDeadline:
    def test_agent_rejected_after_deadline(self):
        orch_kp = Keypair.random()
        agent_kp = Keypair.random()
        assert fund_testnet(orch_kp.public_key)
        assert fund_testnet(agent_kp.public_key)
        time.sleep(3)

        deadline = int(time.time()) + 5
        balance_id, _ = create_escrow(
            orchestrator_kp=orch_kp,
            agent_public=agent_kp.public_key,
            amount_xlm="0.5",
            deadline_unix=deadline,
        )
        time.sleep(2)
        time.sleep(10)  # wait past deadline

        with pytest.raises(Exception):
            claim_escrow(agent_kp, balance_id)


class TestOnChainTaskLifecycle:
    def test_claim_task_on_chain(self):
        orch_kp = Keypair.random()
        assert fund_testnet(orch_kp.public_key)
        time.sleep(3)

        task_id = f"test-task-{int(time.time())}"
        deadline = int(time.time()) + 300

        tx_hash = submit_claim_task(
            requester_kp=orch_kp,
            task_id=task_id,
            handle="pdf-reader",
            deadline=deadline,
        )
        assert tx_hash is not None
        assert len(tx_hash) > 0
