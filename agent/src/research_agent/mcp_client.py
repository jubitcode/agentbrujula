"""HTTP client for MCP-Meta — tool execution and A2A message/send."""

from __future__ import annotations

import os

import httpx

DEFAULT_MCP_URL = "http://127.0.0.1:9876"


def _get_mcp_url() -> str:
    return os.environ.get("MCP_META_URL", DEFAULT_MCP_URL)


def _get_mcp_headers() -> dict:
    """Auth headers for mcp-meta API calls."""
    headers = {"Content-Type": "application/json"}
    api_key = os.environ.get("MCP_META_API_KEY") or os.environ.get("AUTH_FREE_KEY", "")
    if api_key:
        headers["X-API-Key"] = api_key
    return headers


def execute_tool(
    tool_name: str,
    arguments: dict,
    mcp_url: str | None = None,
    timeout: float = 60.0,
) -> dict:
    """Invoke a tool via MCP-Meta's HTTP API.

    Returns the result dict from the tool, or raises on error.
    """
    mcp_url = mcp_url or _get_mcp_url()

    payload = {
        "tool": tool_name,
        "arguments": arguments,
        "timeout": int(timeout),
    }

    resp = httpx.post(
        f"{mcp_url}/v1/execute",
        json=payload,
        headers=_get_mcp_headers(),
        timeout=timeout + 5,
    )
    resp.raise_for_status()

    data = resp.json()

    if data.get("error"):
        raise RuntimeError(f"MCP-Meta execution error: {data['error']}")

    return data.get("result", {})


def send_a2a_message(
    text: str,
    context_id: str | None = None,
    skill_id: str | None = None,
    mcp_url: str | None = None,
    timeout: float = 120.0,
) -> dict:
    """Send a message via A2A protocol. Returns the completed task.

    Args:
        text: The task/message text
        context_id: Groups related messages for cost tracking
        skill_id: Agent/skill to route to (e.g. "adr-debate"). If not set,
                  mcp-meta resolves from text content.
    """
    mcp_url = mcp_url or _get_mcp_url()

    payload = {
        "jsonrpc": "2.0",
        "id": 1,
        "method": "message/send",
        "params": {
            "message": {
                "role": "user",
                "parts": [{"kind": "text", "text": text}],
            },
        },
    }
    if context_id:
        payload["params"]["contextId"] = context_id
    if skill_id:
        payload["params"]["metadata"] = {"skillId": skill_id}

    resp = httpx.post(
        f"{mcp_url}/a2a",
        json=payload,
        headers=_get_mcp_headers(),
        timeout=timeout,
    )
    resp.raise_for_status()

    data = resp.json()

    if data.get("error"):
        err = data["error"]
        msg = err.get("message", str(err)) if isinstance(err, dict) else str(err)
        raise RuntimeError(f"A2A error: {msg}")

    result = data.get("result", {})

    output_parts = []
    for artifact in result.get("artifacts", []):
        for part in artifact.get("parts", []):
            if part.get("text"):
                output_parts.append(part["text"])

    return {
        "task_id": result.get("id", ""),
        "context_id": result.get("contextId", context_id),
        "status": result.get("status", {}).get("state", "unknown"),
        "output": "\n\n".join(output_parts) if output_parts else None,
        "raw": result,
    }


def stream_a2a_message(
    text: str,
    context_id: str | None = None,
    skill_id: str | None = None,
    mcp_url: str | None = None,
    timeout: float = 180.0,
):
    """Stream A2A via mcp-meta /a2a/stream SSE endpoint.

    Yields dicts: {"event": "progress"|"step"|"result"|"error", "data": {...}}
    """
    import json as _json

    mcp_url = mcp_url or _get_mcp_url()
    payload = {
        "message": {
            "role": "user",
            "parts": [{"kind": "text", "text": text}],
        },
    }
    if context_id:
        payload["contextId"] = context_id
    if skill_id:
        payload["metadata"] = {"skillId": skill_id}

    with httpx.stream(
        "POST",
        f"{mcp_url}/a2a/stream",
        json=payload,
        headers=_get_mcp_headers(),
        timeout=timeout,
    ) as resp:
        resp.raise_for_status()
        event_type = "message"
        for line in resp.iter_lines():
            if line.startswith("event: "):
                event_type = line[7:].strip()
            elif line.startswith("data: "):
                try:
                    data = _json.loads(line[6:])
                except (ValueError, _json.JSONDecodeError):
                    data = {"raw": line[6:]}
                yield {"event": event_type, "data": data}
