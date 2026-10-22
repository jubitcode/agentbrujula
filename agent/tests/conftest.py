"""Shared test fixtures for integration tests."""

import os
import subprocess
import sys
import time
from pathlib import Path

import pytest

# ---------------------------------------------------------------------------
# Keep the suite off the developer's database — before anything imports it
# ---------------------------------------------------------------------------
# `api/server.py` reads the repository's .env at import time and calls
# os.environ.setdefault for each key, DATABASE_URL among them. The _clear_tasks
# fixture below imports that module, and it is autouse — so simply running the
# tests used to repoint every TaskStore in the process at the real Postgres.
#
# The damage is permanent, not transient: nonces are single-use by design, so a
# test that claims a fixed nonce passes once per database and fails on every run
# afterwards. That is why these tests behaved differently from one machine to
# the next, and why isolating $HOME alone changed nothing.
#
# Setting the variable here (before any import of server) wins, because
# setdefault will not overwrite an existing key. An empty value sends TaskStore
# down its SQLite path, which lands under $HOME and is therefore disposable.
#
# Override deliberately with TEST_DATABASE_URL when testing Postgres itself.
os.environ["DATABASE_URL"] = os.environ.get("TEST_DATABASE_URL", "")

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

# The alias must be the account the deployed contracts were initialised with:
# usdc-escrow stores it as ORCH and rejects settle/refund from anyone else.
# On testnet that is `brujula-orch` (GAT6TN4C...), which is also the issuer of
# the USDC asset held in escrow. The alias `orchestrator` is a different,
# unrelated account — using it makes every settle fail on require_auth and
# makes the USDC trustline below point at an asset that does not exist.
ORCH_ALIAS = os.environ.get("STELLAR_ORCH_ALIAS", "brujula-orch")

HAS_STELLAR_CLI = _stellar_cli_available()
ORCHESTRATOR_SECRET = _stellar_key(ORCH_ALIAS) if HAS_STELLAR_CLI else ""
TESTCLIENT_SECRET = _stellar_key("testclient") if HAS_STELLAR_CLI else ""

requires_stellar = pytest.mark.skipif(
    not HAS_STELLAR_CLI,
    reason="stellar CLI not installed",
)

requires_orchestrator = pytest.mark.skipif(
    not ORCHESTRATOR_SECRET,
    reason=f"stellar identity '{ORCH_ALIAS}' not configured "
           f"(set STELLAR_ORCH_ALIAS, or run: stellar keys generate {ORCH_ALIAS} --network testnet)",
)

requires_testclient = pytest.mark.skipif(
    not TESTCLIENT_SECRET,
    reason="stellar identity 'testclient' not configured",
)


# mcp-meta serves on 9876. The old default here was 8080, which never matched,
# so this marker skipped unconditionally while the tests that read MCP_META_URL
# went on hitting the real service.
MCP_META_URL = os.environ.get("MCP_META_URL", "http://127.0.0.1:9876").rstrip("/")


def _mcp_meta_reachable() -> bool:
    try:
        import httpx
        r = httpx.get(f"{MCP_META_URL}/health", timeout=2.0)
        return r.status_code == 200
    except Exception:
        return False


requires_mcp_meta = pytest.mark.skipif(
    not _mcp_meta_reachable(),
    reason=f"mcp-meta not reachable at {MCP_META_URL}",
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
        # The USDC issuer is the orchestrator account (it deployed the SAC).
        # Must be the same alias as ORCH_ALIAS — see the note above.
        orch_pub = _stellar_public(ORCH_ALIAS)
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
def _isolated_x402_store(tmp_path, monkeypatch):
    """Give the x402 middleware its own task store, per test.

    `x402_middleware._get_store()` builds `TaskStore()` with no arguments,
    which lands on ~/.mcp-meta/marketplace.db — the developer's own database.
    Nonces are single-use by design, so a test that claims a fixed nonce
    passed the first time it ever ran and failed on every run afterwards, on
    that machine, forever.

    That is not hypothetical: `test_replay_attack_blocked` wrote
    `reused-nonce-123` into that file on 2026-09-12 and had been failing ever
    since, for a reason that has nothing to do with replay protection.

    Clearing `_seen_nonces` (the fixture below) was never enough: that is the
    in-memory cache, and the claim that actually blocks a replay is a row in
    the database.
    """
    from research_agent.task_store import TaskStore

    store = TaskStore(db_path=str(tmp_path / "x402.db"))
    try:
        import research_agent.x402_middleware as mw

        monkeypatch.setattr(mw, "_store", store, raising=False)
    except Exception:
        pass
    yield store
    store.close()


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
