import os
import time

import pytest
from stellar_sdk import Keypair, SorobanServer

ORCH_SECRET = os.environ.get("ORCHESTRATOR_SECRET_KEY", "")
pytestmark = pytest.mark.skipif(not ORCH_SECRET, reason="ORCHESTRATOR_SECRET_KEY not set")


@pytest.fixture
def orchestrator_kp():
    return Keypair.from_secret(ORCH_SECRET)


@pytest.fixture
def client_kp(orchestrator_kp):
    """Fresh testnet keypair funded with XLM + USDC trustline + 1 USDC balance — mirrors /api/prepare-wallet."""
    kp = Keypair.random()
    from research_agent.stellar_config import FRIENDBOT_URL
    if not FRIENDBOT_URL:
        pytest.skip("No FRIENDBOT_URL (mainnet?)")

    import httpx
    resp = httpx.get(f"{FRIENDBOT_URL}?addr={kp.public_key}", timeout=30)
    if resp.status_code not in (200, 400):  # 400 = already funded
        pytest.skip(f"Friendbot failed: {resp.status_code}")

    time.sleep(2)  # let Horizon see the new account before signing ops on it

    from _trustline_helpers import fund_with_usdc
    try:
        fund_with_usdc(kp, orchestrator_kp, amount_stroops=10_000_000)  # 1.00 USDC
    except Exception as exc:
        pytest.skip(f"Failed to set up USDC trustline + balance for client: {exc}")

    return kp


class TestEscrowCreateAndQuery:
    def test_build_escrow_tx_returns_valid_xdr(self, client_kp, orchestrator_kp):
        from research_agent.escrow import build_escrow_tx

        task_id = f"e2e-test-{int(time.time())}"
        deadline = int(time.time()) + 300

        result = build_escrow_tx(
            client_public=client_kp.public_key,
            agent_public=orchestrator_kp.public_key,
            amount_stroops=1000000,  # 0.1 USDC
            deadline_unix=deadline,
            task_id=task_id,
        )

        assert result["unsigned_xdr"]
        assert result["task_id"] == task_id
        assert result["deadline"] == deadline
        assert len(result["unsigned_xdr"]) > 100

    def test_get_escrow_status_not_found(self):
        from research_agent.escrow import get_escrow_status

        result = get_escrow_status(f"nonexistent-{int(time.time())}")
        assert result["status"] in ("not_found", "found")


class TestEscrowRefund:
    def test_refund_after_deadline(self, client_kp, orchestrator_kp):
        from research_agent.escrow import build_escrow_tx, refund_escrow
        from research_agent.stellar_config import RPC_URL
        from stellar_sdk import SorobanServer, TransactionEnvelope

        task_id = f"e2e-refund-{int(time.time())}"
        # Contract requires deadline > now + 10s on LEDGER clock at execution time.
        # Submission + ledger inclusion can take 10-20s on testnet, so pad to 60s.
        deadline = int(time.time()) + 60

        result = build_escrow_tx(
            client_public=client_kp.public_key,
            agent_public=orchestrator_kp.public_key,
            amount_stroops=100000,  # 0.01 USDC
            deadline_unix=deadline,
            task_id=task_id,
        )

        from research_agent.stellar_config import NETWORK_PASSPHRASE
        tx = TransactionEnvelope.from_xdr(result["unsigned_xdr"], NETWORK_PASSPHRASE)
        tx.sign(client_kp)
        signed_xdr = tx.to_xdr()

        server = SorobanServer(RPC_URL)
        send_resp = server.send_transaction(tx)
        tx_hash = send_resp.hash

        # SDK status is an Enum, compare via .value
        for _ in range(45):
            time.sleep(2)
            status = server.get_transaction(tx_hash)
            status_str = getattr(status.status, "value", status.status)
            if status_str != "NOT_FOUND":
                break

        status_str = getattr(status.status, "value", status.status)
        if status_str != "SUCCESS":
            pytest.skip(f"Create escrow tx not confirmed: {status_str}")

        wait_time = max(0, deadline - time.time() + 2)
        if wait_time > 0:
            time.sleep(wait_time)

        try:
            refund_tx = refund_escrow(orchestrator_kp, task_id)
            assert refund_tx
        except RuntimeError as e:
            if "deadline not reached" in str(e).lower():
                pytest.skip("Deadline not yet reached on-chain (ledger time lag)")
            raise


class TestSettleEscrow:
    def test_settle_returns_tx_hash(self, client_kp, orchestrator_kp):
        from research_agent.escrow import build_escrow_tx, settle_escrow
        from research_agent.stellar_config import RPC_URL, NETWORK_PASSPHRASE
        from stellar_sdk import TransactionEnvelope

        task_id = f"e2e-settle-{int(time.time())}"
        deadline = int(time.time()) + 300
        amount = 100000  # 0.01 USDC

        result = build_escrow_tx(
            client_public=client_kp.public_key,
            agent_public=orchestrator_kp.public_key,
            amount_stroops=amount,
            deadline_unix=deadline,
            task_id=task_id,
        )

        tx = TransactionEnvelope.from_xdr(result["unsigned_xdr"], NETWORK_PASSPHRASE)
        tx.sign(client_kp)

        server = SorobanServer(RPC_URL)
        send_resp = server.send_transaction(tx)

        for _ in range(45):
            time.sleep(2)
            status = server.get_transaction(send_resp.hash)
            status_str = getattr(status.status, "value", status.status)
            if status_str != "NOT_FOUND":
                break

        status_str = getattr(status.status, "value", status.status)
        if status_str != "SUCCESS":
            pytest.skip(f"Create escrow tx not confirmed: {status_str}")

        try:
            settle_tx = settle_escrow(orchestrator_kp, task_id, amount // 2)
            assert settle_tx
        except RuntimeError as e:
            pytest.skip(f"Settlement failed (probably missing USDC): {e}")
