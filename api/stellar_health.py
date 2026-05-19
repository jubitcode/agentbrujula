from __future__ import annotations

import os
import time
from typing import Any

from fastapi import APIRouter, Response
from stellar_sdk import Keypair, Server

router = APIRouter()

# Cache the snapshot for 15s to avoid hammering Horizon under prometheus poll
_cache: dict[str, Any] = {"data": None, "expires": 0.0}
_CACHE_TTL = 15.0

LOW_BALANCE_XLM = float(os.environ.get("STELLAR_LOW_BALANCE_XLM", "10"))


def _orchestrator_public_key() -> str | None:
    secret = os.environ.get("ORCHESTRATOR_SECRET_KEY")
    if not secret:
        return None
    try:
        return Keypair.from_secret(secret).public_key
    except Exception:
        return None


def _xlm_balance(public_key: str, horizon_url: str) -> float | None:
    """Return native (XLM) balance for the account, or None if unreachable."""
    try:
        server = Server(horizon_url)
        account = server.accounts().account_id(public_key).call()
        for bal in account.get("balances", []):
            if bal.get("asset_type") == "native":
                return float(bal.get("balance", 0))
        return 0.0
    except Exception:
        return None


def _snapshot() -> dict[str, Any]:
    """Build the health snapshot. Cached for _CACHE_TTL seconds."""
    now = time.monotonic()
    if _cache["data"] is not None and now < _cache["expires"]:
        return _cache["data"]

    from research_agent.stellar_config import HORIZON_URL, STELLAR_NETWORK

    public_key = _orchestrator_public_key()
    balance: float | None = None
    horizon_up = False
    low = False

    if public_key:
        balance = _xlm_balance(public_key, HORIZON_URL)
        horizon_up = balance is not None
        low = balance is not None and balance < LOW_BALANCE_XLM

    data = {
        "network": STELLAR_NETWORK,
        "horizon_url": HORIZON_URL,
        "orchestrator": {
            "configured": public_key is not None,
            "public_key": public_key,
            "xlm_balance": balance,
            "min_balance_xlm": LOW_BALANCE_XLM,
            "low_balance": low,
        },
        "checks": {
            "horizon_reachable": horizon_up,
        },
    }
    _cache["data"] = data
    _cache["expires"] = now + _CACHE_TTL
    return data


@router.get("/health/stellar")
def health_stellar() -> dict[str, Any]:
    return _snapshot()


@router.get("/metrics/stellar")
def metrics_stellar() -> Response:
    snap = _snapshot()
    network = snap["network"]
    balance = snap["orchestrator"]["xlm_balance"]
    horizon_up = 1 if snap["checks"]["horizon_reachable"] else 0
    low = 1 if snap["orchestrator"]["low_balance"] else 0

    lines = [
        "# HELP stellar_orchestrator_xlm_balance Orchestrator account XLM balance.",
        "# TYPE stellar_orchestrator_xlm_balance gauge",
    ]
    if balance is not None:
        lines.append(
            f'stellar_orchestrator_xlm_balance{{network="{network}"}} {balance}'
        )

    lines.extend(
        [
            "# HELP stellar_horizon_up Horizon reachability (1=up, 0=down).",
            "# TYPE stellar_horizon_up gauge",
            f'stellar_horizon_up{{network="{network}"}} {horizon_up}',
            "# HELP stellar_orchestrator_low_balance 1 when orchestrator XLM is below the threshold.",
            "# TYPE stellar_orchestrator_low_balance gauge",
            f'stellar_orchestrator_low_balance{{network="{network}"}} {low}',
        ]
    )

    body = "\n".join(lines) + "\n"
    return Response(content=body, media_type="text/plain; version=0.0.4")
