"""One-shot migration: register the 6 canonical AgentPods in the tool-registry.

An earlier tool-registry contract had 9 agents but their secret keys were lost
(generated server-side during /api/onboard, never persisted centrally). So
this isn't a literal copy — it's a fresh registration of the 6 AgentPods
that actually ship in `mcp-meta/manifests/`, each with a freshly-minted
Stellar keypair, against the tool-registry (which has `update_tool`).

Run from `agent_canonical/`:
    cd agent && uv run python ../scripts/migrate_v4_agents.py

What it does (per agent, idempotent):
  1. Generate a new Stellar keypair.
  2. Friendbot-fund the new account on testnet.
  3. Establish a REP token trustline so future `mint_rep` calls work.
  4. Call `register_tool` on the tool-registry.
  5. Append `{handle, public_key, secret_key, ...}` to
     `scripts/v4_agent_keys.json` so the operator can use `update_tool`
     later to change URLs etc.

Skips an agent if its handle is already registered (uses `get_tool`).

URL chosen: `http://mcp-meta:9876` — works inside docker-compose. Update
later via `submit_update_tool` if/when a public URL exists.
"""

from __future__ import annotations

import json
import os
import sys
import time
from pathlib import Path
from typing import TypedDict

# Allow running from repo root.
sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "agent" / "src"))

from stellar_sdk import Keypair, SorobanServer, scval

from research_agent.soroban_client import (
    submit_register_tool,
    _simulate_invoke,  # used for the pre-check
)
from research_agent.stellar_config import (
    NETWORK_PASSPHRASE,
    RPC_URL,
    TOOL_REGISTRY_CONTRACT_ID,
)
from research_agent.stellar_identity import (
    establish_rep_trustline,
    fund_testnet,
)


class AgentSpec(TypedDict):
    handle: str
    capabilities: list[str]
    model_tier: str
    rate_stroops: int
    max_tokens: int


# Defaults pulled from each AgentPod manifest in mcp-meta/manifests/.
# Capabilities reflect the agent's listed skills; rate/tier mirror the
# previous registrations roughly (cheap pods on HAIKU, content/research
# on SONNET, debate on OPUS).
AGENTS: list[AgentSpec] = [
    {
        "handle": "adr-moderator",
        "capabilities": ["architecture", "debate", "adr", "decision"],
        "model_tier": "OPUS",
        "rate_stroops": 3_000_000,  # 0.3 XLM
        "max_tokens": 8192,
    },
    {
        "handle": "competitive-intel",
        "capabilities": ["competitive_intel", "research", "analysis"],
        "model_tier": "SONNET",
        "rate_stroops": 1_500_000,  # 0.15 XLM
        "max_tokens": 4096,
    },
    {
        "handle": "content-writer",
        "capabilities": ["writing", "content", "blog"],
        "model_tier": "SONNET",
        "rate_stroops": 1_000_000,
        "max_tokens": 4096,
    },
    {
        "handle": "hebbian",
        "capabilities": ["knowledge", "memory", "search", "ingest"],
        "model_tier": "HAIKU",
        "rate_stroops": 500_000,
        "max_tokens": 2048,
    },
    {
        "handle": "research-agent",
        "capabilities": ["research", "analysis", "web_search"],
        "model_tier": "SONNET",
        "rate_stroops": 1_000_000,
        "max_tokens": 4096,
    },
    {
        "handle": "trend-watcher",
        "capabilities": ["trends", "news", "monitoring"],
        "model_tier": "SONNET",
        "rate_stroops": 1_500_000,
        "max_tokens": 4096,
    },
]

DEFAULT_URL = os.environ.get("AGENT_URL", "http://mcp-meta:9876")
KEYS_OUT = Path(__file__).resolve().parent / "v4_agent_keys.json"


def handle_already_registered(handle: str) -> bool:
    """Probe the registry with get_tool; if it doesn't panic, the handle exists."""
    try:
        _simulate_invoke(
            RPC_URL,
            TOOL_REGISTRY_CONTRACT_ID,
            "get_tool",
            [scval.to_string(handle)],
        )
        return True
    except RuntimeError:
        # `get_tool` panics with `"tool not found"` for unknown handles;
        # the simulator surfaces that as RuntimeError.
        return False


def wait_for_account(public_key: str, attempts: int = 20, sleep_s: float = 1.5) -> bool:
    """After friendbot, Horizon takes a moment to expose the account."""
    server = SorobanServer(RPC_URL)
    for _ in range(attempts):
        try:
            server.load_account(public_key)
            return True
        except Exception:
            time.sleep(sleep_s)
    return False


def load_existing_keys() -> dict[str, dict]:
    if KEYS_OUT.exists():
        return json.loads(KEYS_OUT.read_text())
    return {}


def save_keys(keys: dict[str, dict]) -> None:
    KEYS_OUT.write_text(json.dumps(keys, indent=2) + "\n")


def main() -> int:
    print(f"tool-registry contract: {TOOL_REGISTRY_CONTRACT_ID}")
    print(f"default agent URL: {DEFAULT_URL}")
    print(f"keys file: {KEYS_OUT}")
    print()

    keys = load_existing_keys()
    registered = 0
    skipped = 0
    failed = 0

    for spec in AGENTS:
        handle = spec["handle"]
        if handle_already_registered(handle):
            print(f"  SKIP {handle:20s} already registered")
            skipped += 1
            continue

        kp = Keypair.random()
        print(f"  NEW  {handle:20s} {kp.public_key}")

        if not fund_testnet(kp.public_key):
            print(f"       friendbot fund failed for {kp.public_key}")
            failed += 1
            continue
        if not wait_for_account(kp.public_key):
            print(f"       account did not appear on Horizon in time")
            failed += 1
            continue

        try:
            trustline_tx = establish_rep_trustline(kp)
            print(f"       trustline tx: {trustline_tx}")
        except Exception as exc:
            print(f"       trustline failed (non-fatal): {exc}")

        try:
            reg_tx = submit_register_tool(
                kp,
                handle=handle,
                url=DEFAULT_URL,
                capabilities=spec["capabilities"],
                model_tier=spec["model_tier"],
                rate_stroops=spec["rate_stroops"],
                max_tokens=spec["max_tokens"],
            )
            print(f"       register tx:  {reg_tx}")
        except Exception as exc:
            print(f"       register failed: {exc}")
            failed += 1
            continue

        keys[handle] = {
            "public_key": kp.public_key,
            "secret_key": kp.secret,
            "url": DEFAULT_URL,
            "capabilities": spec["capabilities"],
            "model_tier": spec["model_tier"],
            "rate_stroops": spec["rate_stroops"],
            "max_tokens": spec["max_tokens"],
            "registered_at": int(time.time()),
        }
        save_keys(keys)
        registered += 1

    print()
    print(f"summary: registered={registered} skipped={skipped} failed={failed}")
    print(f"secrets saved to {KEYS_OUT}")
    return 0 if failed == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
