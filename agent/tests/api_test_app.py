"""Reusable test app — sets env vars before importing server.

Import this instead of server.py directly. Ensures x402 config
is loaded with correct env vars regardless of import order.
"""

import os
import subprocess
import sys


def _get_stellar_key(alias: str) -> str:
    """Get a secret key from stellar CLI."""
    try:
        result = subprocess.run(
            ["stellar", "keys", "show", alias],
            capture_output=True, text=True, timeout=5,
        )
        return result.stdout.strip()
    except Exception:
        return ""


# Load orchestrator key from stellar CLI if not already set
if not os.environ.get("ORCHESTRATOR_SECRET_KEY"):
    _orch_key = _get_stellar_key("orchestrator")
    if _orch_key:
        os.environ["ORCHESTRATOR_SECRET_KEY"] = _orch_key
    else:
        os.environ["ORCHESTRATOR_SECRET_KEY"] = "PLACEHOLDER"

# Ensure x402 is enabled with real config
os.environ.setdefault("X402_ENABLED", "true")
os.environ.setdefault("X402_DEV_MODE", "true")
os.environ.setdefault("X402_ESCROW_CONTRACT", "CAMPNA3TNE23UYM5WSTXHQWA5HGU7HL6M2T64R5VNQT5PJS6UBIXOCCO")
os.environ.setdefault("X402_USDC_ADDRESS", "CDUQPQI6SCMDBWK4ZKBMJGMGSJHHO5ARKDW45M7GJSEFNC44RUPD2WJQ")
os.environ.setdefault("X402_FACILITATOR_URL", "https://x402.org/facilitator")
os.environ.setdefault("X402_NETWORK", "stellar:testnet")
os.environ.setdefault("STELLAR_RPC_URL", "https://soroban-testnet.stellar.org")

# Add api/ to path
sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", "api"))

from server import app  # noqa: E402
