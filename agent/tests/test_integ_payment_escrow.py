import os
import time

import pytest

from conftest import requires_orchestrator, requires_testclient


class TestBuildPayment:
    pytestmark = [requires_orchestrator]

    def test_build_payment_for_real_agent(self, api_client):
        discover = api_client.post("/api/discover", json={
            "task": "test", "capabilities": ["PDF"], "budget_xlm": 1000.0,
        })
        if discover.status_code != 200:
            pytest.skip("Cannot discover agents")
        agents = discover.json().get("agents", [])
        if not agents:
            pytest.skip("No agents registered on testnet")

        from conftest import _stellar_public
        user_pub = _stellar_public("testclient")
        if not user_pub:
            pytest.skip("testclient public key not available")

        res = api_client.post("/api/build-payment", json={
            "agent_handle": agents[0]["handle"],
            "amount_usdc": "0.10",
            "user_public_key": user_pub,
        })

        if res.status_code in (500, 503):
            detail = res.json().get("detail", "")
            pytest.skip(f"Soroban RPC or contract not available: {detail}")
        data = res.json()
        assert "unsigned_xdr" in data
        assert len(data["unsigned_xdr"]) > 50
        assert data["amount_usdc"] == "0.10"
        assert data["token"] == "USDC"
        assert data["destination"] == os.environ.get(
            "X402_ESCROW_CONTRACT",
            "CDZ2YL6K2MWL72H6Q4L4JEXWNHV43EXUYEZWARLRQF7O36MFBZG4RSTR",
        )

    def test_build_payment_unknown_agent_404(self, api_client):
        from conftest import _stellar_public
        user_pub = _stellar_public("testclient") or "GBZF3HIHM5NOWLNI6QHTOOXCNRMXGR76UHCVUQJXW5TGXN2AF3MQT5I"

        res = api_client.post("/api/build-payment", json={
            "agent_handle": "nonexistent-agent-xyz",
            "amount_usdc": "0.10",
            "user_public_key": user_pub,
        })
        assert res.status_code == 404

    def test_build_payment_missing_public_key_422(self, api_client):
        res = api_client.post("/api/build-payment", json={
            "agent_handle": "test",
            "amount_usdc": "0.10",
        })
        assert res.status_code == 422


class TestSubmitPayment:
    """Requires real signed XDR and funded USDC on testclient."""

    pytestmark = [requires_orchestrator, requires_testclient]

    def test_submit_payment_creates_task(self, api_client, testclient_kp):
        discover = api_client.post("/api/discover", json={
            "task": "test", "capabilities": ["PDF"], "budget_xlm": 1000.0,
        })
        if discover.status_code != 200:
            pytest.skip("Cannot discover agents")
        agents = discover.json().get("agents", [])
        if not agents:
            pytest.skip("No agents registered")

        agent_handle = agents[0]["handle"]

        build_res = api_client.post("/api/build-payment", json={
            "agent_handle": agent_handle,
            "amount_usdc": "0.01",
            "user_public_key": testclient_kp.public_key,
        })
        if build_res.status_code in (500, 503):
            detail = build_res.json().get("detail", "")
            pytest.skip(f"Soroban RPC or contract not available: {detail}")

        unsigned_xdr = build_res.json()["unsigned_xdr"]

        from stellar_sdk import TransactionEnvelope, Network
        envelope = TransactionEnvelope.from_xdr(unsigned_xdr, Network.TESTNET_NETWORK_PASSPHRASE)
        envelope.sign(testclient_kp)
        signed_xdr = envelope.to_xdr()

        submit_res = api_client.post("/api/submit-payment", json={
            "signed_xdr": signed_xdr,
            "agent_handle": agent_handle,
            "task": "integration test task",
            "budget_xlm": 5.0,
        })

        if submit_res.status_code == 502:
            detail = submit_res.json().get("detail", "")
            if "insufficient balance" in detail.lower() or "failed" in detail.lower():
                pytest.skip(f"Testclient may not have USDC: {detail}")
            pytest.fail(f"Submit failed: {detail}")

        assert submit_res.status_code == 200
        data = submit_res.json()
        assert "task_id" in data
        assert "escrow_tx" in data
        assert data["escrow_tx"]
        assert "deadline_unix" in data

    def test_submit_payment_bad_xdr_fails(self, api_client):
        discover = api_client.post("/api/discover", json={
            "task": "test", "capabilities": ["PDF"], "budget_xlm": 1000.0,
        })
        if discover.status_code != 200 or not discover.json().get("agents"):
            pytest.skip("No agents")

        res = api_client.post("/api/submit-payment", json={
            "signed_xdr": "NOT_VALID_XDR",
            "agent_handle": discover.json()["agents"][0]["handle"],
            "task": "test",
            "budget_xlm": 1.0,
        })
        assert res.status_code in (400, 502)
