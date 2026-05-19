import base64
import json
import time

import pytest

from conftest import requires_orchestrator


pytestmark = [requires_orchestrator]


class TestDiscoverIsFree:
    def test_discover_returns_non_402(self, api_client):
        res = api_client.post("/api/discover", json={
            "task": "research AI trends",
            "capabilities": ["PDF"],
            "budget_xlm": 100.0,
        })
        assert res.status_code != 402, "discover should not require payment"

    def test_discover_returns_agents_or_empty(self, api_client):
        res = api_client.post("/api/discover", json={
            "task": "test",
            "capabilities": ["PDF"],
            "budget_xlm": 100.0,
        })
        if res.status_code == 200:
            data = res.json()
            assert "agents" in data
            assert isinstance(data["agents"], list)


class TestHireRequiresPayment:
    def test_hire_without_payment_returns_402(self, api_client):
        res = api_client.post("/api/hire", json={
            "agent_handle": "test-agent",
            "task": "research",
            "budget_xlm": 5.0,
        })
        assert res.status_code == 402

    def test_402_has_payment_required_header(self, api_client):
        res = api_client.post("/api/hire", json={
            "agent_handle": "test-agent",
            "task": "research",
            "budget_xlm": 5.0,
        })
        assert "PAYMENT-REQUIRED" in res.headers

    def test_402_payload_has_real_contract_addresses(self, api_client):
        res = api_client.post("/api/hire", json={
            "agent_handle": "test-agent",
            "task": "research",
            "budget_xlm": 5.0,
        })
        payload = json.loads(base64.b64decode(res.headers["PAYMENT-REQUIRED"]))

        assert "network" in payload
        assert "stellar" in payload["network"]
        assert "payTo" in payload
        assert len(payload["payTo"]) == 56  # G-strkey
        assert "token" in payload
        assert len(payload["token"]) == 56
        assert "facilitatorUrl" in payload
        assert payload["facilitatorUrl"].startswith("http")

    def test_402_payload_has_budget_fields_when_agent_exists(self, api_client):
        discover_res = api_client.post("/api/discover", json={
            "task": "test",
            "capabilities": ["PDF", "WEB"],
            "budget_xlm": 1000.0,
        })
        if discover_res.status_code != 200:
            pytest.skip("No agents discoverable on testnet")

        agents = discover_res.json().get("agents", [])
        if not agents:
            pytest.skip("No agents registered on testnet")

        real_handle = agents[0]["handle"]

        res = api_client.post("/api/hire", json={
            "agent_handle": real_handle,
            "task": "research",
            "budget_xlm": 5.0,
        })
        assert res.status_code == 402
        payload = json.loads(base64.b64decode(res.headers["PAYMENT-REQUIRED"]))

        if "estimated_cost" in payload:
            assert "budget_ceiling" in payload
            assert float(payload["budget_ceiling"]) >= float(payload["estimated_cost"])
            if "rate_breakdown" in payload:
                assert isinstance(payload["rate_breakdown"], list)


class TestDevModeAck:
    """With X402_DEV_MODE=true, an ack signature bypasses the facilitator."""

    def test_dev_mode_ack_passes_gate(self, api_client):
        ack_sig = base64.b64encode(json.dumps({"ack": True}).encode()).decode()

        res = api_client.post(
            "/api/hire",
            json={
                "agent_handle": "nonexistent-agent",
                "task": "research",
                "budget_xlm": 5.0,
            },
            headers={"PAYMENT-SIGNATURE": ack_sig},
        )
        # Past 402; 404 because agent doesn't exist
        assert res.status_code != 402
        assert res.status_code in (200, 404)


class TestRealFacilitator:
    def test_facilitator_reachable(self):
        import httpx
        from research_agent.x402_config import X402_FACILITATOR_URL

        try:
            r = httpx.get(X402_FACILITATOR_URL, timeout=5.0, follow_redirects=True)
            assert r.status_code < 500, f"Facilitator returned server error: {r.status_code}"
        except httpx.ConnectError:
            pytest.skip(f"Facilitator not reachable at {X402_FACILITATOR_URL}")
        except httpx.TimeoutException:
            pytest.skip(f"Facilitator timed out at {X402_FACILITATOR_URL}")

    def test_facilitator_verify_with_stellar_payment(self, api_client):
        """Diagnostic — documents whether x402.org supports Stellar; passes whether the facilitator returns success or error."""
        import httpx
        from research_agent.x402_config import X402_FACILITATOR_URL, X402_NETWORK

        payment_required = base64.b64encode(json.dumps({
            "price": "0.01",
            "network": X402_NETWORK,
            "payTo": "CDZ2YL6K2MWL72H6Q4L4JEXWNHV43EXUYEZWARLRQF7O36MFBZG4RSTR",
            "token": "CBIELTK6YBZJU5UP2WWQEUCYKLPU6AUNZ2BQ4WWFEIE3USCIHMXQDAMA",
        }).encode()).decode()

        fake_sig = base64.b64encode(json.dumps({"test": True}).encode()).decode()

        try:
            r = httpx.post(
                f"{X402_FACILITATOR_URL}/verify",
                json={
                    "paymentSignature": fake_sig,
                    "paymentRequired": payment_required,
                },
                timeout=10.0,
            )
            print(f"\n  x402.org /verify response: {r.status_code} {r.text[:200]}")
        except (httpx.ConnectError, httpx.TimeoutException) as e:
            pytest.skip(f"Facilitator not reachable: {e}")


class TestNonceReplay:
    def test_same_nonce_blocked_on_second_request(self, api_client, monkeypatch):
        import research_agent.x402_middleware as mw

        call_count = 0
        fixed_nonce = f"test-nonce-{time.time()}"

        original_verify = mw._call_facilitator_verify

        async def verify_with_fixed_nonce(payment_sig, payment_req):
            nonlocal call_count
            call_count += 1
            return {"success": True, "txHash": f"tx-{call_count}", "nonce": fixed_nonce}

        monkeypatch.setattr(mw, "_call_facilitator_verify", verify_with_fixed_nonce)

        sig = base64.b64encode(json.dumps({"sig": "valid"}).encode()).decode()
        body = {"agent_handle": "x", "task": "y", "budget_xlm": 1.0}

        res1 = api_client.post("/api/hire", json=body, headers={"PAYMENT-SIGNATURE": sig})
        # Past gate; 404 because agent doesn't exist
        assert res1.status_code in (200, 404)

        res2 = api_client.post("/api/hire", json=body, headers={"PAYMENT-SIGNATURE": sig})
        assert res2.status_code == 403  # replay blocked
