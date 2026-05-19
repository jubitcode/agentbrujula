import base64
import json
import time

import pytest
from fastapi import FastAPI, Depends
from fastapi.testclient import TestClient

from research_agent.x402_middleware import x402_gate
from research_agent.x402_config import X402_NETWORK


app = FastAPI()


@app.get("/free")
def free_endpoint():
    return {"status": "ok"}


@app.get("/paid", dependencies=[Depends(x402_gate(price_usdc="0.01", pay_to="GXXXXFAKEADDRESS"))])
def paid_endpoint():
    return {"data": "premium content"}


client = TestClient(app)


class TestFreeEndpoints:
    def test_free_endpoint_passes_through(self):
        res = client.get("/free")
        assert res.status_code == 200
        assert res.json() == {"status": "ok"}


class TestPaymentRequired:
    def test_paid_endpoint_without_payment_returns_402(self):
        res = client.get("/paid")
        assert res.status_code == 402

    def test_402_includes_payment_required_header(self):
        res = client.get("/paid")
        assert res.status_code == 402
        assert "PAYMENT-REQUIRED" in res.headers

        payload = json.loads(base64.b64decode(res.headers["PAYMENT-REQUIRED"]))
        assert payload["price"] == "0.01"
        assert payload["network"] == X402_NETWORK
        assert payload["payTo"] == "GXXXXFAKEADDRESS"
        assert "token" in payload
        assert "facilitatorUrl" in payload

    def test_paid_endpoint_with_valid_signature_returns_200(self, monkeypatch):
        import research_agent.x402_middleware as mw

        async def mock_verify(payment_sig, payment_req):
            return {"success": True, "txHash": "abc123"}

        monkeypatch.setattr(mw, "_call_facilitator_verify", mock_verify)

        fake_sig = base64.b64encode(json.dumps({"sig": "valid"}).encode()).decode()
        res = client.get("/paid", headers={"PAYMENT-SIGNATURE": fake_sig})
        assert res.status_code == 200
        assert res.json() == {"data": "premium content"}
        assert "PAYMENT-RESPONSE" in res.headers


class TestBadPayments:
    def test_invalid_signature_returns_402(self, monkeypatch):
        import research_agent.x402_middleware as mw

        async def mock_verify_fail(payment_sig, payment_req):
            return {"success": False, "error": "invalid signature"}

        monkeypatch.setattr(mw, "_call_facilitator_verify", mock_verify_fail)

        fake_sig = base64.b64encode(json.dumps({"sig": "bad"}).encode()).decode()
        res = client.get("/paid", headers={"PAYMENT-SIGNATURE": fake_sig})
        assert res.status_code == 402

    def test_malformed_signature_header_returns_400(self):
        res = client.get("/paid", headers={"PAYMENT-SIGNATURE": "not-base64-!!!"})
        assert res.status_code == 400

    def test_replay_attack_blocked(self, monkeypatch):
        import research_agent.x402_middleware as mw

        call_count = 0

        async def mock_verify_with_nonce(payment_sig, payment_req):
            nonlocal call_count
            call_count += 1
            return {"success": True, "txHash": f"tx-{call_count}", "nonce": "reused-nonce-123"}

        monkeypatch.setattr(mw, "_call_facilitator_verify", mock_verify_with_nonce)

        fake_sig = base64.b64encode(json.dumps({"sig": "ok", "nonce": "reused-nonce-123"}).encode()).decode()

        res1 = client.get("/paid", headers={"PAYMENT-SIGNATURE": fake_sig})
        assert res1.status_code == 200

        res2 = client.get("/paid", headers={"PAYMENT-SIGNATURE": fake_sig})
        assert res2.status_code == 403

    def test_facilitator_timeout_returns_502(self, monkeypatch):
        """Facilitator unreachable → 502 (FAIL CLOSED: never give resource without verified payment)."""
        import research_agent.x402_middleware as mw

        async def mock_verify_timeout(payment_sig, payment_req):
            raise TimeoutError("facilitator unreachable")

        monkeypatch.setattr(mw, "_call_facilitator_verify", mock_verify_timeout)

        fake_sig = base64.b64encode(json.dumps({"sig": "ok"}).encode()).decode()
        res = client.get("/paid", headers={"PAYMENT-SIGNATURE": fake_sig})
        assert res.status_code == 502

    def test_facilitator_error_returns_502(self, monkeypatch):
        """Facilitator errors → 502 (FAIL CLOSED)."""
        import research_agent.x402_middleware as mw

        async def mock_verify_error(payment_sig, payment_req):
            raise Exception("facilitator internal error")

        monkeypatch.setattr(mw, "_call_facilitator_verify", mock_verify_error)

        fake_sig = base64.b64encode(json.dumps({"sig": "ok"}).encode()).decode()
        res = client.get("/paid", headers={"PAYMENT-SIGNATURE": fake_sig})
        assert res.status_code == 502


class TestFeatureFlag:
    def test_disabled_x402_passes_everything(self, monkeypatch):
        """X402_ENABLED=false makes the middleware transparent."""
        import research_agent.x402_config as cfg
        monkeypatch.setattr(cfg, "X402_ENABLED", False)

        from research_agent.x402_middleware import x402_gate as gate_fn
        disabled_app = FastAPI()

        @disabled_app.get("/paid", dependencies=[Depends(gate_fn(price_usdc="0.01", pay_to="GXXX"))])
        def paid():
            return {"data": "free when disabled"}

        disabled_client = TestClient(disabled_app)
        res = disabled_client.get("/paid")
        assert res.status_code == 200
