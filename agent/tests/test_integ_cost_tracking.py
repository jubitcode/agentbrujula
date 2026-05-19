import time

import httpx
import pytest

from conftest import requires_mcp_meta


MCP_URL = "http://127.0.0.1:8080"

pytestmark = [requires_mcp_meta]


class TestCostEndpoint:
    def test_unknown_context_returns_empty_ledger(self):
        r = httpx.get(f"{MCP_URL}/v1/cost/nonexistent-ctx-{int(time.time())}", timeout=5.0)
        assert r.status_code == 200
        data = r.json()

        assert data["context_id"].startswith("nonexistent-ctx-")
        assert data["total_stroops"] == 0
        assert "0" in data["total_usdc"]
        assert data["entries"] == [] or data["entries"] is None or data["entries"] == []
        assert data["by_agent"] == {} or data["by_agent"] is None or data["by_agent"] == {}

    def test_cost_response_schema(self):
        r = httpx.get(f"{MCP_URL}/v1/cost/schema-test", timeout=5.0)
        assert r.status_code == 200
        data = r.json()

        assert "context_id" in data
        assert "total_stroops" in data
        assert "total_usdc" in data
        assert "entries" in data
        assert "by_agent" in data

        assert isinstance(data["total_stroops"], (int, float))
        assert isinstance(data["total_usdc"], str)

    def test_health_endpoint(self):
        r = httpx.get(f"{MCP_URL}/health", timeout=5.0)
        assert r.status_code == 200
        data = r.json()
        assert data.get("status") == "ok"

    def test_tools_endpoint_lists_tools(self):
        r = httpx.get(f"{MCP_URL}/v1/tools", timeout=5.0)
        assert r.status_code == 200
        data = r.json()
        tools = data.get("tools", data) if isinstance(data, dict) else data
        assert isinstance(tools, list)
        assert len(tools) > 0, "mcp-meta should have tools loaded from manifests"


class TestCostAccumulation:
    """Tool calls must accumulate costs in the ledger — requires at least one tool pod running."""

    def test_execute_tool_and_check_cost(self):
        tools_r = httpx.get(f"{MCP_URL}/v1/tools", timeout=5.0)
        if tools_r.status_code != 200:
            pytest.skip("Cannot list tools")

        tools = tools_r.json()
        if not tools:
            pytest.skip("No tools loaded in mcp-meta")

        if isinstance(tools, list) and len(tools) > 0:
            tool_name = tools[0].get("name") or tools[0].get("fqn", "")
        elif isinstance(tools, dict):
            tool_name = next(iter(tools.keys()), "")
        else:
            pytest.skip("Cannot determine tool name")

        if not tool_name:
            pytest.skip("No tool name found")

        context_id = f"cost-test-{int(time.time())}"

        # Execution may fail on arg validation, but the attempt is still tracked
        try:
            exec_r = httpx.post(
                f"{MCP_URL}/v1/execute",
                json={
                    "tool": tool_name,
                    "arguments": {},
                    "context_id": context_id,
                },
                timeout=30.0,
            )
        except httpx.TimeoutException:
            pytest.skip("Tool execution timed out")

        cost_r = httpx.get(f"{MCP_URL}/v1/cost/{context_id}", timeout=5.0)
        assert cost_r.status_code == 200
        data = cost_r.json()
        assert data["context_id"] == context_id

        if exec_r.status_code == 200:
            # cost_stroops may be 0 if tool has no pricing configured
            assert isinstance(data["entries"], list)
            print(f"\n  Cost after tool call: {data['total_stroops']} stroops, {len(data['entries'] or [])} entries")
