"""
Real-chain verification that the wallet protagonist features are wired end
to end against testnet. No mocks. Run with:

    cd agent && uv run python tests/manual_wallet_balance_e2e.py

What this proves:

  1. A freshly funded testnet wallet — with a USDC trustline and a USDC
     balance — returns the expected JSON shape from the live FastAPI
     `/api/prepare-wallet` endpoint, which is what the header dropdown
     consumes to render "0.45 USDC".

  2. `usdc_balance_display` is a string the UI can render verbatim, and
     `usdc_balance` is the integer stroops the SAC reports — both stay
     consistent for the test wallet we just funded.

  3. `/api/prepare-wallet` is reachable WITHOUT auth (it lives in
     `_PUBLIC_PATHS`) so the header dropdown can call it before the user
     does anything else.
"""

import json
import os
import subprocess
import sys
import time

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "src"))
os.environ.setdefault(
    "X402_ESCROW_CONTRACT",
    "CAMPNA3TNE23UYM5WSTXHQWA5HGU7HL6M2T64R5VNQT5PJS6UBIXOCCO",
)
os.environ.setdefault("STELLAR_NETWORK", "testnet")
os.environ.setdefault("STELLAR_RPC_URL", "https://soroban-testnet.stellar.org")
os.environ.setdefault("STELLAR_HORIZON_URL", "https://horizon-testnet.stellar.org")

from stellar_sdk import (
    Asset, Keypair, Server, SorobanServer, TransactionBuilder, scval,
)
from research_agent.escrow import _wait_for_tx_confirmation


# Orchestrator secret read from env, never hardcoded.
# Source the same env var the api server uses. Operator: `set -a; . .env; set +a`
# then run, or export ORCHESTRATOR_SECRET_KEY in the shell.
ORCH_SECRET = os.environ.get("ORCHESTRATOR_SECRET_KEY")
if not ORCH_SECRET:
    raise SystemExit(
        "ORCHESTRATOR_SECRET_KEY not set. Source it from .env "
        "(`set -a; . .env; set +a`) or export it before running."
    )
RPC_URL = "https://soroban-testnet.stellar.org"
HORIZON_URL = "https://horizon-testnet.stellar.org"
PASSPHRASE = "Test SDF Network ; September 2015"
USDC_SAC = "CDUQPQI6SCMDBWK4ZKBMJGMGSJHHO5ARKDW45M7GJSEFNC44RUPD2WJQ"
FASTAPI = "http://localhost:5050"

MINT_STROOPS = 4_500_000  # 0.45 USDC — chosen to match a memorable display


def log(emoji, msg):
    print(f"{emoji}  {msg}", flush=True)


def friendbot(public_key: str):
    log("⇡", f"friendbot funding {public_key[:14]}…")
    subprocess.run(
        ["/usr/bin/curl", "-s", "-f", f"https://friendbot.stellar.org?addr={public_key}"],
        check=True, timeout=30,
    )


def open_trustline(test_kp: Keypair, issuer: str):
    server = Server(HORIZON_URL)
    account = server.load_account(test_kp.public_key)
    b = TransactionBuilder(source_account=account, network_passphrase=PASSPHRASE, base_fee=100)
    b.append_change_trust_op(asset=Asset("USDC", issuer))
    b.set_timeout(60)
    tx = b.build(); tx.sign(test_kp)
    server.submit_transaction(tx)


def mint_usdc(orch_kp: Keypair, to: str, amount: int):
    soroban = SorobanServer(RPC_URL)
    src = soroban.load_account(orch_kp.public_key)
    b = TransactionBuilder(source_account=src, network_passphrase=PASSPHRASE, base_fee=1000)
    b.append_invoke_contract_function_op(
        contract_id=USDC_SAC, function_name="mint",
        parameters=[scval.to_address(to), scval.to_int128(amount)],
    )
    b.set_timeout(60); tx = b.build()
    sim = soroban.simulate_transaction(tx)
    assert not sim.error, f"mint sim failed: {sim.error}"
    tx = soroban.prepare_transaction(tx, sim); tx.sign(orch_kp)
    resp = soroban.send_transaction(tx)
    conf = _wait_for_tx_confirmation(soroban, resp.hash, timeout_s=30)
    assert conf["status"] == "SUCCESS", f"mint not confirmed: {conf}"


def post_prepare_wallet(public_key: str) -> dict:
    """Call the real FastAPI endpoint that the header uses."""
    payload = json.dumps({"public_key": public_key}).encode()
    result = subprocess.run(
        ["/usr/bin/curl", "-s", "-f", "-X", "POST",
         "-H", "Content-Type: application/json",
         "-d", payload.decode(),
         f"{FASTAPI}/api/prepare-wallet"],
        capture_output=True, timeout=20,
    )
    if result.returncode != 0:
        raise RuntimeError(f"/api/prepare-wallet failed: {result.stderr.decode()[:300]}")
    return json.loads(result.stdout)


def main():
    log("🟢", "real-chain verification of /api/prepare-wallet (header balance source)")
    orch = Keypair.from_secret(ORCH_SECRET)
    test = Keypair.random()
    log("🟢", f"orchestrator: {orch.public_key[:14]}…")
    log("🟢", f"test wallet:  {test.public_key}")
    print()

    # 1. Bring the wallet to a clean, predictable on-chain state.
    friendbot(test.public_key)
    open_trustline(test, orch.public_key)
    mint_usdc(orch, test.public_key, MINT_STROOPS)
    log("✓", f"minted {MINT_STROOPS/1e7} USDC to test wallet")
    time.sleep(3)  # let the SAC settle before reading
    print()

    # 2. The same call the header dropdown will make.
    log("▶", "POST /api/prepare-wallet (no auth header, public endpoint)")
    resp = post_prepare_wallet(test.public_key)
    log("·", f"response keys: {sorted(resp.keys())}")
    log("·", f"usdc_balance         (raw stroops): {resp.get('usdc_balance')}")
    log("·", f"usdc_balance_display (UI string):   {resp.get('usdc_balance_display')!r}")
    log("·", f"has_trustline                    :  {resp.get('has_trustline')}")
    print()

    # 3. Pin the contract that the header expects.
    assert resp.get("has_trustline") is True, "trustline must be open"
    assert resp.get("usdc_balance") == MINT_STROOPS, (
        f"expected {MINT_STROOPS} stroops, got {resp.get('usdc_balance')}"
    )
    expected_display = f"{MINT_STROOPS / 10_000_000:.2f}"
    assert resp.get("usdc_balance_display") == expected_display, (
        f"display string mismatch: expected {expected_display!r}, got {resp.get('usdc_balance_display')!r}"
    )
    assert isinstance(resp.get("usdc_balance_display"), str), "display must be a string for the UI"
    log("✅", "shape matches what the React header consumes")
    print()

    # 4. Verify the endpoint is reachable without auth (the header dropdown
    #    does not pass a wallet-auth header for read-only calls).
    log("▶", "verifying /api/prepare-wallet stays unauthenticated (header calls it pre-auth)")
    result = subprocess.run(
        ["/usr/bin/curl", "-s", "-o", "/dev/null", "-w", "%{http_code}",
         "-X", "POST",
         "-H", "Content-Type: application/json",
         "-d", json.dumps({"public_key": test.public_key}),
         f"{FASTAPI}/api/prepare-wallet"],
        capture_output=True, timeout=20,
    )
    status = result.stdout.decode().strip()
    assert status == "200", f"expected 200 unauthenticated, got {status}"
    log("✅", "200 OK without auth — header can safely call this on every dropdown open")

    print()
    log("🎉", "wallet-balance E2E passed against live testnet + FastAPI")


if __name__ == "__main__":
    main()
