"""
Search Supervisor Pod — smart search with embedding-based quality gate.

Searches kurai first. Embeds query + result snippets via LM Studio.
If average cosine similarity < threshold, falls back to Tavily.

Tools:
  - smart_search(query, top_k=5) → results with backend indicator
  - ping
"""

import sys
import json
import os
import math
import urllib.request
import urllib.error

TAVILY_KEY = os.environ.get("TAVILY_API_KEY", "")
EMBEDDING_URL = os.environ.get("EMBEDDING_URL", "http://localhost:1234/v1")
MCP_URL = os.environ.get("MCP_META_URL", "http://127.0.0.1:9876")
QUALITY_THRESHOLD = float(os.environ.get("SEARCH_QUALITY_THRESHOLD", "0.5"))


def http_post(url, payload, headers=None, timeout=15):
    """Simple HTTP POST."""
    data = json.dumps(payload).encode()
    req = urllib.request.Request(url, data=data)
    req.add_header("Content-Type", "application/json")
    if headers:
        for k, v in headers.items():
            req.add_header(k, v)
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        return json.loads(resp.read())


def call_mcp(tool, args):
    """Call a tool via MCP-Meta HTTP API."""
    try:
        result = http_post(f"{MCP_URL}/v1/execute", {"tool": tool, "arguments": args}, timeout=25)
        return result.get("result", result)
    except Exception as e:
        return {"error": str(e)}


def embed(texts):
    """Embed one or more texts via LM Studio."""
    try:
        result = http_post(f"{EMBEDDING_URL}/embeddings", {
            "model": "text-embedding-qwen3-embedding-0.6b",
            "input": texts if isinstance(texts, list) else [texts],
        }, timeout=10)
        return [d["embedding"] for d in result.get("data", [])]
    except Exception:
        return []


def cosine_sim(a, b):
    """Cosine similarity between two vectors."""
    if len(a) != len(b) or not a:
        return 0.0
    dot = sum(x * y for x, y in zip(a, b))
    norm_a = math.sqrt(sum(x * x for x in a))
    norm_b = math.sqrt(sum(x * x for x in b))
    if norm_a == 0 or norm_b == 0:
        return 0.0
    return dot / (norm_a * norm_b)


def search_kurai(query, top_k=5):
    """Search via kurai pod."""
    result = call_mcp("kurai.web_search", {"query": query})
    if isinstance(result, dict) and "error" not in result:
        results = result.get("results", [])
        return results[:top_k]
    return []


def search_tavily(query, top_k=5):
    """Search via Tavily API."""
    if not TAVILY_KEY:
        return []
    try:
        result = http_post("https://api.tavily.com/search", {
            "api_key": TAVILY_KEY,
            "query": query,
            "max_results": top_k,
            "include_answer": False,
        }, timeout=15)
        # Normalize Tavily format to match kurai format
        normalized = []
        for r in result.get("results", []):
            normalized.append({
                "title": r.get("title", ""),
                "url": r.get("url", ""),
                "snippet": r.get("content", ""),
                "score": r.get("score", 0),
            })
        return normalized
    except Exception as e:
        return [{"error": str(e)}]


def evaluate_quality(query, results):
    """Embed query + snippets, return average cosine similarity."""
    if not results:
        return 0.0

    snippets = [r.get("snippet", r.get("title", "")) for r in results[:5] if r.get("snippet") or r.get("title")]
    if not snippets:
        return 0.0

    # Batch embed: query + all snippets in one call
    texts = [query] + snippets
    vectors = embed(texts)

    if len(vectors) < 2:
        # Embedding failed — assume quality is OK to avoid unnecessary Tavily calls
        return 1.0

    query_vec = vectors[0]
    snippet_vecs = vectors[1:]

    similarities = [cosine_sim(query_vec, sv) for sv in snippet_vecs]
    avg_sim = sum(similarities) / len(similarities) if similarities else 0.0

    return avg_sim


def _extract_query(raw: str) -> str:
    """Strip action prefixes from queries like 'Search the web for: X'."""
    for sep in [" for: ", " about: ", " on: ", " in: "]:
        if sep in raw:
            return raw.split(sep, 1)[1].strip()
    if raw.lower().startswith(("search:", "research:", "analyze:")):
        return raw.split(":", 1)[1].strip()
    return raw


def handle_smart_search(params):
    raw = params.get("query", params.get("message", ""))
    if not raw:
        return {"error": "query is required"}
    query = _extract_query(raw)

    top_k = params.get("top_k", 5)

    # Step 1: Search kurai
    kurai_results = search_kurai(query, top_k)

    # Step 2: Evaluate quality via embeddings
    avg_sim = evaluate_quality(query, kurai_results)

    # Step 3: Decide
    if kurai_results and avg_sim >= QUALITY_THRESHOLD:
        return {
            "query": query,
            "results": kurai_results,
            "backend": "kurai",
            "quality_score": round(avg_sim, 3),
            "total": len(kurai_results),
        }

    # Step 4: Fallback to Tavily
    tavily_results = search_tavily(query, top_k)

    if tavily_results and not any("error" in r for r in tavily_results):
        return {
            "query": query,
            "results": tavily_results,
            "backend": "tavily",
            "quality_score": round(avg_sim, 3),
            "kurai_rejected_reason": f"avg_similarity={avg_sim:.3f} < {QUALITY_THRESHOLD}",
            "total": len(tavily_results),
        }

    # Both failed — return whatever kurai had (better than nothing)
    return {
        "query": query,
        "results": kurai_results,
        "backend": "kurai_fallback",
        "quality_score": round(avg_sim, 3),
        "tavily_error": "tavily unavailable or returned no results",
        "total": len(kurai_results),
    }


def main():
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            req = json.loads(line)
        except json.JSONDecodeError:
            continue

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
                resp["result"] = {
                    "status": "ok",
                    "agent": "search-supervisor",
                    "tavily_configured": bool(TAVILY_KEY),
                    "embedding_url": EMBEDDING_URL,
                }
            elif method == "smart_search":
                resp["result"] = handle_smart_search(params)
            else:
                resp["error"] = {"code": -32601, "message": f"method not found: {method}"}
        except Exception as exc:
            resp["error"] = {"code": -32000, "message": str(exc)}

        sys.stdout.write(json.dumps(resp) + "\n")
        sys.stdout.flush()


if __name__ == "__main__":
    main()
