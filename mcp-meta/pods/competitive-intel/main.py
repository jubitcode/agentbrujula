"""
Competitive Intel Agent Pod — gathers competitive intelligence.

Tools:
  - intel(target, aspects?) → structured competitive analysis
  - ping
"""

import sys
import json
import os
import threading
import time

from openai import OpenAI

API_KEY = os.environ.get("OPENROUTER_API_KEY", "")
LLM_BASE_URL = os.environ.get("LLM_BASE_URL", "https://openrouter.ai/api/v1")
MODEL = os.environ.get("LLM_MODEL", "google/gemini-2.0-flash-001")

# Inter-pod call infrastructure: ID-matched request/response over stdin/stdout.
# MCP-Meta routes __mcp_meta_call__ through the Router and writes the response
# back to our stdin with the matching ID.
_next_id = 10000
_id_lock = threading.Lock()
_pending = {}       # id → threading.Event
_responses = {}     # id → response dict
_stdin_lock = threading.Lock()
_reader_started = False
_inbound_queue = []  # lines that are requests FROM mcp-meta (not responses)


def _start_reader():
    """Background thread that reads stdin and routes responses by ID."""
    global _reader_started
    if _reader_started:
        return
    _reader_started = True
    # The reader runs in the main loop instead — we can't have two threads
    # reading stdin. Instead, call_pod uses a blocking approach with timeout.


def _get_next_id():
    global _next_id
    with _id_lock:
        _next_id += 1
        return _next_id


def call_pod(pod_tool: str, args: dict) -> dict:
    """Call another pod through MCP-Meta __mcp_meta_call__ routing.

    Writes request to stdout, then reads stdin lines until we find
    the response with our matching ID. Non-matching lines are queued
    for the main loop to process.
    """
    rid = _get_next_id()
    req = {
        "jsonrpc": "2.0",
        "id": rid,
        "method": "__mcp_meta_call__",
        "params": {"tool": pod_tool, "args": args},
    }
    sys.stdout.write(json.dumps(req) + "\n")
    sys.stdout.flush()

    # Read lines until we get our response (timeout 30s)
    deadline = time.time() + 30
    while time.time() < deadline:
        line = sys.stdin.readline()
        if not line:
            return {"error": "stdin closed"}
        line = line.strip()
        if not line:
            continue

        try:
            msg = json.loads(line)
        except json.JSONDecodeError:
            continue

        # Check if this is our response (matches our ID)
        msg_id = msg.get("id")
        if msg_id == rid:
            if "error" in msg and msg["error"]:
                err = msg["error"]
                if isinstance(err, dict):
                    return {"error": err.get("message", str(err))}
                return {"error": str(err)}
            return msg.get("result", {})

        # Not our response — it's a new request from MCP-Meta.
        # Queue it for the main loop.
        _inbound_queue.append(line)

    return {"error": "timeout waiting for inter-pod response"}


def call_pod_safe(pod_tool: str, args: dict) -> dict:
    """Call pod, return empty dict on failure."""
    try:
        result = call_pod(pod_tool, args)
        if isinstance(result, dict) and "error" in result:
            return {}
        return result
    except Exception:
        return {}


def llm_call(client: OpenAI, prompt: str, max_tokens: int = 600) -> str:
    resp = client.chat.completions.create(
        model=MODEL,
        messages=[{"role": "user", "content": prompt}],
        temperature=0.2,
        max_tokens=max_tokens,
    )
    return resp.choices[0].message.content or ""


def handle_intel(client: OpenAI, params: dict) -> dict:
    target = params.get("target", params.get("message", ""))
    if not target:
        return {"error": "target company/product is required"}

    aspects = params.get("aspects", ["product", "pricing", "recent news", "strengths", "weaknesses"])
    if isinstance(aspects, str):
        aspects = [a.strip() for a in aspects.split(",")]

    # 1. Check Hebbian for prior intel
    prior = call_pod_safe("hebbian.search", {"query": f"competitive intelligence {target}", "top_k": 5})
    prior_results = prior.get("results", []) if isinstance(prior, dict) else []
    prior_context = ""
    if prior_results:
        prior_context = "\n".join(f"- {r.get('content', '')}" for r in prior_results)

    # 2. Web search for company info
    web = call_pod_safe("kurai.web_search", {"query": f"{target} company product overview"})
    web_results = web.get("results", []) if isinstance(web, dict) else []

    # 3. News search for recent developments
    news = call_pod_safe("kurai.news_search", {"query": target, "time_range": "month"})
    news_results = news.get("results", []) if isinstance(news, dict) else []

    # 4. Build context
    web_snippets = "\n".join(
        f"- [{r.get('title','')}]: {r.get('snippet','')}"
        for r in web_results[:5]
    )
    news_snippets = "\n".join(
        f"- [{r.get('title','')}]: {r.get('snippet','')}"
        for r in news_results[:5]
    )

    prompt = f"""Competitive Intelligence Report: {target}

Analyze the following aspects: {', '.join(aspects)}
"""
    if prior_context:
        prompt += f"""
## Prior Intelligence (from memory):
{prior_context}
"""
    if web_snippets:
        prompt += f"""
## Web Research:
{web_snippets}
"""
    if news_snippets:
        prompt += f"""
## Recent News:
{news_snippets}
"""
    prompt += """
Produce a structured competitive intelligence report covering each aspect.
Note any changes from prior intelligence. Be specific with facts, numbers, and dates."""

    analysis = llm_call(client, prompt, max_tokens=800)

    # 5. Store in Hebbian
    if analysis:
        call_pod_safe("hebbian.ingest", {
            "question": f"Competitive intelligence on {target}",
            "answer": analysis,
            "pulled_ids": [r.get("id", "") for r in prior_results],
        })

    return {
        "target": target,
        "aspects": aspects,
        "analysis": analysis,
        "sources_web": len(web_results),
        "sources_news": len(news_results),
        "prior_intel_used": len(prior_results),
    }


def _process_request(client, line):
    """Process a single JSON-RPC request line."""
    line = line.strip()
    if not line:
        return
    try:
        req = json.loads(line)
    except json.JSONDecodeError:
        return

    req_id = req.get("id", 0)
    method = req.get("method", "")
    params = req.get("params", {})
    if isinstance(params, str):
        try:
            params = json.loads(params)
        except json.JSONDecodeError:
            params = {"message": params}

    resp = {"jsonrpc": "2.0", "id": req_id}

    try:
        if method == "ping":
            resp["result"] = {"status": "ok", "agent": "competitive-intel"}
        elif method == "intel":
            resp["result"] = handle_intel(client, params)
        else:
            resp["error"] = {"code": -32601, "message": f"method not found: {method}"}
    except Exception as exc:
        resp["error"] = {"code": -32000, "message": str(exc)}

    sys.stdout.write(json.dumps(resp) + "\n")
    sys.stdout.flush()


def main():
    client = OpenAI(base_url=LLM_BASE_URL, api_key=API_KEY or "lm-studio")

    for line in sys.stdin:
        _process_request(client, line)

        # Also process any queued inbound requests from inter-pod calls
        while _inbound_queue:
            queued = _inbound_queue.pop(0)
            _process_request(client, queued)


if __name__ == "__main__":
    main()
