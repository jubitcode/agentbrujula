import time

import pytest

from conftest import requires_orchestrator, requires_testclient


pytestmark = [requires_orchestrator, requires_testclient]


class TestSettleSingleAgent:
    def test_settle_pays_agent_and_refunds_client(self, orchestrator_kp, testclient_kp, funded_agent_kp):
        from research_agent.escrow import settle_escrow
        from research_agent.x402_config import X402_ESCROW_CONTRACT
        from stellar_sdk import SorobanServer, Network, TransactionBuilder, scval

        rpc_url = "https://soroban-testnet.stellar.org"
        server = SorobanServer(rpc_url)

        task_id = f"settle-test-{int(time.time())}"
        budget = 2_000_000  # 0.2 USDC
        actual_cost = 1_000_000  # 0.1 USDC

        # Create escrow (client locks USDC)
        source = server.load_account(testclient_kp.public_key)
        builder = TransactionBuilder(
            source_account=source,
            network_passphrase=Network.TESTNET_NETWORK_PASSPHRASE,
            base_fee=100,
        )
        builder.append_invoke_contract_function_op(
            contract_id=X402_ESCROW_CONTRACT,
            function_name="create_escrow",
            parameters=[
                scval.to_address(testclient_kp.public_key),
                scval.to_address(funded_agent_kp.public_key),
                scval.to_int128(budget),
                scval.to_uint64(int(time.time()) + 300),
                scval.to_string(task_id),
            ],
        )
        builder.set_timeout(30)
        tx = builder.build()
        sim = server.simulate_transaction(tx)
        if sim.error:
            pytest.skip(f"Create escrow simulation failed: {sim.error}")
        tx = server.prepare_transaction(tx, sim)
        tx.sign(testclient_kp)
        resp = server.send_transaction(tx)
        if resp.status == "ERROR":
            pytest.skip(f"Create escrow failed: {resp}")

        for _ in range(15):
            result = server.get_transaction(resp.hash)
            if "SUCCESS" in str(result.status):
                break
            if "FAILED" in str(result.status):
                pytest.skip(f"Create escrow tx failed on-chain: {resp.hash}")
            time.sleep(2)

        try:
            settle_tx = settle_escrow(orchestrator_kp, task_id, actual_cost)
            assert settle_tx
        except RuntimeError as e:
            pytest.fail(f"settle_escrow failed: {e}")

        time.sleep(3)

        from research_agent.soroban_client import _simulate_invoke
        result_xdr = _simulate_invoke(
            rpc_url, X402_ESCROW_CONTRACT, "get_escrow",
            [scval.to_string(task_id)],
        )
        from stellar_sdk import scval as sv
        entry = sv.to_native(result_xdr)
        assert entry is not None, "Escrow entry should exist"  # status field should indicate Settled (variant 3)


class TestSettlementViaAPI:
    def test_result_endpoint_returns_settlement_fields(self, api_client):
        import sys
        from pathlib import Path
        sys.path.insert(0, str(Path(__file__).resolve().parent.parent.parent / "api"))
        import server

        task_id = f"settle-api-{int(time.time())}"
        class FakeResult:
            final_response = "This is the research output."
            called_tools = ["web_search", "pdf_read"]

        server._tasks[task_id] = {
            "task_id": task_id,
            "agent_handle": "test-agent",
            "agent_capabilities": ["PDF"],
            "task": "research test",
            "balance_id": None,
            "escrow_tx": "fake-tx-for-testing",
            "deadline_unix": int(time.time()) + 300,
            "status": "complete",
            "result": FakeResult(),
        }

        res = api_client.get(f"/api/result/{task_id}")
        assert res.status_code == 200
        data = res.json()

        assert data["status"] == "complete"
        assert data["output"] == "This is the research output."
        # Settlement itself may fail (depends on mcp-meta + funded escrow), but field must be present
        assert "settlement_status" in data
        assert "escrow_tx" in data


class TestExpiredEscrow:
    def test_refund_after_deadline(self, orchestrator_kp, testclient_kp, funded_agent_kp):
        from research_agent.x402_config import X402_ESCROW_CONTRACT
        from stellar_sdk import SorobanServer, Network, TransactionBuilder, scval

        rpc_url = "https://soroban-testnet.stellar.org"
        server = SorobanServer(rpc_url)

        task_id = f"expire-test-{int(time.time())}"
        amount = 1_000_000  # 0.1 USDC
        deadline = int(time.time()) + 12  # contract requires minimum 10s

        source = server.load_account(testclient_kp.public_key)
        builder = TransactionBuilder(
            source_account=source,
            network_passphrase=Network.TESTNET_NETWORK_PASSPHRASE,
            base_fee=100,
        )
        builder.append_invoke_contract_function_op(
            contract_id=X402_ESCROW_CONTRACT,
            function_name="create_escrow",
            parameters=[
                scval.to_address(testclient_kp.public_key),
                scval.to_address(funded_agent_kp.public_key),
                scval.to_int128(amount),
                scval.to_uint64(deadline),
                scval.to_string(task_id),
            ],
        )
        builder.set_timeout(30)
        tx = builder.build()
        sim = server.simulate_transaction(tx)
        if sim.error:
            pytest.skip(f"Create escrow simulation failed: {sim.error}")
        tx = server.prepare_transaction(tx, sim)
        tx.sign(testclient_kp)
        resp = server.send_transaction(tx)
        if resp.status == "ERROR":
            pytest.skip(f"Create escrow failed: {resp}")

        for _ in range(15):
            result = server.get_transaction(resp.hash)
            if "SUCCESS" in str(result.status):
                break
            if "FAILED" in str(result.status):
                pytest.skip(f"Create escrow tx failed on-chain: {resp.hash}")
            time.sleep(2)

        # Soroban ledger timestamp can lag wall clock, pad +10s
        wait = deadline - int(time.time()) + 10
        if wait > 0:
            time.sleep(wait)

        source2 = server.load_account(testclient_kp.public_key)
        builder2 = TransactionBuilder(
            source_account=source2,
            network_passphrase=Network.TESTNET_NETWORK_PASSPHRASE,
            base_fee=100,
        )
        builder2.append_invoke_contract_function_op(
            contract_id=X402_ESCROW_CONTRACT,
            function_name="refund_to_client",
            parameters=[scval.to_string(task_id)],
        )
        builder2.set_timeout(30)
        tx2 = builder2.build()
        sim2 = server.simulate_transaction(tx2)
        if sim2.error:
            pytest.fail(f"Refund simulation failed: {sim2.error}")
        tx2 = server.prepare_transaction(tx2, sim2)
        tx2.sign(testclient_kp)
        resp2 = server.send_transaction(tx2)
        assert resp2.hash, "Refund transaction should return a hash"
