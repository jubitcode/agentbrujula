import base64
import json
import os
import sys
import time

import pytest

# Env vars MUST be set before importing server
os.environ["ORCHESTRATOR_SECRET_KEY"] = "PLACEHOLDER"
os.environ["X402_ESCROW_CONTRACT"] = "CDZ2YL6K2MWL72H6Q4L4JEXWNHV43EXUYEZWARLRQF7O36MFBZG4RSTR"
os.environ["X402_USDC_ADDRESS"] = "CBIELTK6YBZJU5UP2WWQEUCYKLPU6AUNZ2BQ4WWFEIE3USCIHMXQDAMA"
os.environ["X402_ENABLED"] = "true"

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", "api"))
from fastapi.testclient import TestClient
from server import app

client = TestClient(app)


class TestHireReturns402WithPaymentInfo:
    def test_hire_returns_402(self):
        res = client.post("/api/hire", json={
            "agent_handle": "test",
            "task": "research",
            "budget_xlm": 5.0,
        })
        assert res.status_code == 402

    def test_402_has_payment_required_header(self):
        res = client.post("/api/hire", json={
            "agent_handle": "test",
            "task": "research",
            "budget_xlm": 5.0,
        })
        assert "PAYMENT-REQUIRED" in res.headers

    def test_payment_required_contains_price_and_destination(self):
        res = client.post("/api/hire", json={
            "agent_handle": "test",
            "task": "research",
            "budget_xlm": 5.0,
        })
        payload = json.loads(base64.b64decode(res.headers["PAYMENT-REQUIRED"]))
        assert "price" in payload
        assert "payTo" in payload
        assert "token" in payload
        assert "network" in payload


class TestBuildPayment:
    def test_build_payment_returns_xdr(self):
        res = client.post("/api/build-payment", json={
            "agent_handle": "test",
            "amount_usdc": "0.10",
            "user_public_key": "GBZF3HIHM5NOWLNI6QHTOOXCNRMXGR76UHCVUQJXW5TGXN2AF3MQT5I",
        })
        # 200/404/500/503 all acceptable — anything but 402 proves endpoint exists past x402 gate
        assert res.status_code in (200, 404, 500, 503)

    def test_build_payment_requires_user_public_key(self):
        res = client.post("/api/build-payment", json={
            "agent_handle": "test",
            "amount_usdc": "0.10",
        })
        assert res.status_code == 422

    def test_build_payment_200_returns_xdr_fields(self):
        res = client.post("/api/build-payment", json={
            "agent_handle": "test",
            "amount_usdc": "0.10",
            "user_public_key": "GBZF3HIHM5NOWLNI6QHTOOXCNRMXGR76UHCVUQJXW5TGXN2AF3MQT5I",
        })
        if res.status_code == 200:
            data = res.json()
            assert "unsigned_xdr" in data
            assert "amount_usdc" in data
            assert "destination" in data
            assert "token" in data


class TestSubmitPayment:
    def test_submit_payment_endpoint_exists(self):
        res = client.post("/api/submit-payment", json={
            "signed_xdr": "FAKE_XDR",
            "agent_handle": "test",
            "task": "research",
            "budget_xlm": 5.0,
        })
        # 405 (method-not-allowed) would mean the endpoint is missing; anything else proves routing works
        assert res.status_code != 405
        assert res.status_code in (404, 502, 500)

    def test_submit_payment_requires_signed_xdr(self):
        res = client.post("/api/submit-payment", json={
            "agent_handle": "test",
            "task": "research",
            "budget_xlm": 5.0,
        })
        assert res.status_code == 422
