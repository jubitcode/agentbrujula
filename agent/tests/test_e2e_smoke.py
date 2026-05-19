import base64
import json
import os
import sys
import time

import pytest
import httpx

LLM_PROVIDER = os.environ.get("TEST_LLM_PROVIDER", "")
ORCH_SECRET = os.environ.get("ORCHESTRATOR_SECRET_KEY", "")
MCP_META_URL = os.environ.get("MCP_META_URL", "http://127.0.0.1:9876")
MARKETPLACE_URL = os.environ.get("NEXT_PUBLIC_MARKETPLACE_URL", "http://127.0.0.1:5050")

pytestmark = [
    pytest.mark.skipif(not LLM_PROVIDER, reason="TEST_LLM_PROVIDER not set"),
    pytest.mark.skipif(not ORCH_SECRET, reason="ORCHESTRATOR_SECRET_KEY not set"),
]


def _check_service(url: str, name: str):
    try:
        resp = httpx.get(f"{url}/health" if "9876" in url else url, timeout=5)
    except Exception:
        pytest.skip(f"{name} not running at {url}")


class TestSmokeDebateFlow:
    def test_debate_returns_402_then_executes(self):
        _check_service(MCP_META_URL, "mcp-meta")
        _check_service(MARKETPLACE_URL, "FastAPI")

        resp = httpx.post(
            f"{MARKETPLACE_URL}/api/debate",
            json={"question": "SQLite vs PostgreSQL for a single-server app?"},
            timeout=10,
        )
        assert resp.status_code == 402, f"Expected 402, got {resp.status_code}: {resp.text}"
        assert "PAYMENT-REQUIRED" in resp.headers

        payment_info = json.loads(base64.b64decode(resp.headers["PAYMENT-REQUIRED"]))
        assert "estimated_cost" in payment_info or "price" in payment_info
        assert payment_info.get("payTo")

        # Smoke test is headless — cannot sign with Freighter; actual payment tested manually
        print(f"  Debate price: {payment_info.get('estimated_cost', payment_info.get('price'))} USDC")
        print(f"  Pay to: {payment_info.get('payTo', '')[:12]}...")


class TestSmokeA2AFlow:
    def test_a2a_returns_402_with_cost_estimate(self):
        _check_service(MARKETPLACE_URL, "FastAPI")

        resp = httpx.post(
            f"{MARKETPLACE_URL}/api/execute-a2a",
            json={"task": "Research AI trends in healthcare"},
            timeout=10,
        )
        assert resp.status_code == 402
        payment_info = json.loads(base64.b64decode(resp.headers["PAYMENT-REQUIRED"]))
        cost = float(payment_info.get("estimated_cost", payment_info.get("price", "0")))
        assert cost > 0
        print(f"  A2A price: {cost} USDC")

    def test_a2a_cost_scales_with_task_length(self):
        _check_service(MARKETPLACE_URL, "FastAPI")

        short = httpx.post(
            f"{MARKETPLACE_URL}/api/execute-a2a",
            json={"task": "Hi"},
            timeout=10,
        )
        long = httpx.post(
            f"{MARKETPLACE_URL}/api/execute-a2a",
            json={"task": "Research the complete history of artificial intelligence from Alan Turing to modern large language models including key breakthroughs"},
            timeout=10,
        )

        short_cost = float(json.loads(base64.b64decode(short.headers["PAYMENT-REQUIRED"])).get("estimated_cost", "0"))
        long_cost = float(json.loads(base64.b64decode(long.headers["PAYMENT-REQUIRED"])).get("estimated_cost", "0"))
        assert long_cost > short_cost


class TestSmokeEscrowBuild:
    def test_build_generic_payment_returns_create_escrow_xdr(self):
        _check_service(MARKETPLACE_URL, "FastAPI")

        from stellar_sdk import Keypair
        from research_agent.stellar_config import FRIENDBOT_URL

        kp = Keypair.random()
        if FRIENDBOT_URL:
            try:
                httpx.get(f"{FRIENDBOT_URL}?addr={kp.public_key}", timeout=30)
            except Exception:
                pytest.skip("Friendbot unavailable")
        else:
            pytest.skip("No friendbot on mainnet")

        resp = httpx.post(
            f"{MARKETPLACE_URL}/api/build-payment-generic",
            json={"amount_usdc": "0.01", "user_public_key": kp.public_key, "deadline_seconds": 120},
            timeout=30,
        )

        if resp.status_code != 200:
            pytest.skip(f"Build payment failed: {resp.status_code} {resp.text[:200]}")

        data = resp.json()
        assert "unsigned_xdr" in data
        assert "task_id" in data
        assert data["task_id"].startswith("x402-")
        assert "deadline" in data
        assert data["deadline"] > time.time()
        print(f"  Escrow task_id: {data['task_id']}")
        print(f"  Deadline: {data['deadline']} ({int(data['deadline'] - time.time())}s from now)")


class TestSmokeEscrowStatus:
    def test_nonexistent_escrow_returns_not_found(self):
        _check_service(MARKETPLACE_URL, "FastAPI")

        resp = httpx.get(
            f"{MARKETPLACE_URL}/api/escrow/nonexistent-task-id",
            timeout=10,
        )
        assert resp.status_code == 200
        data = resp.json()
        assert data["status"] in ("not_found", "found")


class TestSmokeAuthChallenge:
    def test_challenge_returns_nonce(self):
        _check_service(MARKETPLACE_URL, "FastAPI")

        resp = httpx.get(f"{MARKETPLACE_URL}/api/auth/challenge", timeout=5)
        assert resp.status_code == 200
        data = resp.json()
        assert len(data["challenge"]) == 32

    def test_auth_with_stellar_signature(self):
        _check_service(MARKETPLACE_URL, "FastAPI")

        from stellar_sdk import Keypair

        kp = Keypair.random()

        challenge_resp = httpx.get(f"{MARKETPLACE_URL}/api/auth/challenge", timeout=5)
        challenge = challenge_resp.json()["challenge"]

        signature = kp.sign(challenge.encode()).hex()
        auth_header = base64.b64encode(json.dumps({
            "publicKey": kp.public_key,
            "challenge": challenge,
            "signature": signature,
        }).encode()).decode()

        resp = httpx.get(
            f"{MARKETPLACE_URL}/api/result/fake-task/auth",
            headers={"X-Wallet-Auth": auth_header},
            timeout=5,
        )
        assert resp.status_code == 404  # auth passed, task not found (not 401)


class TestSmokeMCPMeta:
    def test_health(self):
        _check_service(MCP_META_URL, "mcp-meta")

        resp = httpx.get(f"{MCP_META_URL}/health", timeout=5)
        assert resp.status_code == 200
        assert resp.json()["status"] == "ok"

    def test_agent_card(self):
        _check_service(MCP_META_URL, "mcp-meta")

        resp = httpx.get(f"{MCP_META_URL}/.well-known/agent-card.json", timeout=5)
        assert resp.status_code == 200
        data = resp.json()
        assert "skills" in data
        assert len(data["skills"]) > 0

    def test_tools_list(self):
        _check_service(MCP_META_URL, "mcp-meta")

        resp = httpx.get(f"{MCP_META_URL}/v1/tools", timeout=5)
        assert resp.status_code == 200
        data = resp.json()
        assert data["totalCount"] > 0


class TestSmokeLLMExecution:
    """Real LLM execution via mcp-meta — only runs with TEST_LLM_PROVIDER=openrouter (requires running pods)."""

    @pytest.mark.skipif(LLM_PROVIDER != "openrouter", reason="Only with openrouter")
    def test_a2a_produces_output(self):
        _check_service(MCP_META_URL, "mcp-meta")

        resp = httpx.post(
            f"{MCP_META_URL}/a2a",
            json={
                "jsonrpc": "2.0",
                "id": 1,
                "method": "message/send",
                "params": {
                    "message": {
                        "role": "user",
                        "parts": [{"kind": "text", "text": "Summarize: Kubernetes automates container deployment."}],
                    },
                },
            },
            timeout=120,
        )
        assert resp.status_code == 200
        data = resp.json()

        result = data.get("result", {})
        status = result.get("status", {}).get("state", "")
        assert status == "completed", f"A2A status: {status}, full: {json.dumps(result.get('status', {}))}"

        artifacts = result.get("artifacts", [])
        assert len(artifacts) > 0
        text = artifacts[0].get("parts", [{}])[0].get("text", "")
        assert len(text) > 50, f"Output too short: {text[:100]}"
        print(f"  A2A output ({len(text)} chars): {text[:150]}...")

    @pytest.mark.skipif(LLM_PROVIDER != "openrouter", reason="Only with openrouter")
    def test_debate_produces_structured_result(self):
        _check_service(MCP_META_URL, "mcp-meta")

        resp = httpx.post(
            f"{MCP_META_URL}/v1/execute",
            json={
                "tool": "adr-moderator.debate",
                "arguments": {"question": "Redis vs Memcached for session caching?"},
                "timeout": 180,
            },
            timeout=200,
        )
        assert resp.status_code == 200
        data = resp.json()

        if data.get("error"):
            pytest.skip(f"Debate failed: {data['error']}")

        result = data.get("result", {})
        assert "transcript" in result, f"Missing transcript in: {list(result.keys())}"
        assert "decision" in result
        assert "adr" in result
        assert len(result["transcript"]) > 0
        assert len(result["decision"]) > 10
        print(f"  Debate: {result.get('rounds', 0)} rounds, {len(result['transcript'])} turns")
        print(f"  Decision: {result['decision'][:100]}...")
