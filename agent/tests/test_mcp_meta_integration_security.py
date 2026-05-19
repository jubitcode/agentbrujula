import sys
import time
from pathlib import Path

import httpx
import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "src"))

MCP_URL = "http://127.0.0.1:9876"
TENANT_A_KEY = "mcp-test-tenant-a"
TENANT_B_KEY = "mcp-test-tenant-b"
TIMEOUT = 30.0


def _mcp_reachable() -> bool:
    try:
        r = httpx.get(f"{MCP_URL}/health", timeout=2.0)
        return r.status_code == 200
    except Exception:
        return False


requires_mcp = pytest.mark.skipif(
    not _mcp_reachable(),
    reason="mcp-meta not running at localhost:9876",
)


def _a2a_send(key: str, text: str, context_id: str | None = None) -> dict:
    payload = {
        "jsonrpc": "2.0", "id": 1,
        "method": "message/send",
        "params": {
            "message": {"role": "user", "parts": [{"kind": "text", "text": text}]},
        },
    }
    if context_id:
        payload["params"]["contextId"] = context_id

    resp = httpx.post(f"{MCP_URL}/a2a",
        json=payload,
        headers={"X-API-Key": key, "Content-Type": "application/json"},
        timeout=TIMEOUT,
    )
    return resp.json()


def _a2a_get_task(key: str, task_id: str) -> httpx.Response:
    payload = {
        "jsonrpc": "2.0", "id": 1,
        "method": "tasks/get",
        "params": {"id": task_id},
    }
    return httpx.post(f"{MCP_URL}/a2a",
        json=payload,
        headers={"X-API-Key": key, "Content-Type": "application/json"},
        timeout=10.0,
    )


def _a2a_cancel_task(key: str, task_id: str) -> httpx.Response:
    payload = {
        "jsonrpc": "2.0", "id": 1,
        "method": "tasks/cancel",
        "params": {"id": task_id},
    }
    return httpx.post(f"{MCP_URL}/a2a",
        json=payload,
        headers={"X-API-Key": key, "Content-Type": "application/json"},
        timeout=10.0,
    )


@requires_mcp
class TestV1_DirectAccessBlocked:
    """Limited to 2 auth failures to avoid the IP-based brute-force blocker (5 failures / 15 min)."""

    def test_a2a_no_auth_401(self):
        resp = httpx.post(f"{MCP_URL}/a2a",
            json={"jsonrpc": "2.0", "id": 1, "method": "message/send",
                  "params": {"message": {"role": "user", "parts": [{"kind": "text", "text": "test"}]}}},
            timeout=5.0,
        )
        assert resp.status_code == 401

    def test_tools_no_auth_401(self):
        resp = httpx.get(f"{MCP_URL}/v1/tools", timeout=5.0)
        assert resp.status_code == 401

    def test_health_public_200(self):
        resp = httpx.get(f"{MCP_URL}/health", timeout=5.0)
        assert resp.status_code == 200


@requires_mcp
class TestV6_CostTenantIsolation:
    def test_tenant_a_creates_task_tenant_b_cannot_read_cost(self):
        result = _a2a_send(TENANT_A_KEY, "hello for cost test")
        context_id = result.get("result", {}).get("contextId", "")
        assert context_id, f"No contextId in result: {result}"

        resp_a = httpx.get(f"{MCP_URL}/v1/cost/{context_id}",
            headers={"X-API-Key": TENANT_A_KEY}, timeout=5.0)
        assert resp_a.status_code == 200, f"Tenant-A should read own cost, got {resp_a.status_code}"

        resp_b = httpx.get(f"{MCP_URL}/v1/cost/{context_id}",
            headers={"X-API-Key": TENANT_B_KEY}, timeout=5.0)
        assert resp_b.status_code == 403, (
            f"Tenant-B should NOT read tenant-A's cost, got {resp_b.status_code}: {resp_b.text}"
        )

    def test_unknown_context_returns_empty_not_error(self):
        resp = httpx.get(f"{MCP_URL}/v1/cost/nonexistent-context-12345",
            headers={"X-API-Key": TENANT_A_KEY}, timeout=5.0)
        # No tenant mapping = treated as public, returns empty data
        assert resp.status_code == 200
        data = resp.json()
        assert data["total_stroops"] == 0


@requires_mcp
class TestV4_A2ATaskIsolation:
    """A2A task operations must verify tenant ownership."""

    def test_tenant_b_cannot_read_tenant_a_task(self):
        result = _a2a_send(TENANT_A_KEY, "test task for isolation")
        task_id = result.get("result", {}).get("id", "")
        assert task_id, f"No task ID: {result}"

        resp_a = _a2a_get_task(TENANT_A_KEY, task_id)
        data_a = resp_a.json()
        assert "error" not in data_a or data_a.get("result") is not None, (
            f"Tenant-A should read own task: {data_a}"
        )

        resp_b = _a2a_get_task(TENANT_B_KEY, task_id)
        data_b = resp_b.json()
        has_error = "error" in data_b
        assert has_error, (
            f"Tenant-B should NOT read tenant-A's task, got: {data_b}"
        )

    def test_tenant_b_cannot_cancel_tenant_a_task(self):
        result = _a2a_send(TENANT_A_KEY, "task to cancel test")
        task_id = result.get("result", {}).get("id", "")
        assert task_id, f"No task ID: {result}"

        resp_b = _a2a_cancel_task(TENANT_B_KEY, task_id)
        data_b = resp_b.json()
        has_error = "error" in data_b
        assert has_error, (
            f"Tenant-B should NOT cancel tenant-A's task, got: {data_b}"
        )


@requires_mcp
class TestV7_RateLimiting:
    def test_rate_limit_triggers_on_burst(self):
        """Uses TENANT_B to avoid exhausting TENANT_A's quota for other tests."""
        hit_limit = False
        for i in range(60):
            resp = httpx.get(f"{MCP_URL}/v1/tools",
                headers={"X-API-Key": TENANT_B_KEY}, timeout=5.0)
            if resp.status_code == 429:
                hit_limit = True
                break
        # Free tier burst is 5, must trip within 60 requests
        assert hit_limit, "Rate limit never triggered after 60 rapid requests"
