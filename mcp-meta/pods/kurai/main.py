#!/usr/bin/env python3
"""
Kurai Pod - Web search, image search, and news via Kurai re-ranking engine.

Translates JSON-RPC 2.0 calls to HTTP requests against a running Kurai instance.
Uses only stdlib (urllib) — no external dependencies.
"""

import sys
import json
import os
import urllib.request
import urllib.parse
import urllib.error

KURAI_URL = os.environ.get("KURAI_URL", "http://localhost:8080")


def _kurai_get(path, params=None):
    """Make a GET request to the Kurai HTTP API."""
    url = KURAI_URL.rstrip("/") + path
    if params:
        # Filter out None values
        filtered = {k: v for k, v in params.items() if v is not None}
        if filtered:
            url += "?" + urllib.parse.urlencode(filtered)

    try:
        req = urllib.request.Request(url)
        with urllib.request.urlopen(req, timeout=25) as resp:
            return json.loads(resp.read().decode("utf-8"))
    except urllib.error.HTTPError as e:
        body = e.read().decode("utf-8", errors="replace")
        return {"error": f"Kurai HTTP {e.code}: {body[:500]}"}
    except urllib.error.URLError as e:
        return {"error": f"Kurai connection failed: {e.reason}"}
    except Exception as e:
        return {"error": f"Kurai request failed: {e}"}


def web_search(params):
    """Search the web via Kurai's /search endpoint."""
    query = params.get("query")
    if not query:
        raise ValueError("query is required")

    api_params = {"q": query}
    if "page" in params:
        api_params["page"] = params["page"]
    if "category" in params:
        api_params["category"] = params["category"]

    result = _kurai_get("/search", api_params)

    # Simplify the response for LLM consumption
    if "error" in result:
        return result

    simplified_results = []
    for r in result.get("results", []):
        entry = {
            "title": r.get("title", ""),
            "url": r.get("url", ""),
            "snippet": r.get("snippet", ""),
        }
        if r.get("badges"):
            entry["badges"] = r["badges"]
        if r.get("adjusted_score"):
            entry["score"] = round(r["adjusted_score"], 3)
        simplified_results.append(entry)

    return {
        "query": result.get("query", query),
        "results": simplified_results,
        "total": result.get("meta", {}).get("total", len(simplified_results)),
        "page": result.get("meta", {}).get("page", 1),
    }


def image_search(params):
    """Search for images via Kurai's /images endpoint."""
    query = params.get("query")
    if not query:
        raise ValueError("query is required")

    api_params = {"q": query}
    if "safesearch" in params:
        api_params["safesearch"] = params["safesearch"]
    if "size" in params:
        api_params["size"] = params["size"]

    return _kurai_get("/images", api_params)


def news_search(params):
    """Search news via Kurai's /news endpoint."""
    query = params.get("query")
    if not query:
        raise ValueError("query is required")

    api_params = {"q": query}
    if "time_range" in params:
        api_params["time_range"] = params["time_range"]
    if "cluster" in params:
        api_params["cluster"] = str(params["cluster"]).lower()

    return _kurai_get("/news", api_params)


def trending_news(params):
    """Get trending news stories via Kurai's /news/trending endpoint."""
    return _kurai_get("/news/trending")


def ping(params):
    # Also verify Kurai connectivity
    health = _kurai_get("/health")
    if "error" in health:
        return {"status": "degraded", "pod": "kurai", "upstream": health["error"]}
    return {"status": "ok", "pod": "kurai", "upstream": health.get("status", "unknown")}


def shutdown(params):
    return {"status": "shutting_down"}


TOOLS = {
    "web_search": web_search,
    "image_search": image_search,
    "news_search": news_search,
    "trending_news": trending_news,
    "ping": ping,
    "shutdown": shutdown,
}


def handle_request(request):
    method = request.get("method")
    params = request.get("params", {})
    request_id = request.get("id")

    if method not in TOOLS:
        return {
            "jsonrpc": "2.0",
            "id": request_id,
            "error": {"code": -32601, "message": f"Method not found: {method}"},
        }

    try:
        result = TOOLS[method](params)
        if method == "shutdown":
            response = {"jsonrpc": "2.0", "id": request_id, "result": result}
            print(json.dumps(response, ensure_ascii=False), flush=True)
            os._exit(0)
        return {"jsonrpc": "2.0", "id": request_id, "result": result}
    except ValueError as e:
        return {
            "jsonrpc": "2.0",
            "id": request_id,
            "error": {"code": -32602, "message": str(e)},
        }
    except Exception as e:
        return {
            "jsonrpc": "2.0",
            "id": request_id,
            "error": {"code": -32000, "message": str(e)},
        }


def main():
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            request = json.loads(line)
            response = handle_request(request)
            print(json.dumps(response, ensure_ascii=False), flush=True)
        except json.JSONDecodeError:
            print(
                json.dumps(
                    {
                        "jsonrpc": "2.0",
                        "id": None,
                        "error": {"code": -32700, "message": "Parse error"},
                    }
                ),
                flush=True,
            )


if __name__ == "__main__":
    main()
