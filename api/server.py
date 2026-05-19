from __future__ import annotations

import asyncio
import base64
import json as _json_mod
import logging
import os
import time
import uuid
from pathlib import Path

logger = logging.getLogger(__name__)

for _p in [Path(__file__).parent.parent, Path.cwd(), Path.cwd().parent]:
    _env = _p / ".env"
    if _env.exists():
        for _line in _env.read_text().splitlines():
            if "=" in _line and not _line.startswith("#"):
                _k, _v = _line.split("=", 1)
                os.environ.setdefault(_k.strip(), _v.strip())
        break

from fastapi import FastAPI, Depends, HTTPException, Request
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import StreamingResponse
from pydantic import BaseModel

from research_agent.x402_middleware import x402_gate
from research_agent.x402_config import X402_ESCROW_CONTRACT

from research_agent.soroban_client import (
    list_active_full,
    get_rep,
    stroops_to_xlm,
    submit_claim_task,
    submit_register_tool,
    verify_compression,
    invalidate_registry_cache,
)
from research_agent.hebbian_bridge import query_hebbian, reinforce_hebbian, store_output
from research_agent.hire import _match_capabilities
from research_agent.mcp_client import send_a2a_message, execute_tool
from research_agent.escrow import create_escrow, settle_escrow, settle_team_escrow
from research_agent.team import (
    compose_team,
    decompose_task,
    estimate_team_cost,
    build_execution_plan,
)
from research_agent.stellar_identity import (
    generate_agent_keypair,
    fund_testnet,
    set_capability_manifest,
    establish_rep_trustline,
    read_capability_manifest,
)
from research_agent.stellar_config import (
    NETWORK_PASSPHRASE,
    HORIZON_URL,
    RPC_URL,
    STELLAR_NETWORK,
)
from stellar_sdk import Keypair, Server

from stellar_health import router as stellar_health_router

from contextlib import asynccontextmanager


async def _refund_sweeper():
    """
    Background task: refund expired escrows + clean up old data every 60 seconds.
    """
    import logging
    from research_agent.refund_sweeper import sweep_expired_refunds
    from research_agent.escrow import refund_escrow, get_escrow_status

    logger = logging.getLogger("sweeper")

    # Per-sweep cap: 10 refunds default. Override via env so a flood of expirations can be controlled without a code change.
    max_per_sweep = int(os.environ.get("REFUND_SWEEPER_MAX_PER_CYCLE", "10"))
    retry_interval = float(
        os.environ.get("REFUND_SWEEPER_RETRY_INTERVAL_SECONDS", "600")
    )

    while True:
        await asyncio.sleep(60)
        try:
            # Refund expired escrows (idempotent + rate-limited + reconciling)
            orch_kp = _load_orchestrator_kp()
            if orch_kp:
                result = sweep_expired_refunds(
                    _store,
                    orch_kp,
                    refund_fn=refund_escrow,
                    status_fn=get_escrow_status,
                    max_per_sweep=max_per_sweep,
                    retry_interval_seconds=retry_interval,
                )
                if any(
                    result[k] for k in ("refunded", "reconciled", "failed", "skipped")
                ):
                    logger.info(
                        "Sweeper: refunded=%d reconciled=%d failed=%d "
                        "dead_letter=%d skipped=%d",
                        result["refunded"],
                        result["reconciled"],
                        result["failed"],
                        result["dead_letter"],
                        result["skipped"],
                    )

            # Clean up old completed tasks (>24h)
            cleaned = _store.cleanup(max_age_seconds=86400)
            if cleaned:
                logger.info(f"Sweeper: cleaned {cleaned} old tasks")

            # Clean up expired nonces (>5min)
            _store.cleanup_nonces(ttl=300)
        except Exception as e:
            logger.error(f"Sweeper: unexpected error: {e}")


@asynccontextmanager
async def lifespan(app_instance):
    sweeper_task = asyncio.create_task(_refund_sweeper())
    yield
    sweeper_task.cancel()


app = FastAPI(title="Agent Marketplace API", lifespan=lifespan)

_ALLOWED_ORIGINS = [
    o.strip()
    for o in os.environ.get(
        "ALLOWED_ORIGINS",
        "http://localhost:3002,http://127.0.0.1:3002",
    ).split(",")
    if o.strip()
]
app.add_middleware(
    CORSMiddleware,
    allow_origins=_ALLOWED_ORIGINS,
    allow_credentials=False,
    allow_methods=["GET", "POST", "OPTIONS"],
    allow_headers=["*"],
    expose_headers=["PAYMENT-REQUIRED", "PAYMENT-RESPONSE"],
)

from starlette.middleware.base import BaseHTTPMiddleware


class SecurityHeadersMiddleware(BaseHTTPMiddleware):
    async def dispatch(self, request, call_next):
        response = await call_next(request)
        response.headers["X-Content-Type-Options"] = "nosniff"
        response.headers["X-Frame-Options"] = "DENY"
        response.headers["Referrer-Policy"] = "strict-origin-when-cross-origin"
        response.headers["X-XSS-Protection"] = "1; mode=block"
        if request.url.scheme == "https":
            response.headers["Strict-Transport-Security"] = (
                "max-age=31536000; includeSubDomains"
            )
        return response


app.add_middleware(SecurityHeadersMiddleware)

from slowapi import Limiter, _rate_limit_exceeded_handler
from slowapi.errors import RateLimitExceeded


def _real_ip_key(request: Request) -> str:
    """
    Extract the actual client IP for rate-limit keying.
    """
    xff = request.headers.get("X-Forwarded-For", "")
    if xff:
        # Front Door appends the real client IP rightmost. Split, take last.
        return [ip.strip() for ip in xff.split(",") if ip.strip()][-1]
    return request.client.host if request.client else "unknown"


limiter = Limiter(key_func=_real_ip_key)
app.state.limiter = limiter
app.add_exception_handler(RateLimitExceeded, _rate_limit_exceeded_handler)


_PUBLIC_PATHS = {
    "/api/auth/challenge",
    "/api/auth/challenge-tx",
    "/api/auth/session",
    "/api/registry",
    "/api/hebbian",
    "/api/hebbian/graph",
    "/api/discover",
    "/api/hire",
    "/api/hire-team",
    "/api/debate",
    "/api/execute-a2a",
    "/api/build-payment",
    "/api/build-payment-generic",
    "/docs",
    "/openapi.json",
    "/redoc",
}

_PUBLIC_PREFIXES = (
    "/api/escrow/",  # escrow status by task_id
    "/api/agent/",  # public agent info by public_key
)


class AuthGateMiddleware(BaseHTTPMiddleware):
    """Require authentication on all /api/ endpoints except public ones."""

    async def dispatch(self, request, call_next):
        path = request.url.path

        # Always allow CORS preflight requests
        if request.method == "OPTIONS":
            return await call_next(request)

        # Skip non-API paths and public endpoints
        if not path.startswith("/api/"):
            return await call_next(request)
        if path in _PUBLIC_PATHS:
            return await call_next(request)
        if any(path.startswith(prefix) for prefix in _PUBLIC_PREFIXES):
            return await call_next(request)

        has_wallet_auth = request.headers.get("X-Wallet-Auth")
        has_payment_sig = request.headers.get("PAYMENT-SIGNATURE")
        auth_header = request.headers.get("Authorization", "")
        has_bearer = auth_header.startswith("Bearer ") and len(auth_header) > len(
            "Bearer "
        )

        if not has_wallet_auth and not has_payment_sig and not has_bearer:
            from fastapi.responses import JSONResponse

            resp = JSONResponse(
                status_code=401,
                content={"detail": "Authentication required. Connect your wallet."},
            )
            resp.headers["X-Content-Type-Options"] = "nosniff"
            resp.headers["X-Frame-Options"] = "DENY"
            resp.headers["Referrer-Policy"] = "strict-origin-when-cross-origin"
            return resp

        return await call_next(request)


app.add_middleware(AuthGateMiddleware)

_AZURE_FDID_EXPECTED = os.getenv("AZURE_FDID", "").strip()


class FrontDoorOriginGate(BaseHTTPMiddleware):
    async def dispatch(self, request, call_next):
        if not _AZURE_FDID_EXPECTED:
            return await call_next(request)

        path = request.url.path
        if not path.startswith("/api/"):
            return await call_next(request)

        if request.method == "OPTIONS":
            return await call_next(request)

        received = request.headers.get("X-Azure-FDID", "").strip()
        if received != _AZURE_FDID_EXPECTED:
            from fastapi.responses import JSONResponse

            return JSONResponse(
                status_code=403,
                content={
                    "detail": "Requests must arrive via Front Door (FDID header missing or mismatched)."
                },
            )

        return await call_next(request)


app.add_middleware(FrontDoorOriginGate)

from research_agent.task_store import TaskStore

_store = TaskStore()

_TASK_STORE_COLUMNS = {
    "task_id",
    "wallet_address",
    "agent_handle",
    "task_text",
    "escrow_tx",
    "deadline_unix",
    "status",
    "result_json",
    "created_at",
    "settled",
    "refunded",
}


def _get_wallet(request: Request | None) -> str:
    """Extract wallet_address from request.state (set by x402 middleware)."""
    if request and hasattr(request.state, "wallet_address"):
        return request.state.wallet_address
    return ""


def _validate_signed_challenge(
    public_key: str,
    challenge: str,
    signature: str,
    signed_xdr: str,
) -> None:
    """Validate a signed wallet challenge. Raises HTTPException on failure.

    Consumes one nonce slot on success (or 401 if the nonce was used twice
    already). Used by both `_wallet_auth` (legacy single-use header) and the
    `/api/auth/session` endpoint (one-time exchange for a JWT).

    Supports two formats:
    - Format 1 (raw signature): publicKey + challenge + signature
    - Format 2 (Wallet Kit / Freighter signed XDR): publicKey + challenge + signedXdr
    """
    if not public_key or not challenge:
        raise HTTPException(400, "Missing publicKey or challenge")
    if not signature and not signed_xdr:
        raise HTTPException(400, "Missing signature or signedXdr")

    challenge_key = f"auth-challenge-{challenge}"
    challenge_key_2 = f"auth-challenge-2-{challenge}"
    if not _store.claim_nonce(challenge_key):
        if not _store.claim_nonce(challenge_key_2):
            raise HTTPException(401, "Challenge already used or expired")

    if signed_xdr:
        from stellar_sdk import TransactionEnvelope, Keypair as _Kp

        try:
            te = TransactionEnvelope.from_xdr(signed_xdr, NETWORK_PASSPHRASE)
        except Exception:
            raise HTTPException(400, "Malformed signedXdr")

        try:
            source_addr = te.transaction.source.account_id
        except AttributeError:
            source_addr = str(te.transaction.source)
        if source_addr != public_key:
            raise HTTPException(
                403,
                "Signed XDR source account does not match claimed publicKey",
            )

        tx_hash = te.hash()
        signer_kp = _Kp.from_public_key(public_key)
        verified = False
        for sig in te.signatures:
            try:
                signer_kp.verify(tx_hash, sig.signature)
                verified = True
                break
            except Exception:
                continue
        if not verified:
            raise HTTPException(403, "Signed XDR not signed by claimed publicKey")

        memo = te.transaction.memo
        memo_text = getattr(memo, "memo_text", None)
        if (
            memo_text is None
            or (
                memo_text.decode("utf-8", errors="replace")
                if isinstance(memo_text, (bytes, bytearray))
                else memo_text
            )
            != challenge
        ):
            raise HTTPException(
                403,
                "Signed XDR memo does not match claimed challenge",
            )

        from stellar_sdk.operation import ManageData

        found_manage_data = False
        for op in te.transaction.operations:
            if isinstance(op, ManageData) and op.data_name == "auth_challenge":
                op_value = op.data_value
                op_value_str = (
                    op_value.decode("utf-8", errors="replace")
                    if isinstance(op_value, (bytes, bytearray))
                    else op_value
                )
                if op_value_str == challenge:
                    found_manage_data = True
                    break
        if not found_manage_data:
            raise HTTPException(
                403,
                "Signed XDR missing required manage_data auth_challenge op",
            )
    else:
        from research_agent.wallet_auth import verify_wallet_signature

        if not verify_wallet_signature(public_key, challenge, signature):
            raise HTTPException(403, "Invalid wallet signature")


def _wallet_auth(request: Request) -> str:
    """FastAPI dependency: verify wallet auth and return the signer public key.

    Accepts either:
    - `Authorization: Bearer <jwt>` — session token (preferred; 1 sign per
      hour). Issued by POST /api/auth/session in exchange for a signed
      challenge. Subsequent calls within the JWT TTL bypass Freighter
      prompts entirely.
    - `X-Wallet-Auth: base64(JSON)` — legacy single-use payload. Each call
      consumes a fresh challenge nonce (Format 1 raw sig OR Format 2 signed
      XDR). Kept for CLI / one-off use and backward compat.

    Sessions are issued via `_validate_signed_challenge` (same validation
    logic) — single source of truth for what constitutes wallet ownership.
    """
    auth_header = request.headers.get("Authorization", "")
    if auth_header.startswith("Bearer "):
        token = auth_header[7:].strip()
        from research_agent import session_tokens
        import jwt as _jwt

        try:
            return session_tokens.verify(token)
        except _jwt.ExpiredSignatureError:
            raise HTTPException(401, "Session token expired. Sign in again.")
        except _jwt.InvalidTokenError:
            raise HTTPException(401, "Invalid session token.")

    header = request.headers.get("X-Wallet-Auth")
    if not header:
        raise HTTPException(
            401,
            "Authentication required. Use Authorization: Bearer <token> "
            "(exchange via POST /api/auth/session) or X-Wallet-Auth header.",
        )

    try:
        auth_data = _json_mod.loads(base64.b64decode(header))
        public_key = auth_data.get("publicKey", "")
        challenge = auth_data.get("challenge", "")
        signature = auth_data.get("signature", "")
        signed_xdr = auth_data.get("signedXdr", "")
    except Exception:
        raise HTTPException(400, "Malformed X-Wallet-Auth header")

    _validate_signed_challenge(public_key, challenge, signature, signed_xdr)
    return public_key


def _save_task(task_id: str, data: dict) -> dict:
    """Create a new task in the store from an arbitrary dict."""
    extras = {k: v for k, v in data.items() if k not in _TASK_STORE_COLUMNS}
    _store.create_task(
        task_id=task_id,
        wallet_address=data.get("wallet_address", ""),
        agent_handle=data.get("agent_handle", ""),
        task_text=data.get("task", data.get("task_text", "")),
        escrow_tx=data.get("escrow_tx") or "",
        deadline_unix=data.get("deadline_unix", 0),
        status=data.get("status", "pending"),
    )
    if extras:
        _store.update_task(task_id, result_json=_json_mod.dumps(extras))
    return _load_task(task_id)


def _load_task(task_id: str, wallet_address: str | None = None) -> dict | None:
    """
    Load a task and unpack result_json extras into the top-level dict.
    """
    if wallet_address:
        row = _store.get_task(task_id, wallet_address=wallet_address)
    else:
        row = _store.get_task(task_id)
    if row is None:
        return None
    blob = row.pop("result_json", "") or ""
    if blob:
        try:
            extras = _json_mod.loads(blob)
            if isinstance(extras, dict):
                row.update(extras)
        except (ValueError, TypeError):
            pass
    if "task" not in row and "task_text" in row:
        row["task"] = row["task_text"]
    return row


def _update_task(task_id: str, **fields) -> None:
    """Update a task.  DB-native fields go to columns; extras merge into result_json."""
    db_fields = {}
    extra_fields = {}
    for k, v in fields.items():
        if k in {
            "status",
            "settled",
            "refunded",
            "escrow_tx",
            "agent_handle",
            "deadline_unix",
        }:
            db_fields[k] = v
        else:
            extra_fields[k] = v

    if db_fields:
        _store.update_task(task_id, **db_fields)

    if extra_fields:
        row = _store.get_task(task_id)
        if row:
            blob = row.get("result_json", "") or ""
            try:
                existing = _json_mod.loads(blob) if blob else {}
            except (ValueError, TypeError):
                existing = {}
            existing.update(extra_fields)
            _store.update_task(task_id, result_json=_json_mod.dumps(existing))


def _load_knowledge():
    try:
        from research_agent.knowledge import ResearchKnowledgeStore
        from research_agent.provider import LM_STUDIO

        return ResearchKnowledgeStore(
            llm_base_url=LM_STUDIO.base_url,
            llm_api_key=LM_STUDIO.api_key,
            llm_model=LM_STUDIO.model,
        )
    except Exception:
        return None


def _load_orchestrator_kp() -> Keypair | None:
    secret = os.environ.get("ORCHESTRATOR_SECRET_KEY")
    if not secret:
        return None
    return Keypair.from_secret(secret)


class DiscoverRequest(BaseModel):
    task: str
    capabilities: list[str]
    budget_xlm: float


class HireRequest(BaseModel):
    agent_handle: str
    task: str
    budget_xlm: float
    user_public_key: str | None = None


class BuildPaymentRequest(BaseModel):
    agent_handle: str
    amount_usdc: str
    user_public_key: str


class BuildGenericPaymentRequest(BaseModel):
    amount_usdc: str
    user_public_key: str
    deadline_seconds: int = 300  # 5 min default


class SubmitPaymentRequest(BaseModel):
    signed_xdr: str
    agent_handle: str
    task: str
    budget_xlm: float
    task_id: str | None = (
        None  # From build-payment response; if missing, auto-generated
    )
    link_task_id: str | None = (
        None  # If set, links escrow TX to this existing task (for team flows)
    )


class BuildDeactivateRequest(BaseModel):
    handle: str


class SubmitDeactivateRequest(BaseModel):
    signed_xdr: str
    handle: str


class OnboardRequest(BaseModel):
    handle: str
    url: str = "http://localhost:3001/invoke"
    capabilities: list[str]
    tier: str
    rate_stroops: int
    max_tokens: int = 4096


class HireTeamRequest(BaseModel):
    task: str
    capabilities: list[str]
    budget_xlm: float


class DebateRequest(BaseModel):
    question: str


class ExecuteA2ARequest(BaseModel):
    task: str
    context_id: str | None = None


def _format_output(raw: str) -> str:
    """Convert raw pod JSON output into clean markdown for the frontend."""
    import json as _json

    try:
        data = _json.loads(raw)
    except (ValueError, TypeError):
        return raw  # Already plain text / markdown

    if not isinstance(data, dict):
        return raw

    if "error" in data and len(data) <= 2:
        return f"**Error:** {data['error']}"

    # trend-watcher: has "analysis" field with markdown
    if "analysis" in data:
        parts = [data["analysis"]]
        meta = []
        if data.get("trends_identified"):
            meta.append(f"**Trends identified:** {data['trends_identified']}")
        if data.get("news_sources"):
            meta.append(f"**News sources consulted:** {data['news_sources']}")
        if data.get("historical_context_used"):
            meta.append(
                f"**Historical context used:** {data['historical_context_used']}"
            )
        if meta:
            parts.append("\n---\n" + " · ".join(meta))
        return "\n".join(parts)

    # research-agent / competitive-intel: has "findings" list
    if "findings" in data and isinstance(data["findings"], list):
        parts = []
        for f in data["findings"]:
            if not isinstance(f, dict):
                continue
            source = f.get("source", "")
            content = f.get("content", "")
            if f.get("type") == "research_summary":
                parts.append(content)
            elif content:
                label = source.replace("_", " ").title() if source else "Finding"
                parts.append(f"### {label}\n\n{content}")
        # Append linked sources
        sources = data.get("sources", [])
        if sources and isinstance(sources, list):
            valid = [
                s
                for s in sources
                if isinstance(s, dict) and s.get("url") and s.get("title")
            ]
            if valid:
                parts.append("\n---\n### Sources\n")
                for s in valid:
                    parts.append(f"- [{s['title']}]({s['url']})")
        if data.get("prior_knowledge_used"):
            parts.append(
                f"\n*Prior knowledge nodes used: {data['prior_knowledge_used']}*"
            )
        return "\n\n".join(parts) if parts else raw

    # reviewer: has "review" field
    if "review" in data:
        return data["review"]

    # summarizer: has "summary" field
    if "summary" in data:
        return data["summary"]

    # content-writer: has "content" or "article" field
    for key in ("content", "article", "text", "output", "result"):
        if key in data and isinstance(data[key], str) and len(data[key]) > 50:
            return data[key]

    # Debate/ADR output — return raw JSON for frontend to parse and render
    if "adr" in data and ("transcript" in data or "decision" in data):
        return raw

    # Unknown JSON shape — render key-value pairs as markdown
    parts = []
    for k, v in data.items():
        if isinstance(v, str) and len(v) > 100:
            parts.append(f"### {k.replace('_', ' ').title()}\n\n{v}")
        elif isinstance(v, (str, int, float, bool)):
            parts.append(f"**{k.replace('_', ' ').title()}:** {v}")
    return "\n\n".join(parts) if parts else raw


def _format_team_output(results: dict, sub_tasks: list) -> str:
    """Format team execution results into a unified markdown document."""

    task_map = {}
    for st in sub_tasks:
        if isinstance(st, dict):
            task_map[st.get("id", "")] = st
        else:
            task_map[getattr(st, "id", "")] = st

    parts = []
    for task_id, raw_output in results.items():
        st = task_map.get(task_id, {})
        handle = (
            st.get("agent_handle", task_id)
            if isinstance(st, dict)
            else getattr(st, "agent_handle", task_id)
        )
        action = (
            st.get("action", "") if isinstance(st, dict) else getattr(st, "action", "")
        )

        header = f"## {handle}"
        if action:
            header += f"\n*{action[:120]}*"

        formatted = (
            _format_output(raw_output)
            if isinstance(raw_output, str)
            else str(raw_output)
        )
        parts.append(f"{header}\n\n{formatted}")

    return "\n\n---\n\n".join(parts)


def _estimate_hire_cost(body: dict) -> dict:
    """Estimate cost for a single-agent hire based on registry rates."""
    agent_handle = body.get("agent_handle", "")
    try:
        tools = list_active_full()
        tool = next((t for t in tools if t["handle"] == agent_handle), None)
        if tool:
            rate_xlm = stroops_to_xlm(tool["rate_stroops"])
            # Estimate ~3 tool calls per hire task
            estimated_calls = 3
            estimated_cost = round(rate_xlm * estimated_calls, 7)
            return {
                "estimated_cost": str(estimated_cost),
                "rate_breakdown": [
                    {"tool": cap, "rate": str(rate_xlm)} for cap in tool["capabilities"]
                ],
            }
    except Exception:
        pass
    # Fallback
    return {"estimated_cost": "0.10", "rate_breakdown": []}


def _estimate_debate_cost(body: dict) -> dict:
    """Flat-rate estimate for architecture debates (~4 agents, ~24 LLM calls)."""
    return {
        "estimated_cost": "0.25",
        "rate_breakdown": [
            {"tool": "adr-moderator.debate", "rate": "0.25"},
        ],
    }


_MAX_A2A_TASK_WORDS = int(os.environ.get("MAX_A2A_TASK_WORDS", "5000"))
_MAX_A2A_ESTIMATED_USDC = float(os.environ.get("MAX_A2A_ESTIMATED_USDC", "5.00"))


def _estimate_a2a_cost(body: dict) -> dict:
    """
    Estimate cost for generic A2A execution based on task length.
    """
    task = body.get("task", "")
    word_count = len(task.split())
    if word_count > _MAX_A2A_TASK_WORDS:
        raise HTTPException(
            413,
            f"Task too long: {word_count} words > {_MAX_A2A_TASK_WORDS} cap. "
            f"Override via MAX_A2A_TASK_WORDS env if you need a higher limit.",
        )
    raw_estimate = 0.10 + word_count * 0.001
    estimated = round(min(raw_estimate, _MAX_A2A_ESTIMATED_USDC), 7)
    return {
        "estimated_cost": str(estimated),
        "rate_breakdown": [
            {"tool": "a2a-execution", "rate": str(estimated)},
        ],
    }


@app.get("/health")
def health():
    return {"status": "ok"}


app.include_router(stellar_health_router)


def _require_dev_environment():
    if STELLAR_NETWORK == "public":
        raise HTTPException(404, "Not found")


@app.get("/debug/mcp-meta")
def debug_mcp_meta():
    """
    Diagnostic: test connectivity and A2A call to mcp-meta.
    """
    _require_dev_environment()
    import httpx
    from research_agent.mcp_client import _get_mcp_url, _get_mcp_headers

    mcp_url = _get_mcp_url()
    headers = _get_mcp_headers()
    result = {"mcp_url": mcp_url, "headers_keys": list(headers.keys())}
    try:
        resp = httpx.get(f"{mcp_url}/health", headers=headers, timeout=10.0)
        result["health_status"] = resp.status_code
        result["health_body"] = resp.text[:200]
    except Exception as exc:
        result["health_error"] = f"{type(exc).__name__}: {exc}"
    try:
        a2a_payload = {
            "jsonrpc": "2.0",
            "id": 1,
            "method": "message/send",
            "params": {
                "message": {
                    "role": "user",
                    "parts": [{"kind": "text", "text": "ping test"}],
                }
            },
        }
        resp3 = httpx.post(
            f"{mcp_url}/a2a", json=a2a_payload, headers=headers, timeout=30.0
        )
        result["a2a_status"] = resp3.status_code
        result["a2a_body"] = resp3.text[:500]
    except Exception as exc:
        result["a2a_error"] = f"{type(exc).__name__}: {exc}"
    return result


@app.get("/debug/stream-debate")
async def debug_stream_debate():
    """Test the full streaming debate path: FastAPI → mcp-meta /a2a/stream → SSE.

    Testnet/dev only — each call spends real OpenRouter budget.
    """
    _require_dev_environment()
    import json as _json
    import queue

    from research_agent.mcp_client import stream_a2a_message

    async def event_stream():
        def send(event_type: str, message: str, data: dict | None = None):
            event = {
                "type": event_type,
                "timestamp": time.strftime("%H:%M:%S"),
                "message": message,
            }
            if data:
                event["data"] = data
            return f"data: {_json.dumps(event)}\n\n"

        yield send("log", "Starting debug stream debate")

        q: queue.Queue = queue.Queue()
        _DONE = object()

        def _stream():
            try:
                for evt in stream_a2a_message(
                    "Redis vs Memcached?",
                    context_id="debug-test",
                    skill_id="adr-debate",
                ):
                    q.put(evt)
            except Exception as exc:
                q.put({"event": "error", "data": {"error": str(exc)}})
            finally:
                q.put(_DONE)

        loop = asyncio.get_event_loop()
        loop.run_in_executor(None, _stream)

        final_result = None
        while True:
            evt = await loop.run_in_executor(None, q.get)
            if evt is _DONE:
                break
            if evt["event"] == "progress":
                yield send("progress", "", evt["data"])
            elif evt["event"] == "result":
                final_result = evt["data"]
            elif evt["event"] == "error":
                yield send("error", str(evt["data"]))
                return

        if final_result:
            yield send("complete", "Debate finished")
        else:
            yield send("error", "No result")

    return StreamingResponse(event_stream(), media_type="text/event-stream")


@app.post("/api/discover")
@limiter.limit("10/minute")
def discover(req: DiscoverRequest, request: Request = None):
    tools = list_active_full()
    matching = [
        t for t in tools if _match_capabilities(t["capabilities"], req.capabilities)
    ]

    for tool in matching:
        try:
            tool["rep_score"] = get_rep(tool["agent"])
        except Exception:
            tool["rep_score"] = 0

    knowledge = _load_knowledge()
    rec_handle, rec_confidence = query_hebbian(knowledge, req.capabilities)

    agents = []
    for tool in matching:
        cost_xlm = stroops_to_xlm(tool["rate_stroops"])
        if cost_xlm > req.budget_xlm:
            continue
        agents.append(
            {
                "handle": tool["handle"],
                "public_key": tool["agent"],
                "tier": tool["model_tier"],
                "capabilities": tool["capabilities"],
                "rate_stroops": tool["rate_stroops"],
                "rate_xlm": cost_xlm,
                "rep_score": tool["rep_score"],
                "hebbian_confidence": rec_confidence
                if tool["handle"] == rec_handle
                else None,
            }
        )

    agents.sort(
        key=lambda a: (a["hebbian_confidence"] or 0, a["rep_score"]), reverse=True
    )

    return {
        "agents": agents,
        "registry_contract": os.environ.get("TOOL_REGISTRY_CONTRACT_ID", ""),
        "rep_contract": os.environ.get("REP_ASSET_CONTRACT", ""),
        "hebbian_nodes": len(knowledge.graph.nodes) if knowledge else 0,
    }


@app.post(
    "/api/hire",
    dependencies=[
        Depends(
            x402_gate(
                price_usdc="0.10",
                pay_to=X402_ESCROW_CONTRACT or "PLACEHOLDER",
                estimate_fn=_estimate_hire_cost,
            )
        )
    ],
)
@limiter.limit("5/minute")
def hire(req: HireRequest, request: Request):
    tools = list_active_full()
    tool = next((t for t in tools if t["handle"] == req.agent_handle), None)
    if not tool:
        raise HTTPException(404, f"Agent '{req.agent_handle}' not found")

    # x402 handles payment — orchestrator key for on-chain ops
    payer_kp = _load_orchestrator_kp()

    task_id = uuid.uuid4().hex
    deadline = int(time.time()) + 300

    balance_id = None
    escrow_tx = None
    if payer_kp:
        cost_xlm = stroops_to_xlm(tool["rate_stroops"])
        balance_id, escrow_tx = create_escrow(
            orchestrator_kp=payer_kp,
            agent_public=tool["agent"],
            amount_xlm=str(cost_xlm),
            deadline_unix=deadline,
        )
        import threading

        threading.Thread(
            target=lambda: submit_claim_task(
                payer_kp, task_id, req.agent_handle, deadline
            ),
            daemon=True,
        ).start()

    _save_task(
        task_id,
        {
            "task_id": task_id,
            "wallet_address": _get_wallet(request),
            "agent_handle": req.agent_handle,
            "agent_capabilities": tool["capabilities"],
            "task": req.task,
            "balance_id": balance_id,
            "escrow_tx": escrow_tx if balance_id else None,
            "deadline_unix": deadline,
            "status": "pending",
            "result": None,
        },
    )

    return {
        "task_id": task_id,
        "balance_id": balance_id,
        "escrow_tx": escrow_tx if balance_id else None,
        "deadline_unix": deadline,
    }


@app.post("/api/build-payment")
@limiter.limit("20/minute")
def build_payment(request: Request, req: BuildPaymentRequest):
    """Build an unsigned create_escrow tx for single-agent hire.

    Uses the agent's on-chain address as the escrow recipient.
    Returns task_id for tracking.
    """
    tools = list_active_full()
    tool = next((t for t in tools if t["handle"] == req.agent_handle), None)
    if not tool:
        raise HTTPException(404, f"Agent '{req.agent_handle}' not found in registry")

    from research_agent.escrow import build_escrow_tx

    task_id = uuid.uuid4().hex
    deadline_unix = int(time.time()) + 300
    amount_stroops = int(float(req.amount_usdc) * 10_000_000)

    orch_kp = _load_orchestrator_kp()
    agent_address = orch_kp.public_key if orch_kp else tool["agent"]

    try:
        result = build_escrow_tx(
            client_public=req.user_public_key,
            agent_public=agent_address,
            amount_stroops=amount_stroops,
            deadline_unix=deadline_unix,
            task_id=task_id,
        )
        return {
            "unsigned_xdr": result["unsigned_xdr"],
            "task_id": task_id,
            "deadline": deadline_unix,
            "amount_usdc": req.amount_usdc,
            "destination": X402_ESCROW_CONTRACT,
            "destination_label": f"Escrow for {req.agent_handle}",
            "token": "USDC",
        }
    except RuntimeError as e:
        raise HTTPException(500, f"Escrow tx build failed: {e}")
    except Exception as e:
        raise HTTPException(503, f"Could not build payment transaction: {str(e)}")


@app.post("/api/build-payment-generic")
@limiter.limit("20/minute")
def build_payment_generic(request: Request, req: BuildGenericPaymentRequest):
    """Build an unsigned create_escrow tx. Returns task_id for tracking.

    The user signs this with Freighter. On-chain, the escrow contract locks
    their USDC with a deadline. If execution fails/times out, refund_to_client
    can recover the funds.
    """
    from research_agent.escrow import build_escrow_tx

    task_id = f"x402-{uuid.uuid4().hex[:12]}"
    deadline_unix = int(time.time()) + req.deadline_seconds
    amount_stroops = int(float(req.amount_usdc) * 10_000_000)

    # Orchestrator address receives the funds on settlement
    orch_kp = _load_orchestrator_kp()
    agent_address = orch_kp.public_key if orch_kp else req.user_public_key

    try:
        result = build_escrow_tx(
            client_public=req.user_public_key,
            agent_public=agent_address,
            amount_stroops=amount_stroops,
            deadline_unix=deadline_unix,
            task_id=task_id,
        )
        return {
            "unsigned_xdr": result["unsigned_xdr"],
            "task_id": task_id,
            "deadline": deadline_unix,
            "amount_usdc": req.amount_usdc,
            "destination": X402_ESCROW_CONTRACT,
            "destination_label": "USDC Escrow Contract",
            "token": "USDC",
        }
    except RuntimeError as e:
        raise HTTPException(500, f"Escrow tx build failed: {e}")
    except Exception as e:
        raise HTTPException(503, f"Could not build payment transaction: {str(e)}")


@app.post("/api/submit-payment")
@limiter.limit("10/minute")
async def submit_payment(
    request: Request,
    req: SubmitPaymentRequest,
    wallet: str = Depends(_wallet_auth),
):
    """
    Submit a wallet-signed transaction to Stellar and create/link the escrow.
    """
    tool = None
    if not req.link_task_id:
        # Only look up agent for new task creation (not for linking to existing team task)
        tools = list_active_full()
        tool = next((t for t in tools if t["handle"] == req.agent_handle), None)
        if not tool:
            raise HTTPException(404, f"Agent '{req.agent_handle}' not found")

    try:
        from stellar_sdk import SorobanServer
        import stellar_sdk

        rpc_url = RPC_URL
        loop = asyncio.get_event_loop()

        def _submit_and_poll():
            server = SorobanServer(rpc_url)
            tx = stellar_sdk.TransactionEnvelope.from_xdr(
                req.signed_xdr,
                NETWORK_PASSPHRASE,
            )
            resp = server.send_transaction(tx)
            if resp.status == "ERROR":
                raise RuntimeError(f"Stellar rejected transaction: {resp.error}")
            return resp.hash

        tx_hash = await loop.run_in_executor(None, _submit_and_poll)

        # Poll for confirmation without blocking the event loop
        for _ in range(30):
            result = await loop.run_in_executor(
                None, lambda: SorobanServer(rpc_url).get_transaction(tx_hash)
            )
            if "SUCCESS" in str(result.status):
                break
            if "FAILED" in str(result.status):
                raise HTTPException(502, f"Transaction failed on Stellar: {tx_hash}")
            await asyncio.sleep(2)

    except HTTPException:
        raise
    except Exception as e:
        raise HTTPException(502, f"Failed to submit transaction: {str(e)}")

    # Use task_id from build-payment (matches the on-chain escrow entry)
    task_id = req.task_id or f"hire-{req.agent_handle}-{int(time.time())}"
    deadline = int(time.time()) + 300

    orch_kp = _load_orchestrator_kp()
    if orch_kp:
        try:
            submit_claim_task(orch_kp, task_id, req.agent_handle, deadline)
        except Exception:
            pass  # best-effort

    wallet_addr = wallet

    if req.link_task_id:
        existing = _load_task(req.link_task_id, wallet_address=wallet)
        if not existing:
            raise HTTPException(
                404,
                "Task not found or you don't own it",
            )
        _store.update_task(req.link_task_id, escrow_tx=tx_hash)
        return {
            "task_id": req.link_task_id,
            "escrow_tx": tx_hash,
            "deadline_unix": existing.get("deadline_unix", deadline),
        }

    _save_task(
        task_id,
        {
            "task_id": task_id,
            "wallet_address": wallet_addr,
            "agent_handle": req.agent_handle,
            "agent_capabilities": tool["capabilities"],
            "task": req.task,
            "balance_id": None,
            "escrow_tx": tx_hash,
            "deadline_unix": deadline,
            "status": "pending",
            "result": None,
        },
    )

    return {
        "task_id": task_id,
        "escrow_tx": tx_hash,
        "deadline_unix": deadline,
    }


@app.get("/api/execute/{task_id}")
async def execute(task_id: str, wallet: str = Depends(_wallet_auth)):
    task = _load_task(task_id, wallet_address=wallet)
    if not task:
        raise HTTPException(404, "Task not found or access denied")

    skill_id = task.get("agent_handle") or None
    is_debate = skill_id == "adr-debate"

    async def event_stream():
        import json

        def send(event_type: str, message: str, data: dict | None = None):
            event = {
                "type": event_type,
                "timestamp": time.strftime("%H:%M:%S"),
                "message": message,
            }
            if data:
                event["data"] = data
            return f"data: {json.dumps(event)}\n\n"

        yield send("log", "Sending to A2A")

        _update_task(task_id, status="running")

        try:
            if is_debate:
                # Streaming path: forward progress events in real-time via queue
                from research_agent.mcp_client import stream_a2a_message
                import queue

                loop = asyncio.get_event_loop()
                q: queue.Queue = queue.Queue()
                _DONE = object()

                def _stream():
                    try:
                        for evt in stream_a2a_message(
                            task["task"],
                            context_id=task_id,
                            skill_id=skill_id,
                        ):
                            q.put(evt)
                    except Exception as exc:
                        q.put({"event": "error", "data": {"error": str(exc)}})
                    finally:
                        q.put(_DONE)

                loop.run_in_executor(None, _stream)

                final_result = None
                while True:
                    evt = await loop.run_in_executor(None, q.get)
                    if evt is _DONE:
                        break
                    if evt["event"] == "progress":
                        yield send("progress", "", evt["data"])
                    elif evt["event"] == "result":
                        final_result = evt["data"]
                    elif evt["event"] == "error":
                        _update_task(task_id, status="failed")
                        yield send("error", str(evt["data"]))
                        return

                if final_result:
                    output_parts = []
                    for artifact in final_result.get("artifacts", []):
                        for part in artifact.get("parts", []):
                            if part.get("text"):
                                output_parts.append(part["text"])
                    output = "\n\n".join(output_parts) if output_parts else None

                    if output:
                        _update_task(
                            task_id,
                            status="complete",
                            context_id=final_result.get("contextId", task_id),
                            result={"final_response": output, "called_tools": ["a2a"]},
                        )
                        yield send("complete", "Execution complete")
                    else:
                        _update_task(task_id, status="failed")
                        yield send("error", "A2A returned no output")
                else:
                    _update_task(task_id, status="failed")
                    yield send("error", "No result from streaming A2A")
            else:
                # Non-streaming path (research, single agent)
                loop = asyncio.get_event_loop()
                a2a_result = await loop.run_in_executor(
                    None,
                    lambda: send_a2a_message(
                        task["task"],
                        context_id=task_id,
                        skill_id=skill_id,
                    ),
                )

                yield send("tool_call", f"A2A task: {a2a_result.get('task_id', '')}")

                if a2a_result.get("output"):
                    _update_task(
                        task_id,
                        status="complete",
                        context_id=a2a_result.get("context_id", task_id),
                        result={
                            "final_response": a2a_result["output"],
                            "called_tools": ["a2a"],
                        },
                    )
                    yield send(
                        "complete",
                        "Execution complete",
                        {
                            "output_preview": a2a_result["output"][:200],
                        },
                    )
                else:
                    _update_task(task_id, status="failed")
                    yield send(
                        "error",
                        f"A2A returned no output (status: {a2a_result.get('status')})",
                    )
        except Exception as exc:
            import traceback

            err_detail = f"{type(exc).__name__}: {exc}"
            print(f"[EXECUTE ERROR] task={task_id} error={err_detail}", flush=True)
            traceback.print_exc()
            _update_task(task_id, status="failed")
            yield send("error", err_detail)

    return StreamingResponse(event_stream(), media_type="text/event-stream")


@app.get("/api/result/{task_id}")
def get_result(task_id: str, wallet: str = Depends(_wallet_auth)):
    task = _load_task(task_id, wallet_address=wallet)
    if not task:
        raise HTTPException(404, "Task not found or access denied")
    if task["status"] != "complete" or not task.get("result"):
        return {"status": task["status"], "output": None}

    result = task["result"]
    final_response = (
        result["final_response"] if isinstance(result, dict) else result.final_response
    )
    called_tools = (
        result.get("called_tools", [])
        if isinstance(result, dict)
        else result.called_tools
    )
    prompt_tokens = len(task["task"].split())
    output_tokens = len(final_response.split())

    input_tokens = prompt_tokens
    if prompt_tokens < 100:
        max_ratio = 0
        compliant = True
    else:
        max_ratio = 200
        compliant = False
        try:
            compliant = verify_compression(
                task_id, input_tokens, output_tokens, max_ratio_pct=max_ratio
            )
        except Exception:
            pass

    # Reinforce Hebbian on success — only once per task
    if compliant and not task.get("hebbian_reinforced"):
        knowledge = _load_knowledge()
        caps = task.get("agent_capabilities", [])
        capability_key = " ".join(caps)
        reinforce_hebbian(knowledge, capability_key, task["agent_handle"])
        store_output(
            knowledge, task["task"], task["agent_handle"], final_response, caps
        )
        _update_task(task_id, hebbian_reinforced=True)

    # Query mcp-meta for actual cost and settle if compliant
    settlement = None
    if compliant and task.get("escrow_tx") and not task.get("settled"):
        try:
            import httpx
            from research_agent.mcp_client import _get_mcp_url, _get_mcp_headers

            mcp_url = _get_mcp_url()
            context_id = task.get("context_id", task_id)
            cost_resp = httpx.get(
                f"{mcp_url}/v1/cost/{context_id}",
                headers=_get_mcp_headers(),
                timeout=5.0,
            )

            if cost_resp.status_code == 200:
                cost_data = cost_resp.json()
                actual_stroops = cost_data.get("total_stroops", 0)

                # Look up agent rate from registry as fallback if mcp-meta tracked 0
                if actual_stroops == 0:
                    tools = list_active_full()
                    tool = next(
                        (t for t in tools if t["handle"] == task["agent_handle"]), None
                    )
                    if tool:
                        num_calls = len(called_tools)
                        actual_stroops = tool["rate_stroops"] * max(num_calls, 1)

                orch_kp = _load_orchestrator_kp()
                if orch_kp and actual_stroops > 0:
                    settle_tx = settle_escrow(orch_kp, task_id, actual_stroops)
                    _update_task(task_id, settled=1)
                    task["settled"] = 1  # keep local dict in sync for response
                    settlement = {
                        "settle_tx": settle_tx,
                        "actual_cost_stroops": actual_stroops,
                        "actual_cost_usdc": f"{actual_stroops / 10_000_000:.7f}",
                        "cost_breakdown": cost_data.get("entries", []),
                        "by_agent": cost_data.get("by_agent", {}),
                    }
        except Exception as settle_err:
            import logging

            logging.error(f"Settlement failed for {task_id}: {settle_err}")

    return {
        "status": task["status"],
        "output": _format_output(final_response),
        "input_tokens": input_tokens,
        "output_tokens": output_tokens,
        "compliant": compliant,
        "max_ratio": max_ratio,
        "balance_id": task.get("balance_id"),
        "escrow_tx": task.get("escrow_tx"),
        "settlement": settlement,
        "settlement_status": (
            "settled"
            if task.get("settled")
            else "ready_for_claim"
            if compliant
            and task.get("escrow_tx")
            and time.time() < task.get("deadline_unix", 0)
            else "expired"
            if task.get("escrow_tx") and time.time() >= task.get("deadline_unix", 0)
            else "pending"
        ),
        "deadline_unix": task.get("deadline_unix"),
    }


@app.post(
    "/api/debate",
    dependencies=[
        Depends(
            x402_gate(
                price_usdc="0.25",
                pay_to=X402_ESCROW_CONTRACT or "PLACEHOLDER",
                estimate_fn=_estimate_debate_cost,
            )
        )
    ],
)
@limiter.limit("3/minute")
def debate(req: DebateRequest, request: Request):
    """Run an architecture debate via mcp-meta. x402 gated."""
    escrow_task_id = getattr(request.state, "escrow_task_id", "") if request else ""
    try:
        result = execute_tool(
            "adr-moderator.debate",
            {"question": req.question},
            timeout=180.0,
        )
    except (RuntimeError, Exception) as e:
        # Attempt refund on failure
        if escrow_task_id:
            try:
                orch_kp = _load_orchestrator_kp()
                if orch_kp:
                    from research_agent.escrow import refund_escrow

                    refund_tx = refund_escrow(orch_kp, escrow_task_id)
                    raise HTTPException(
                        502,
                        detail={
                            "error": f"Debate execution failed: {e}",
                            "refund_tx": refund_tx,
                            "escrow_task_id": escrow_task_id,
                        },
                    )
            except HTTPException:
                raise
            except Exception:
                pass  # Refund failed too — sweeper will catch it
        raise HTTPException(502, f"Debate execution failed: {e}")

    # Settle escrow on success
    settlement = None
    if escrow_task_id:
        try:
            orch_kp = _load_orchestrator_kp()
            if orch_kp:
                # Use flat rate for debate settlement
                actual_stroops = 2500000  # 0.25 USDC
                settle_tx = settle_escrow(orch_kp, escrow_task_id, actual_stroops)
                settlement = {
                    "settle_tx": settle_tx,
                    "actual_cost_usdc": f"{actual_stroops / 10_000_000:.7f}",
                    "escrow_task_id": escrow_task_id,
                }
        except Exception as e:
            import logging

            logging.error(f"Debate settlement failed for {escrow_task_id}: {e}")

    if isinstance(result, dict):
        result["settlement"] = settlement
        result["settlement_status"] = "settled" if settlement else "pending"
    return result


@app.post(
    "/api/execute-a2a",
    dependencies=[
        Depends(
            x402_gate(
                price_usdc="0.10",
                pay_to=X402_ESCROW_CONTRACT or "PLACEHOLDER",
                estimate_fn=_estimate_a2a_cost,
            )
        )
    ],
)
@limiter.limit("5/minute")
def execute_a2a(req: ExecuteA2ARequest, request: Request):
    """Generic x402-gated A2A execution. Returns formatted markdown output."""
    escrow_task_id = getattr(request.state, "escrow_task_id", "") if request else ""

    def _try_refund(error_msg: str):
        if not escrow_task_id:
            raise HTTPException(502, error_msg)
        try:
            orch_kp = _load_orchestrator_kp()
            if orch_kp:
                from research_agent.escrow import refund_escrow

                refund_tx = refund_escrow(orch_kp, escrow_task_id)
                raise HTTPException(
                    502,
                    detail={
                        "error": error_msg,
                        "refund_tx": refund_tx,
                        "escrow_task_id": escrow_task_id,
                    },
                )
        except HTTPException:
            raise
        except Exception:
            pass
        raise HTTPException(502, error_msg)

    a2a_result = {}
    try:
        a2a_result = send_a2a_message(
            req.task,
            context_id=req.context_id,
            timeout=120.0,
        )
    except (RuntimeError, Exception) as e:
        _try_refund(f"A2A execution failed: {e}")

    raw_output = a2a_result.get("output")
    if not raw_output:
        _try_refund(f"A2A returned no output (status: {a2a_result.get('status')})")

    # Settle escrow on success
    settlement = None
    if escrow_task_id:
        try:
            orch_kp = _load_orchestrator_kp()
            if orch_kp:
                actual_stroops = 1000000  # 0.10 USDC base rate for A2A
                settle_tx = settle_escrow(orch_kp, escrow_task_id, actual_stroops)
                settlement = {
                    "settle_tx": settle_tx,
                    "actual_cost_usdc": f"{actual_stroops / 10_000_000:.7f}",
                    "escrow_task_id": escrow_task_id,
                }
        except Exception as e:
            import logging

            logging.error(f"A2A settlement failed for {escrow_task_id}: {e}")

    return {
        "output": _format_output(raw_output),
        "status": a2a_result.get("status", "completed"),
        "task_id": a2a_result.get("task_id", ""),
        "context_id": a2a_result.get("context_id"),
        "escrow_task_id": escrow_task_id,
        "settlement": settlement,
        "settlement_status": "settled" if settlement else "pending",
    }


@app.get("/api/escrow/{task_id}")
def escrow_status(task_id: str):
    """Get on-chain escrow status for a task."""
    from research_agent.escrow import get_escrow_status

    result = get_escrow_status(task_id)

    # Enrich with stored task info if available
    task = _load_task(task_id)
    if task:
        result["server_status"] = task.get("status")
        result["settled"] = bool(task.get("settled", False))
        result["refunded"] = bool(task.get("refunded", False))
        result["deadline_unix"] = task.get("deadline_unix")

    return result


class LinkEscrowRequest(BaseModel):
    task_id: str
    escrow_tx: str
    wallet_address: str = ""


@app.post("/api/link-escrow")
def link_escrow(req: LinkEscrowRequest, wallet: str = Depends(_wallet_auth)):
    """Link an escrow transaction to an existing task.

    Called after the frontend submits a payment TX directly to Horizon.
    Updates the task's escrow_tx and wallet_address so settlement can work.
    """
    task = _load_task(req.task_id, wallet_address=wallet)
    if not task:
        raise HTTPException(404, "Task not found or access denied")

    _store.update_task(
        req.task_id,
        escrow_tx=req.escrow_tx,
    )
    # Also update wallet_address if not already set
    if not task.get("wallet_address"):
        try:
            _store._execute(
                "UPDATE tasks SET wallet_address = ? WHERE task_id = ?",
                (req.wallet_address or wallet, req.task_id),
            )
            _store._commit()
        except Exception:
            pass

    return {"linked": True, "task_id": req.task_id, "escrow_tx": req.escrow_tx}


@app.get("/api/auth/challenge")
def auth_challenge():
    """Generate a challenge nonce for wallet-signature authentication."""
    nonce = uuid.uuid4().hex
    return {"challenge": nonce, "expires_in": 300}


@app.get("/api/auth/challenge-tx")
def auth_challenge_tx(public_key: str = ""):
    """Generate a challenge as an unsigned Stellar transaction (for Freighter wallets).

    Args:
        public_key: The user's Stellar public key (G...). Used as tx source
            so Freighter shows a valid account. Optional — falls back to a
            dummy account for testing.

    Returns unsigned XDR with the challenge nonce as memo + manage_data op.
    Freighter signs this, and the signed XDR is sent back in X-Wallet-Auth.
    This transaction is NEVER submitted to the network.
    """
    from stellar_sdk import TransactionBuilder, Account, SorobanServer

    nonce = uuid.uuid4().hex[:28]  # 28 chars max for Stellar memo text

    # Use the user's real account if provided (Freighter-friendly on mainnet)
    if public_key and public_key.startswith("G") and len(public_key) == 56:
        try:
            server = SorobanServer(RPC_URL)
            source_account = server.load_account(public_key)
        except Exception:
            # Account not found or RPC error — use dummy
            source_account = Account(public_key, 0)
    else:
        source_account = Account(
            "GAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAWHF", 0
        )

    builder = TransactionBuilder(
        source_account=source_account,
        network_passphrase=NETWORK_PASSPHRASE,
        base_fee=100,
    )
    # Freighter requires at least one operation — manage_data as a no-op
    builder.append_manage_data_op("auth_challenge", nonce)
    builder.add_text_memo(nonce)
    builder.set_timeout(300)
    tx = builder.build()

    return {"challenge": nonce, "unsigned_xdr": tx.to_xdr(), "expires_in": 300}


class SessionTokenRequest(BaseModel):
    publicKey: str
    challenge: str
    signature: str = ""
    signedXdr: str = ""


@app.post("/api/auth/session")
@limiter.limit("30/hour")
def exchange_session_token(request: Request, req: SessionTokenRequest):
    """Exchange a signed challenge for a session JWT.

    The wallet signs ONE challenge (via /api/auth/challenge-tx) and POSTs the
    signed payload here. Server verifies the signature, consumes the nonce
    (single-use), and returns a JWT valid for 1 hour.

    Rate-limited at 30/hour per IP — enough headroom for token refresh on
    expiry, low enough to make signed-challenge replay attempts cheap to
    rate-limit.
    """
    _validate_signed_challenge(
        req.publicKey,
        req.challenge,
        req.signature,
        req.signedXdr,
    )
    _store.claim_nonce(f"auth-challenge-2-{req.challenge}")
    from research_agent import session_tokens

    token, expires_in = session_tokens.issue(req.publicKey)
    return {"token": token, "expires_in": expires_in, "token_type": "Bearer"}


@app.get("/api/result/{task_id}/auth")
def get_result_auth(task_id: str, wallet: str = Depends(_wallet_auth)):
    """Get task result with wallet authentication. Only the task owner can access."""
    task = _load_task(task_id, wallet_address=wallet)
    if not task:
        raise HTTPException(404, "Task not found or access denied")

    # Same response format as /api/result/{task_id} but enforced by wallet
    result_data = task.get("result")
    if task["status"] != "complete" or not result_data:
        return {"status": task["status"], "output": None}

    output = (
        result_data.get("final_response", "")
        if isinstance(result_data, dict)
        else str(result_data)
    )
    return {
        "status": task["status"],
        "output": _format_output(output) if output else None,
        "settlement_status": "settled" if task.get("settled") else "pending",
    }


class FundRequest(BaseModel):
    public_key: str


@app.post("/api/fund")
@limiter.limit("5/minute")
def fund_account(request: Request, req: FundRequest):
    from research_agent.stellar_config import FRIENDBOT_URL

    if not FRIENDBOT_URL:
        raise HTTPException(
            400,
            "Auto-funding is unavailable on this network. "
            "Fund the account externally (exchange withdrawal or another funded account).",
        )
    funded = fund_testnet(req.public_key)
    if not funded:
        raise HTTPException(400, "Friendbot funding failed")
    return {"funded": True, "public_key": req.public_key}


class PrepareWalletRequest(BaseModel):
    public_key: str


_MINT_COOLDOWN_SECONDS = int(
    os.environ.get("PREPARE_WALLET_MINT_COOLDOWN_SECONDS", "3600")
)


@app.post("/api/prepare-wallet")
@limiter.limit("3/hour")
def prepare_wallet(
    request: Request,
    req: PrepareWalletRequest,
    wallet: str = Depends(_wallet_auth),
):
    """Check if wallet has USDC trustline, return trustline tx if missing.

    The frontend calls this before payment. If the user needs a trustline,
    it returns an unsigned tx for Freighter to sign. Also mints test USDC
    on testnet if the balance is zero.
    """
    if STELLAR_NETWORK == "public":
        raise HTTPException(
            403,
            "Mainnet does not provide test USDC mint. Acquire real USDC externally.",
        )

    if req.public_key != wallet:
        raise HTTPException(
            403,
            "X-Wallet-Auth wallet does not match the public_key in the request body.",
        )

    last_mint = _store.last_mint_at(req.public_key)
    if last_mint is not None:
        elapsed = time.time() - last_mint
        if elapsed < _MINT_COOLDOWN_SECONDS:
            wait = int(_MINT_COOLDOWN_SECONDS - elapsed)
            raise HTTPException(
                429,
                f"Mint cooldown active for this wallet. Retry in {wait}s.",
            )

    from research_agent.x402_config import X402_USDC_ADDRESS
    from stellar_sdk import SorobanServer, scval

    rpc_url = RPC_URL

    # Check USDC balance via SAC
    try:
        from research_agent.soroban_client import _simulate_invoke

        result_xdr = _simulate_invoke(
            rpc_url,
            X402_USDC_ADDRESS,
            "balance",
            [scval.to_address(req.public_key)],
        )
        balance = int(scval.to_native(result_xdr))
        has_trustline = True
    except Exception:
        balance = 0
        has_trustline = False

    # If no trustline, build the trustline tx
    trustline_xdr = None
    if not has_trustline:
        orch_kp = _load_orchestrator_kp()
        if orch_kp:
            from stellar_sdk import Asset as StellarAsset, TransactionBuilder as TB

            server_horizon = Server(HORIZON_URL)
            account = server_horizon.load_account(req.public_key)
            builder = TB(
                source_account=account,
                network_passphrase=NETWORK_PASSPHRASE,
                base_fee=100,
            )
            builder.append_change_trust_op(
                asset=StellarAsset("USDC", orch_kp.public_key),
            )
            builder.set_timeout(300)
            tx = builder.build()
            trustline_xdr = tx.to_xdr()

    # If trustline exists but balance is 0, mint test USDC
    mint_tx = None
    mint_error = None
    if has_trustline and balance == 0:
        orch_kp = _load_orchestrator_kp()
        if not orch_kp:
            mint_error = "ORCHESTRATOR_SECRET_KEY not configured"
            logger.error(
                "prepare_wallet mint failed: orchestrator key not loaded "
                "(env ORCHESTRATOR_SECRET_KEY missing or empty); "
                "wallet=%s",
                req.public_key,
            )
        else:
            try:
                from stellar_sdk import TransactionBuilder

                soroban = SorobanServer(rpc_url)
                source = soroban.load_account(orch_kp.public_key)
                builder = TransactionBuilder(
                    source_account=source,
                    network_passphrase=NETWORK_PASSPHRASE,
                    base_fee=100,
                )
                builder.append_invoke_contract_function_op(
                    contract_id=X402_USDC_ADDRESS,
                    function_name="mint",
                    parameters=[
                        scval.to_address(req.public_key),
                        scval.to_int128(10_000_000_000),  # 1000 USDC
                    ],
                )
                builder.set_timeout(30)
                tx = builder.build()
                sim = soroban.simulate_transaction(tx)
                if sim.error:
                    mint_error = f"simulation error: {sim.error}"
                    logger.error(
                        "prepare_wallet mint simulation failed: wallet=%s error=%s",
                        req.public_key,
                        sim.error,
                    )
                else:
                    tx = soroban.prepare_transaction(tx, sim)
                    tx.sign(orch_kp)
                    resp = soroban.send_transaction(tx)
                    mint_tx = resp.hash
                    balance = 10_000_000_000
                    _store.record_mint(req.public_key)
            except Exception as exc:
                mint_error = f"{type(exc).__name__}: {exc}"
                logger.exception(
                    "prepare_wallet mint failed: wallet=%s",
                    req.public_key,
                )

    return {
        "public_key": req.public_key,
        "has_trustline": has_trustline,
        "usdc_balance": balance,
        "usdc_balance_display": f"{balance / 10_000_000:.2f}" if balance else "0",
        "trustline_xdr": trustline_xdr,
        "mint_tx": mint_tx,
        "mint_error": mint_error,
        "usdc_contract": X402_USDC_ADDRESS,
    }


@app.post("/api/build-deactivate")
def build_deactivate(req: BuildDeactivateRequest, wallet: str = Depends(_wallet_auth)):
    """Build an unsigned deactivate_tool tx for the agent to sign.

    Requires wallet auth to verify the caller owns the agent's keypair.
    Returns unsigned XDR for client-side signing (Freighter).
    """
    from research_agent.soroban_client import build_deactivate_tx

    tools = list_active_full()
    tool = next((t for t in tools if t["handle"] == req.handle), None)
    if not tool:
        raise HTTPException(404, f"Agent '{req.handle}' not found in registry")

    # Verify the caller is the agent owner
    if tool["agent"] != wallet:
        raise HTTPException(403, "Only the agent owner can deactivate")

    try:
        result = build_deactivate_tx(
            agent_public=wallet,
            handle=req.handle,
        )
        return result
    except RuntimeError as e:
        raise HTTPException(500, f"Deactivate tx build failed: {e}")
    except Exception as e:
        raise HTTPException(503, f"Could not build deactivate transaction: {str(e)}")


@app.post("/api/submit-deactivate")
def submit_deactivate(
    req: SubmitDeactivateRequest, wallet: str = Depends(_wallet_auth)
):
    """Submit a wallet-signed deactivate_tool transaction to Stellar.
    Without these checks the endpoint accepts any signed XDR with a
    presence-only `X-Wallet-Auth` header — DoS griefing of registered
    agents by spamming garbage submissions that burn orchestrator RPC
    budget.
    """
    # Registry ownership check
    tools = list_active_full()
    tool = next((t for t in tools if t["handle"] == req.handle), None)
    if not tool:
        raise HTTPException(404, f"Agent '{req.handle}' not found in registry")
    if tool["agent"] != wallet:
        raise HTTPException(403, "Only the agent owner can submit deactivate")

    try:
        from stellar_sdk import SorobanServer
        import stellar_sdk

        server = SorobanServer(RPC_URL)

        try:
            tx = stellar_sdk.TransactionEnvelope.from_xdr(
                req.signed_xdr,
                NETWORK_PASSPHRASE,
            )
        except Exception:
            raise HTTPException(400, "Malformed signed_xdr")

        # Source-account binding: the signed XDR must come FROM the
        # authenticated wallet. Stops a caller from submitting a different
        # agent's signed XDR even if step 1 returns the matching tool entry
        # (e.g., cache-race scenarios).
        try:
            source_addr = tx.transaction.source.account_id
        except AttributeError:
            source_addr = str(tx.transaction.source)
        if source_addr != wallet:
            raise HTTPException(
                403,
                "signed_xdr source account does not match authenticated wallet",
            )

        resp = server.send_transaction(tx)
        if resp.status == "ERROR":
            raise HTTPException(502, f"Stellar rejected transaction: {resp.error}")

        tx_hash = resp.hash

        for _ in range(15):
            result = server.get_transaction(tx_hash)
            if "SUCCESS" in str(result.status):
                invalidate_registry_cache()
                return {"tx_hash": tx_hash, "handle": req.handle}
            if "FAILED" in str(result.status):
                raise HTTPException(502, f"Transaction failed on Stellar: {tx_hash}")
            time.sleep(2)

        return {"tx_hash": tx_hash, "handle": req.handle, "status": "pending"}

    except HTTPException:
        raise
    except Exception as e:
        raise HTTPException(502, f"Failed to submit deactivate transaction: {str(e)}")


@app.get("/api/hebbian")
def hebbian_status():
    """Return Hebbian knowledge graph stats and recent findings."""
    knowledge = _load_knowledge()
    if not knowledge:
        return {"nodes": 0, "edges": 0, "recent": []}

    nodes_count = len(knowledge.graph.nodes)
    edges_count = len(list(knowledge.graph.edges))

    # Get agent-sourced association nodes
    agent_findings = []
    # Get recently extracted knowledge nodes (from ingest)
    extracted_facts = []

    for _, node in knowledge.graph.nodes.items():
        source = node.metadata.get("source_url", "")
        if source.startswith("agent:"):
            agent_findings.append(
                {
                    "id": node.id,
                    "content": (node.content or node.label)[:200],
                    "source": source,
                    "domain": node.metadata.get("domain", ""),
                    "importance": node.importance,
                    "type": "association",
                }
            )
        elif node.metadata.get("domain") and node.importance >= 0.5:
            # Extracted fact nodes have domain set by the extractor
            label = node.label or ""
            content = node.content or node.summary or label
            if content and len(content) > 10:
                extracted_facts.append(
                    {
                        "id": node.id,
                        "content": content[:200],
                        "label": label[:80],
                        "importance": node.importance,
                        "type": "fact",
                    }
                )

    agent_findings.sort(key=lambda r: r["importance"], reverse=True)
    extracted_facts.sort(key=lambda r: r["importance"], reverse=True)

    # Personality introspection (zero LLM calls)
    personality = None
    if nodes_count >= 5:
        try:
            from research_agent.hebbian.introspection import IntrospectionEngine
            from research_agent.hebbian.personality import PersonalitySynthesizer

            engine = IntrospectionEngine(knowledge.graph)
            model = engine.compute_self_model()
            synth = PersonalitySynthesizer()
            snapshot = synth.synthesize(model)
            personality = {
                "description": snapshot.description,
                "strengths": snapshot.strengths,
                "growth_areas": snapshot.growth_areas,
                "reasoning_style": snapshot.reasoning_style,
            }
        except Exception:
            pass

    return {
        "nodes": nodes_count,
        "edges": edges_count,
        "agent_findings": agent_findings[:5],
        "extracted_facts": extracted_facts[:10],
        "personality": personality,
    }


@app.get("/api/hebbian/graph")
def hebbian_graph():
    """Return full graph structure for visualization."""
    knowledge = _load_knowledge()
    if not knowledge:
        return {"nodes": [], "edges": []}

    nodes = []
    for node_id, node in knowledge.graph.nodes.items():
        source = node.metadata.get("source_url", "")
        agent_source = (
            source.replace("agent:", "") if source.startswith("agent:") else None
        )
        nodes.append(
            {
                "id": node_id,
                "label": (node.label or node_id)[:60],
                "importance": node.importance,
                "type": getattr(node, "node_type", "unknown"),
                "agent_source": agent_source,
            }
        )

    edges = []
    for edge in knowledge.graph.edges:
        if edge.weight > 0.05:  # skip near-zero edges for performance
            edges.append(
                {
                    "source": edge.source_id,
                    "target": edge.target_id,
                    "weight": round(edge.weight, 3),
                }
            )

    return {"nodes": nodes, "edges": edges}


@app.get("/api/registry")
def registry():
    tools = list_active_full()
    agents = []
    for tool in tools:
        rep = 0
        try:
            rep = get_rep(tool["agent"])
        except Exception:
            pass
        agents.append(
            {
                "handle": tool["handle"],
                "public_key": tool["agent"],
                "tier": tool["model_tier"],
                "capabilities": tool["capabilities"],
                "rate_stroops": tool["rate_stroops"],
                "rate_xlm": stroops_to_xlm(tool["rate_stroops"]),
                "rep_score": rep,
            }
        )
    return {"agents": agents}


@app.get("/api/agent/{public_key}")
def agent_detail(public_key: str):
    server = Server(HORIZON_URL)

    try:
        account = server.accounts().account_id(public_key).call()
    except Exception as exc:
        raise HTTPException(404, f"Account not found: {exc}")

    # Claimable balances
    try:
        cb_resp = server.claimable_balances().for_claimant(public_key).call()
        claimable_balances = cb_resp["_embedded"]["records"]
    except Exception:
        claimable_balances = []

    # Parse manage_data
    manifest = read_capability_manifest(public_key)

    return {
        "public_key": public_key,
        "manifest": manifest,
        "balances": account.get("balances", []),
        "claimable_balances": claimable_balances,
        "horizon_data": account.get("data", {}),
    }


_MAX_TEAM_AGENTS = int(os.environ.get("MAX_TEAM_AGENTS", "10"))
_MAX_TEAM_CAPABILITIES = int(os.environ.get("MAX_TEAM_CAPABILITIES", "10"))
_MAX_TEAM_ESTIMATED_USDC = float(os.environ.get("MAX_TEAM_ESTIMATED_USDC", "5.00"))


def _estimate_team_cost(body: dict) -> dict:
    """
    Estimate cost for a team hire based on registry rates.
    """
    capabilities = body.get("capabilities", [])
    if len(capabilities) > _MAX_TEAM_CAPABILITIES:
        raise HTTPException(
            413,
            f"Too many capabilities: {len(capabilities)} > {_MAX_TEAM_CAPABILITIES} cap. ",
        )
    try:
        tools = list_active_full()
        for tool in tools:
            try:
                tool["rep_score"] = get_rep(tool["agent"])
            except Exception:
                tool["rep_score"] = 0

        team = compose_team(capabilities, tools)
        if len(team) > _MAX_TEAM_AGENTS:
            raise HTTPException(
                413, f"Team too large: {len(team)} agents > {_MAX_TEAM_AGENTS} cap. "
            )
        sub_tasks = decompose_task(body.get("task", ""), team)
        estimated, _ = estimate_team_cost(sub_tasks, team)
        # Cap the raw estimate so a malicious capability list + large
        # rate_stroops can't push the ceiling unboundedly.
        capped_estimate = min(estimated, _MAX_TEAM_ESTIMATED_USDC)

        rate_breakdown = [
            {
                "agent": a.handle,
                "capabilities": a.capabilities,
                "rate": str(stroops_to_xlm(a.rate_stroops)),
            }
            for a in team
        ]
        return {
            "estimated_cost": str(round(capped_estimate, 7)),
            "rate_breakdown": rate_breakdown,
        }
    except HTTPException:
        raise
    except Exception:
        return {"estimated_cost": "0.30", "rate_breakdown": []}


@app.post(
    "/api/hire-team",
    dependencies=[
        Depends(
            x402_gate(
                price_usdc="0.30",
                pay_to=X402_ESCROW_CONTRACT or "PLACEHOLDER",
                estimate_fn=_estimate_team_cost,
            )
        )
    ],
)
@limiter.limit("3/minute")
def hire_team(req: HireTeamRequest, request: Request):
    """Hire a team of agents for a task. Returns team composition + sub-tasks.

    x402-gated: same payment flow as /api/hire.
    First call → 402 with price. Client pays, retries with PAYMENT-SIGNATURE.
    Orchestrator creates escrow after payment is verified.
    """
    if len(req.capabilities) > _MAX_TEAM_CAPABILITIES:
        raise HTTPException(
            413,
            f"Too many capabilities: {len(req.capabilities)} > {_MAX_TEAM_CAPABILITIES} cap. ",
        )

    tools = list_active_full()
    for tool in tools:
        try:
            tool["rep_score"] = get_rep(tool["agent"])
        except Exception:
            tool["rep_score"] = 0

    team = compose_team(req.capabilities, tools)
    if not team:
        raise HTTPException(
            404, f"No agents found for capabilities: {req.capabilities}"
        )
    if len(team) > _MAX_TEAM_AGENTS:
        raise HTTPException(
            413,
            f"Team too large: {len(team)} agents > {_MAX_TEAM_AGENTS} cap. "
            f"Override via MAX_TEAM_AGENTS env if you need a higher limit.",
        )

    sub_tasks = decompose_task(req.task, team)
    estimated, ceiling = estimate_team_cost(sub_tasks, team)
    execution_plan = build_execution_plan(sub_tasks)

    task_id = uuid.uuid4().hex
    deadline = int(time.time()) + 600  # 10 min for team tasks

    # Create escrow (same as single-agent hire)
    payer_kp = _load_orchestrator_kp()
    escrow_tx = None
    if payer_kp:
        try:
            _, escrow_tx = create_escrow(
                orchestrator_kp=payer_kp,
                agent_public=team[0].public_key,
                amount_xlm=str(ceiling),
                deadline_unix=deadline,
            )
        except Exception:
            pass
        # Best-effort claim_task in background
        import threading

        threading.Thread(
            target=lambda: submit_claim_task(
                payer_kp, task_id, team[0].handle, deadline
            ),
            daemon=True,
        ).start()

    _save_task(
        task_id,
        {
            "task_id": task_id,
            "wallet_address": _get_wallet(request),
            "task": req.task,
            "agent_handle": team[0].handle if team else "",
            "team": [
                {
                    "handle": a.handle,
                    "public_key": a.public_key,
                    "capabilities": a.capabilities,
                }
                for a in team
            ],
            "sub_tasks": [
                {
                    "id": st.id,
                    "agent_handle": st.agent_handle,
                    "action": st.action,
                    "depends_on": st.depends_on,
                }
                for st in sub_tasks
            ],
            "execution_plan": [
                [st.id for st in round_tasks] for round_tasks in execution_plan
            ],
            "escrow_tx": escrow_tx,
            "deadline_unix": deadline,
            "estimated_cost": estimated,
            "budget_ceiling": ceiling,
            "status": "pending",
            "result": None,
            "is_team": True,
        },
    )

    return {
        "task_id": task_id,
        "team": [
            {
                "handle": a.handle,
                "capabilities": a.capabilities,
                "rate_xlm": stroops_to_xlm(a.rate_stroops),
                "rep_score": a.rep_score,
            }
            for a in team
        ],
        "sub_tasks": [
            {
                "id": st.id,
                "agent_handle": st.agent_handle,
                "action": st.action,
                "depends_on": st.depends_on,
            }
            for st in sub_tasks
        ],
        "execution_plan": [
            [st.id for st in round_tasks] for round_tasks in execution_plan
        ],
        "estimated_cost_usdc": str(round(estimated, 7)),
        "budget_ceiling_usdc": str(round(ceiling, 7)),
        "deadline_unix": deadline,
    }


@app.get("/api/execute-team/{task_id}")
async def execute_team(task_id: str, wallet: str = Depends(_wallet_auth)):
    """Execute a team task: run sub-tasks in rounds, stream per-agent progress."""
    task = _load_task(task_id, wallet_address=wallet)
    if not task or not task.get("is_team"):
        raise HTTPException(404, "Team task not found or access denied")

    async def event_stream():
        import json as _json

        def send(event_type: str, message: str, data: dict | None = None):
            event = {
                "type": event_type,
                "timestamp": time.strftime("%H:%M:%S"),
                "message": message,
            }
            if data:
                event["data"] = data
            return f"data: {_json.dumps(event)}\n\n"

        yield send(
            "log", f"Team execution started — {len(task['team'])} agents via A2A"
        )
        _update_task(task_id, status="running")

        try:
            all_results = {}
            loop = asyncio.get_event_loop()

            for round_idx, round_task_ids in enumerate(task["execution_plan"]):
                round_sub_tasks = [
                    st for st in task["sub_tasks"] if st["id"] in round_task_ids
                ]
                yield send(
                    "log", f"Round {round_idx + 1}: {len(round_sub_tasks)} sub-tasks"
                )

                for st in round_sub_tasks:
                    yield send(
                        "tool_call", f"[{st['agent_handle']}] {st['action'][:80]}"
                    )

                    try:
                        a2a_result = await loop.run_in_executor(
                            None,
                            lambda action=st["action"]: send_a2a_message(
                                action, context_id=task_id
                            ),
                        )

                        if a2a_result.get("output"):
                            all_results[st["id"]] = a2a_result["output"]
                            yield send("log", f"[{st['agent_handle']}] Complete")
                        else:
                            all_results[st["id"]] = (
                                f"No output (status: {a2a_result.get('status')})"
                            )
                            yield send("log", f"[{st['agent_handle']}] No output")
                    except Exception as sub_err:
                        all_results[st["id"]] = f"Error: {sub_err}"
                        yield send("error", f"[{st['agent_handle']}] {sub_err}")

            _update_task(
                task_id, status="complete", context_id=task_id, result=all_results
            )
            yield send(
                "complete",
                "Team execution complete",
                {
                    "sub_task_count": len(all_results),
                    "agents_used": list(
                        {st["agent_handle"] for st in task["sub_tasks"]}
                    ),
                },
            )
        except Exception as exc:
            _update_task(task_id, status="failed")
            yield send("error", str(exc))

    return StreamingResponse(event_stream(), media_type="text/event-stream")


@app.get("/api/result-team/{task_id}")
def get_result_team(task_id: str, wallet: str = Depends(_wallet_auth)):
    """Get team task result with per-agent settlement."""
    task = _load_task(task_id, wallet_address=wallet)
    if not task or not task.get("is_team"):
        raise HTTPException(404, "Team task not found or access denied")
    if task["status"] != "complete" or not task.get("result"):
        return {"status": task["status"], "output": None}

    # Query mcp-meta for cost breakdown
    settlement = None
    if task.get("escrow_tx") and not task.get("settled"):
        try:
            import httpx
            from research_agent.mcp_client import _get_mcp_url, _get_mcp_headers

            mcp_url = _get_mcp_url()
            context_id = task.get("context_id", task_id)
            cost_resp = httpx.get(
                f"{mcp_url}/v1/cost/{context_id}",
                headers=_get_mcp_headers(),
                timeout=5.0,
            )

            if cost_resp.status_code == 200:
                cost_data = cost_resp.json()
                by_agent = cost_data.get("by_agent", {})

                # Build splits from per-agent costs
                # Map pod names to agent public keys
                team_map = {a["handle"]: a["public_key"] for a in task["team"]}
                splits = []
                for pod_name, cost in by_agent.items():
                    pub_key = team_map.get(pod_name)
                    if pub_key and cost > 0:
                        splits.append((pub_key, cost))

                # If mcp-meta didn't track costs, use registry rates
                if not splits:
                    tools = list_active_full()
                    tool_map = {t["handle"]: t for t in tools}
                    for agent in task["team"]:
                        tool = tool_map.get(agent["handle"])
                        if tool:
                            splits.append((agent["public_key"], tool["rate_stroops"]))

                orch_kp = _load_orchestrator_kp()
                if orch_kp and splits:
                    settle_tx = settle_team_escrow(orch_kp, task_id, splits)
                    _update_task(task_id, settled=1)
                    task["settled"] = 1  # keep local dict in sync for response
                    total_agent = sum(s[1] for s in splits)
                    settlement = {
                        "settle_tx": settle_tx,
                        "total_cost_stroops": total_agent,
                        "total_cost_usdc": f"{total_agent / 10_000_000:.7f}",
                        "splits": [
                            {
                                "address": addr,
                                "amount_stroops": amt,
                                "amount_usdc": f"{amt / 10_000_000:.7f}",
                            }
                            for addr, amt in splits
                        ],
                        "by_agent": by_agent,
                    }
        except Exception:
            pass

    # Format individual sub-task results and build combined markdown
    raw_results = task.get("result", {})
    formatted_results = {
        k: _format_output(v) if isinstance(v, str) else v
        for k, v in raw_results.items()
    }
    combined_output = _format_team_output(raw_results, task.get("sub_tasks", []))

    return {
        "status": task["status"],
        "output": combined_output,
        "sub_tasks": task.get("sub_tasks", []),
        "results": formatted_results,
        "team": task.get("team", []),
        "settlement": settlement,
        "settlement_status": "settled" if task.get("settled") else "pending",
        "estimated_cost": task.get("estimated_cost"),
        "budget_ceiling": task.get("budget_ceiling"),
        "deadline_unix": task.get("deadline_unix"),
    }


@app.post("/api/onboard")
async def onboard(req: OnboardRequest):
    async def event_stream():
        import json

        def send(step: str, status: str, data: dict | None = None):
            event = {
                "step": step,
                "status": status,
                "timestamp": time.strftime("%H:%M:%S"),
            }
            if data:
                event["data"] = data
            return f"data: {json.dumps(event)}\n\n"

        # Generate keypair (secret stays server-side only)
        kp = generate_agent_keypair()
        yield send("keypair", "done", {"public_key": kp.public_key})

        # Fund via Friendbot
        from research_agent.stellar_config import FRIENDBOT_URL as _FRIENDBOT

        yield send("fund", "running")
        if not _FRIENDBOT:
            yield send(
                "fund",
                "error",
                {
                    "message": "Auto-funding is unavailable on this network. "
                    "Fund the account externally and retry onboarding."
                },
            )
            return
        loop = asyncio.get_event_loop()
        funded = await loop.run_in_executor(None, lambda: fund_testnet(kp.public_key))
        if not funded:
            yield send("fund", "error", {"message": "Friendbot failed"})
            return
        await asyncio.sleep(2)
        yield send("fund", "done")

        # Set capabilities
        yield send("manifest", "running")
        try:
            tx_hash = await loop.run_in_executor(
                None,
                lambda: set_capability_manifest(
                    kp, req.capabilities, req.tier, req.rate_stroops
                ),
            )
            yield send("manifest", "done", {"tx_hash": tx_hash})
        except Exception as exc:
            yield send("manifest", "error", {"message": str(exc)})
            return

        # REP trustline
        yield send("trustline", "running")
        try:
            tx_hash = await loop.run_in_executor(
                None, lambda: establish_rep_trustline(kp)
            )
            yield send("trustline", "done", {"tx_hash": tx_hash})
        except Exception as exc:
            yield send("trustline", "error", {"message": str(exc)})
            return

        # Register in Soroban tool registry
        yield send("register", "running")
        try:
            agent_url = (
                f"{req.url}/{req.handle}"
                if not req.url.endswith(req.handle)
                else req.url
            )
            tx_hash = await loop.run_in_executor(
                None,
                lambda: submit_register_tool(
                    agent_kp=kp,
                    handle=req.handle,
                    url=agent_url,
                    capabilities=req.capabilities,
                    model_tier=req.tier,
                    rate_stroops=req.rate_stroops,
                    max_tokens=req.max_tokens,
                ),
            )
            yield send("register", "done", {"tx_hash": tx_hash})
        except Exception as exc:
            yield send("register", "error", {"message": str(exc)})
            return

        yield send("complete", "done", {"public_key": kp.public_key})

    return StreamingResponse(event_stream(), media_type="text/event-stream")
