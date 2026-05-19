"""Test helpers for setting up USDC trustlines + minting test balances.

The escrow contract calls `usdc_token.transfer(client, escrow, amount)`. On
Stellar that fails for any account that has not established a trustline to
the USDC asset — and on testnet a freshly-friendbot-funded account starts
with zero trustlines. So every E2E test that goes through `create_escrow`
needs to first:

  1. Establish a USDC trustline on the client account (signed by client).
  2. Have the issuer (orchestrator on testnet) mint USDC to the client.

These helpers encapsulate both, so individual tests don't repeat the
boilerplate. They mirror the production `prepare_wallet` endpoint logic
in `api/server.py` so a green E2E test gives confidence the user-facing
onboarding flow will work too.
"""

from __future__ import annotations

import time

from stellar_sdk import (
    Asset,
    Keypair,
    Server,
    SorobanServer,
    TransactionBuilder,
    scval,
)

from research_agent.stellar_config import (
    HORIZON_URL,
    NETWORK_PASSPHRASE,
    RPC_URL,
    USDC_TOKEN_ID,
)

# How many ledger-poll cycles to wait for a Soroban tx to land. Testnet
# can spike past 30s under load, so we give it ~90s of budget.
_SOROBAN_POLL_ATTEMPTS = 45
_SOROBAN_POLL_INTERVAL_S = 2.0


def establish_usdc_trustline(client_kp: Keypair, orchestrator_kp: Keypair) -> str:
    """Submit a CHANGE_TRUST op for `Asset("USDC", orchestrator.public_key)`.

    Returns the Horizon tx hash. Idempotent at the network layer — Stellar
    treats a duplicate trustline as a no-op.
    """
    server = Server(HORIZON_URL)
    account = server.load_account(client_kp.public_key)

    builder = TransactionBuilder(
        source_account=account,
        network_passphrase=NETWORK_PASSPHRASE,
        base_fee=100,
    )
    builder.append_change_trust_op(
        asset=Asset("USDC", orchestrator_kp.public_key),
    )
    builder.set_timeout(60)
    tx = builder.build()
    tx.sign(client_kp)

    resp = server.submit_transaction(tx)
    return resp["hash"]


def mint_usdc_via_sac(orchestrator_kp: Keypair, recipient_public: str, amount_stroops: int) -> str:
    """Mint USDC to `recipient_public` via the SAC `mint` function.

    The orchestrator account must be the testnet USDC asset issuer
    (verified once via `balance` returning i64::MAX). Returns the
    Soroban tx hash once the transaction is confirmed SUCCESS.
    """
    server = SorobanServer(RPC_URL)
    source = server.load_account(orchestrator_kp.public_key)

    builder = TransactionBuilder(
        source_account=source,
        network_passphrase=NETWORK_PASSPHRASE,
        base_fee=100,
    )
    builder.append_invoke_contract_function_op(
        contract_id=USDC_TOKEN_ID,
        function_name="mint",
        parameters=[
            scval.to_address(recipient_public),
            scval.to_int128(amount_stroops),
        ],
    )
    builder.set_timeout(60)
    tx = builder.build()

    sim = server.simulate_transaction(tx)
    if sim.error:
        raise RuntimeError(f"Mint USDC simulation failed: {sim.error}")

    tx = server.prepare_transaction(tx, sim)
    tx.sign(orchestrator_kp)

    sent = server.send_transaction(tx)
    tx_hash = sent.hash

    # Note: SDK returns an Enum (GetTransactionStatus), not a string. Compare
    # against `.value` so this works regardless of how the enum was imported.
    for _ in range(_SOROBAN_POLL_ATTEMPTS):
        time.sleep(_SOROBAN_POLL_INTERVAL_S)
        status = server.get_transaction(tx_hash)
        status_str = getattr(status.status, "value", status.status)
        if status_str == "SUCCESS":
            return tx_hash
        if status_str == "FAILED":
            raise RuntimeError(f"Mint USDC failed on-chain: {status}")

    raise TimeoutError(f"Mint USDC tx {tx_hash} did not confirm in time")


def fund_with_usdc(
    client_kp: Keypair,
    orchestrator_kp: Keypair,
    amount_stroops: int = 1_000_000,
) -> dict:
    """One-shot helper: trustline + mint. Returns dict with both tx hashes."""
    trustline_tx = establish_usdc_trustline(client_kp, orchestrator_kp)
    mint_tx = mint_usdc_via_sac(orchestrator_kp, client_kp.public_key, amount_stroops)
    return {"trustline_tx": trustline_tx, "mint_tx": mint_tx, "amount": amount_stroops}
