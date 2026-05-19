"""
Hebbian Knowledge Pod — MCP-Meta JSON-RPC service.

Wraps the research_agent Hebbian engine as a standalone tool pod.
Reads JSON-RPC requests from stdin, writes responses to stdout.
Persists knowledge in SQLite at ~/.hebbian/knowledge.db.

Tools:
  - search(query, top_k=5) → list of facts
  - ingest(question, answer, pulled_ids=[]) → new node IDs
  - store_finding(content, importance=0.5) → node ID
  - decay_and_prune() → stats
  - ping() → status
"""

import sys
import os
import json

# Add the agent project to path so we can import the Hebbian engine
AGENT_ROOT = os.environ.get(
    "AGENT_ROOT",
    os.path.join(os.path.dirname(__file__), "..", "..", "..", "agent", "src"),
)
if os.path.isdir(AGENT_ROOT):
    sys.path.insert(0, AGENT_ROOT)

from research_agent.knowledge import ResearchKnowledgeStore

DB_PATH = os.environ.get(
    "HEBBIAN_DB_PATH",
    os.path.join(os.path.expanduser("~"), ".hebbian", "knowledge.db"),
)

LLM_BASE_URL = os.environ.get("OPENROUTER_URL", "https://openrouter.ai/api/v1")
LLM_API_KEY = os.environ.get("OPENROUTER_API_KEY", "")
LLM_MODEL = os.environ.get("HEBBIAN_LLM_MODEL", "google/gemini-2.0-flash-001")


def build_store() -> ResearchKnowledgeStore:
    os.makedirs(os.path.dirname(DB_PATH), exist_ok=True)
    db_url = os.environ.get("DATABASE_URL", "")
    backend = "postgresql" if db_url.startswith(("postgresql://", "postgres://")) else "sqlite"
    print(f"[HEBBIAN] backend={backend} db_path={DB_PATH}", file=sys.stderr, flush=True)
    store = ResearchKnowledgeStore(
        db_path=DB_PATH,
        llm_base_url=LLM_BASE_URL if LLM_API_KEY else None,
        llm_api_key=LLM_API_KEY or None,
        llm_model=LLM_MODEL,
    )
    print(f"[HEBBIAN] loaded {len(store.graph.nodes)} nodes, {len(store.graph.edges)} edges", file=sys.stderr, flush=True)
    return store


def handle_search(store: ResearchKnowledgeStore, params: dict) -> dict:
    query = params.get("query", params.get("message", ""))
    top_k = params.get("top_k", 5)
    if not query:
        return {"error": "query is required"}
    results = store.search(query, top_k=top_k)
    return {"results": results, "count": len(results)}


def handle_ingest(store: ResearchKnowledgeStore, params: dict) -> dict:
    question = params.get("question", params.get("message", ""))
    answer = params.get("answer", "")
    pulled_ids = params.get("pulled_ids", [])
    if not question or not answer:
        return {"error": "question and answer are required"}
    new_ids = store.ingest(question, answer, pulled_ids=pulled_ids)
    print(f"[HEBBIAN] ingested {len(new_ids)} nodes, total={len(store.graph.nodes)} nodes, {len(store.graph.edges)} edges", file=sys.stderr, flush=True)
    return {
        "new_ids": new_ids,
        "total_nodes": len(store.graph.nodes),
    }


def handle_store_finding(store: ResearchKnowledgeStore, params: dict) -> dict:
    content = params.get("content", params.get("message", ""))
    importance = params.get("importance", 0.5)
    if not content:
        return {"error": "content is required"}
    node_id = store.store_finding(content, importance=importance)
    return {"node_id": node_id}


def handle_graph(store: ResearchKnowledgeStore, params: dict) -> dict:
    """Return full graph structure for visualization."""
    nodes = []
    for nid, node in store.graph.nodes.items():
        nodes.append({
            "id": nid,
            "label": node.label,
            "content": (node.content or node.summary or "")[:200],
            "type": node.node_type,
            "importance": node.importance,
            "access_count": node.access_count,
        })

    edges = []
    for source_id, edge_list in store.graph.outgoing_edges.items():
        for edge in edge_list:
            edges.append({
                "source": edge.source_id,
                "target": edge.target_id,
                "weight": round(edge.weight, 3),
                "relation": edge.relation_type.value if hasattr(edge.relation_type, 'value') else str(edge.relation_type),
                "co_activations": edge.co_activations,
            })

    return {
        "nodes": nodes,
        "edges": edges,
        "total_nodes": len(nodes),
        "total_edges": len(edges),
    }


def handle_decay_and_prune(store: ResearchKnowledgeStore, params: dict) -> dict:
    stats = store.decay_and_prune()
    return stats


def main():
    store = build_store()

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
                    "agent": "hebbian",
                    "nodes": len(store.graph.nodes),
                }
            elif method == "search":
                resp["result"] = handle_search(store, params)
            elif method == "ingest":
                resp["result"] = handle_ingest(store, params)
            elif method == "store_finding":
                resp["result"] = handle_store_finding(store, params)
            elif method == "decay_and_prune":
                resp["result"] = handle_decay_and_prune(store, params)
            elif method == "graph":
                resp["result"] = handle_graph(store, params)
            else:
                resp["error"] = {"code": -32601, "message": f"method not found: {method}"}
        except Exception as exc:
            resp["error"] = {"code": -32000, "message": str(exc)}

        sys.stdout.write(json.dumps(resp) + "\n")
        sys.stdout.flush()


if __name__ == "__main__":
    main()
