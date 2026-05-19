import base64
import json
import subprocess
import time

import pytest
from fastapi.testclient import TestClient


def _get_secret(alias: str) -> str:
    result = subprocess.run(
        ["stellar", "keys", "show", alias],
        capture_output=True, text=True,
    )
    return result.stdout.strip()


# Env vars MUST be set before importing server
import os
os.environ["ORCHESTRATOR_SECRET_KEY"] = _get_secret("orchestrator")
os.environ["X402_ESCROW_CONTRACT"] = "CDZ2YL6K2MWL72H6Q4L4JEXWNHV43EXUYEZWARLRQF7O36MFBZG4RSTR"
os.environ["X402_USDC_ADDRESS"] = "CBIELTK6YBZJU5UP2WWQEUCYKLPU6AUNZ2BQ4WWFEIE3USCIHMXQDAMA"
os.environ["X402_ENABLED"] = "true"

from research_agent.x402_middleware import x402_gate, _call_facilitator_verify


class TestDiscoverIsFree:
    def test_discover_returns_200_without_payment(self):
        from api_test_app import app
        client = TestClient(app)

        res = client.post("/api/discover", json={
            "task": "Research AI trends",
            "capabilities": ["PDF"],
            "budget_xlm": 100.0,
        })
        # May 200 or 500 (no registered agents) but NEVER 402
        assert res.status_code != 402


class TestHireRequiresPayment:
    def test_hire_without_payment_returns_402(self):
        from api_test_app import app
        client = TestClient(app)

        res = client.post("/api/hire", json={
            "agent_handle": "test-agent",
            "task": "Research AI",
            "budget_xlm": 5.0,
        })
        assert res.status_code == 402

    def test_hire_402_has_payment_required_header(self):
        from api_test_app import app
        client = TestClient(app)

        res = client.post("/api/hire", json={
            "agent_handle": "test-agent",
            "task": "Research AI",
            "budget_xlm": 5.0,
        })
        assert "PAYMENT-REQUIRED" in res.headers

        payload = json.loads(base64.b64decode(res.headers["PAYMENT-REQUIRED"]))
        assert payload["price"] == "0.10"
        assert "stellar" in payload["network"]
        assert payload["payTo"] == "CDZ2YL6K2MWL72H6Q4L4JEXWNHV43EXUYEZWARLRQF7O36MFBZG4RSTR"
        assert "facilitatorUrl" in payload

    def test_hire_with_valid_payment_proceeds(self, monkeypatch):
        from api_test_app import app
        import research_agent.x402_middleware as mw

        async def mock_verify(payment_sig, payment_req):
            return {"success": True, "txHash": "mock-tx-hash", "nonce": f"nonce-{time.time()}"}

        monkeypatch.setattr(mw, "_call_facilitator_verify", mock_verify)

        client = TestClient(app)
        fake_sig = base64.b64encode(json.dumps({"sig": "valid"}).encode()).decode()

        res = client.post(
            "/api/hire",
            json={
                "agent_handle": "nonexistent-agent",
                "task": "Research AI",
                "budget_xlm": 5.0,
            },
            headers={"PAYMENT-SIGNATURE": fake_sig},
        )
        # Past x402; 404 because the agent handle doesn't exist
        assert res.status_code != 402
        assert res.status_code == 404


class TestX402Disabled:
    def test_hire_without_x402_works(self, monkeypatch):
        import research_agent.x402_config as cfg
        monkeypatch.setattr(cfg, "X402_ENABLED", False)

        from api_test_app import app
        client = TestClient(app)

        res = client.post("/api/hire", json={
            "agent_handle": "nonexistent",
            "task": "test",
            "budget_xlm": 1.0,
        })
        assert res.status_code != 402
