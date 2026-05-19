"""
Research Agent Pod — deep web research with Hebbian memory.

Tools:
  - research(query, depth="standard") → structured findings
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


def llm_call(client: OpenAI, prompt: str, max_tokens: int = 500) -> str:
    resp = client.chat.completions.create(
        model=MODEL,
        messages=[{"role": "user", "content": prompt}],
        temperature=0.3,
        max_tokens=max_tokens,
    )
    return resp.choices[0].message.content or ""


def _extract_query(raw: str) -> str:
    """Extract the actual research query, stripping action prefixes."""
    # Handle patterns like "Search the web for: X", "Research: X", "Use X on: Y"
    for sep in [" for: ", " about: ", " on: ", " in: "]:
        if sep in raw:
            return raw.split(sep, 1)[1].strip()
    if raw.lower().startswith(("research:", "review:", "analyze:")):
        return raw.split(":", 1)[1].strip()
    return raw


def handle_research(client: OpenAI, params: dict) -> dict:
    raw = params.get("query", params.get("message", ""))
    if not raw:
        return {"error": "query is required"}
    query = _extract_query(raw)

    depth = params.get("depth", "standard")
    findings = []
    sources = []

    # 1. Check Hebbian for prior knowledge
    prior = call_pod_safe("hebbian.search", {"query": query, "top_k": 5})
    prior_results = prior.get("results", []) if isinstance(prior, dict) else []
    prior_context = ""
    if prior_results:
        lines = [f"- {r.get('content', '')}" for r in prior_results]
        prior_context = "\n".join(lines)
        findings.append({
            "type": "prior_knowledge",
            "content": prior_context,
            "source": "hebbian_knowledge_graph",
        })

    # 2. Web search (via search supervisor — quality gate + Tavily fallback)
    search_result = call_pod_safe("search.smart_search", {"query": query})
    web_results = search_result.get("results", []) if isinstance(search_result, dict) else []

    for r in web_results[:5]:
        sources.append({"title": r.get("title", ""), "url": r.get("url", "")})

    snippets = "\n".join(
        f"- [{r.get('title','')}]({r.get('url','')}): {r.get('snippet','')}"
        for r in web_results[:5]
    )

    # 3. Summarize with LLM
    prompt = f"""Research topic: {query}

"""
    if prior_context:
        prompt += f"""## Prior Knowledge (from memory):
{prior_context}

"""
    if snippets:
        prompt += f"""## Web Search Results:
{snippets}

"""
    prompt += """Synthesize a comprehensive research summary. Include key facts, cite sources, and identify any gaps in the available information. Be specific and factual."""

    summary = llm_call(client, prompt, max_tokens=800 if depth == "deep" else 500)

    findings.append({
        "type": "research_summary",
        "content": summary,
        "source": "llm_synthesis",
    })

    # 4. Store in Hebbian
    if summary:
        call_pod_safe("hebbian.ingest", {
            "question": query,
            "answer": summary,
            "pulled_ids": [r.get("id", "") for r in prior_results],
        })

    return {
        "query": query,
        "findings": findings,
        "sources": sources,
        "prior_knowledge_used": len(prior_results),
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
            resp["result"] = {"status": "ok", "agent": "research-agent"}
        elif method == "research":
            resp["result"] = handle_research(client, params)
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
