import os
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "src"))


def _mcp_reachable() -> bool:
    try:
        import httpx
        r = httpx.get("http://127.0.0.1:9876/health", timeout=2.0)
        return r.status_code == 200
    except Exception:
        return False


requires_mcp = pytest.mark.skipif(
    not _mcp_reachable(),
    reason="mcp-meta not running at localhost:9876",
)

MCP_URL = "http://127.0.0.1:9876"
VALID_KEY = os.environ.get("AUTH_FREE_KEY", "mcp-dev-key")


@requires_mcp
class TestDirectAccessWithoutAuth:
    """All protected endpoints must reject unauthenticated requests."""

    def test_a2a_rejects_no_auth(self):
        import httpx
        resp = httpx.post(f"{MCP_URL}/a2a", json={
            "jsonrpc": "2.0", "id": 1,
            "method": "message/send",
            "params": {"message": {"role": "user", "parts": [{"kind": "text", "text": "test"}]}},
        }, timeout=5.0)
        assert resp.status_code == 401, f"A2A without auth should be 401, got {resp.status_code}"

    def test_execute_rejects_no_auth(self):
        import httpx
        resp = httpx.post(f"{MCP_URL}/v1/execute", json={
            "tool": "test", "arguments": {},
        }, timeout=5.0)
        assert resp.status_code == 401

    def test_tools_rejects_no_auth(self):
        import httpx
        resp = httpx.get(f"{MCP_URL}/v1/tools", timeout=5.0)
        assert resp.status_code == 401

    def test_cost_rejects_no_auth(self):
        import httpx
        resp = httpx.get(f"{MCP_URL}/v1/cost/test-context", timeout=5.0)
        assert resp.status_code == 401

    def test_health_is_public(self):
        import httpx
        resp = httpx.get(f"{MCP_URL}/health", timeout=5.0)
        assert resp.status_code == 200

    def test_ready_is_public(self):
        import httpx
        resp = httpx.get(f"{MCP_URL}/ready", timeout=5.0)
        assert resp.status_code == 200


@requires_mcp
class TestInvalidAuthRejected:
    """Invalid and forged API keys must be rejected."""

    def test_wrong_api_key_rejected(self):
        import httpx
        resp = httpx.get(f"{MCP_URL}/v1/tools",
            headers={"X-API-Key": "wrong-key-12345"},
            timeout=5.0,
        )
        assert resp.status_code == 401

    def test_empty_api_key_rejected(self):
        import httpx
        resp = httpx.get(f"{MCP_URL}/v1/tools",
            headers={"X-API-Key": ""},
            timeout=5.0,
        )
        assert resp.status_code == 401


class TestEnvVarLeakage:
    """Sensitive keys must NOT be forwarded to pods via deploy.go's forwardEnvVars list."""

    def test_auth_keys_not_in_forward_list(self):
        deploy_go = Path(__file__).resolve().parent.parent.parent / "mcp-meta" / "internal" / "deploy" / "deploy.go"
        content = deploy_go.read_text()

        assert "forwardEnvVars" in content, "Can't find forwardEnvVars in deploy.go"

        sensitive_keys = ["AUTH_FREE_KEY", "AUTH_PAID_KEY", "ORCHESTRATOR_SECRET_KEY", "MCP_META_API_KEY"]
        for key in sensitive_keys:
            assert key not in content.split("forwardEnvVars")[1].split("}")[0], (
                f"SECURITY: {key} is forwarded to pods via deploy.go — pods can read it!"
            )


@requires_mcp
class TestFastAPIAuthGate:
    def test_discover_rejects_no_auth(self):
        import httpx
        resp = httpx.post("http://localhost:5050/api/discover",
            json={"task": "test", "capabilities": ["research"], "budget_xlm": 10},
            timeout=5.0,
        )
        assert resp.status_code == 401, f"Discover without auth should be 401, got {resp.status_code}"

    def test_fund_rejects_no_auth(self):
        import httpx
        resp = httpx.post("http://localhost:5050/api/fund",
            json={"public_key": "GTEST"},
            timeout=5.0,
        )
        assert resp.status_code == 401

    def test_registry_is_public(self):
        import httpx
        resp = httpx.get("http://localhost:5050/api/registry", timeout=5.0)
        assert resp.status_code == 200, "Registry should be public"

    def test_auth_challenge_is_public(self):
        import httpx
        resp = httpx.get("http://localhost:5050/api/auth/challenge", timeout=5.0)
        assert resp.status_code == 200, "Auth challenge should be public"
