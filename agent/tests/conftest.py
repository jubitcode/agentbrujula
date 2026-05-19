"""Shared test fixtures for integration tests."""

import os
import subprocess
import sys
import time
from pathlib import Path

import pytest

# Ensure src/ is on path
sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "src"))


# ---------------------------------------------------------------------------
# Stellar CLI helpers
# ---------------------------------------------------------------------------

def _stellar_key(alias: str) -> str:
    try:
        r = subprocess.run(["stellar", "keys", "show", alias], capture_output=True, text=True, timeout=5)
        return r.stdout.strip()
    except Exception:
        return ""


def _stellar_public(alias: str) -> str:
    try:
        r = subprocess.run(["stellar", "keys", "address", alias], capture_output=True, text=True, timeout=5)
        return r.stdout.strip()
    except Exception:
        return ""


def _stellar_cli_available() -> bool:
    try:
        r = subprocess.run(["stellar", "--version"], capture_output=True, timeout=5)
        return r.returncode == 0
    except Exception:
        return False


# ---------------------------------------------------------------------------
# Skip markers
# ---------------------------------------------------------------------------

HAS_STELLAR_CLI = _stellar_cli_available()
ORCHESTRATOR_SECRET = _stellar_key("orchestrator") if HAS_STELLAR_CLI else ""
TESTCLIENT_SECRET = _stellar_key("testclient") if HAS_STELLAR_CLI else ""

requires_stellar = pytest.mark.skipif(
    not HAS_STELLAR_CLI,
    reason="stellar CLI not installed",
)

requires_orchestrator = pytest.mark.skipif(
    not ORCHESTRATOR_SECRET,
    reason="stellar identity 'orchestrator' not configured (run: stellar keys generate orchestrator --network testnet)",
)

requires_testclient = pytest.mark.skipif(
    not TESTCLIENT_SECRET,
    reason="stellar identity 'testclient' not configured",
)


def _mcp_meta_reachable() -> bool:
    try:
        import httpx
        r = httpx.get("http://127.0.0.1:8080/health", timeout=2.0)
        return r.status_code == 200
    except Exception:
        return False


requires_mcp_meta = pytest.mark.skipif(
    not _mcp_meta_reachable(),
    reason="mcp-meta not running at localhost:8080",
)


# ---------------------------------------------------------------------------
# Testnet keypair fixtures
# ---------------------------------------------------------------------------

@pytest.fixture(scope="module")
def orchestrator_kp():
    """Real orchestrator keypair from stellar CLI."""
    from stellar_sdk import Keypair
    if not ORCHESTRATOR_SECRET:
        pytest.skip("orchestrator key not configured")
    return Keypair.from_secret(ORCHESTRATOR_SECRET)


@pytest.fixture(scope="module")
def testclient_kp():
    """Real testclient keypair from stellar CLI."""
    from stellar_sdk import Keypair
    if not TESTCLIENT_SECRET:
        pytest.skip("testclient key not configured")
    return Keypair.from_secret(TESTCLIENT_SECRET)


@pytest.fixture(scope="module")
def funded_agent_kp():
    """Fresh funded keypair for agent role in tests, with USDC trustline."""
    from stellar_sdk import Keypair, Server, TransactionBuilder, Network, Asset
    from research_agent.stellar_identity import fund_testnet, HORIZON_TESTNET
    from research_agent.x402_config import X402_USDC_ADDRESS

    kp = Keypair.random()
    funded = fund_testnet(kp.public_key)
    if not funded:
        pytest.skip("Friendbot funding failed")
    time.sleep(2)  # Wait for ledger

    # Establish USDC trustline so agent can receive USDC from settle()
    try:
        # The USDC issuer is the orchestrator (SAC deployer)
        orch_pub = _stellar_public("orchestrator")
        if orch_pub:
            server = Server(HORIZON_TESTNET)
            account = server.load_account(kp.public_key)
            builder = TransactionBuilder(
                source_account=account,
                network_passphrase=Network.TESTNET_NETWORK_PASSPHRASE,
                base_fee=100,
            )
            builder.append_change_trust_op(
                asset=Asset("USDC", orch_pub),
            )
            builder.set_timeout(30)
            tx = builder.build()
            tx.sign(kp)
            server.submit_transaction(tx)
            time.sleep(2)
    except Exception:
        pass  # Best effort — some tests may still work without it

    return kp


# ---------------------------------------------------------------------------
# FastAPI test client
# ---------------------------------------------------------------------------

@pytest.fixture(scope="module")
def api_client():
    """FastAPI TestClient with real x402 config loaded."""
    from fastapi.testclient import TestClient
    from api_test_app import app
    return TestClient(app)


# ---------------------------------------------------------------------------
# Cleanup fixtures
# ---------------------------------------------------------------------------

@pytest.fixture(autouse=True)
def _clear_nonce_cache():
    """Reset x402 nonce cache between tests to avoid replay false positives."""
    try:
        import research_agent.x402_middleware as mw
        mw._seen_nonces.clear()
    except Exception:
        pass
    yield
    try:
        import research_agent.x402_middleware as mw
        mw._seen_nonces.clear()
    except Exception:
        pass


@pytest.fixture(autouse=True)
def _clear_tasks():
    """Clean task store between tests."""
    try:
        sys.path.insert(0, str(Path(__file__).resolve().parent.parent.parent / "api"))
        import server
        server._tasks.clear()
    except Exception:
        pass
    yield
