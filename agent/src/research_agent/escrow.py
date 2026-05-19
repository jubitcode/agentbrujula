from __future__ import annotations

import os
from typing import Any
from enum import Enum

from stellar_sdk import (
    Asset,
    Claimant,
    ClaimPredicate,
    Keypair,
    Server,
    SorobanServer,
    TransactionBuilder,
    scval,
)
from stellar_sdk import xdr as stellar_xdr

from .stellar_config import NETWORK_PASSPHRASE, HORIZON_URL, RPC_URL
from .x402_config import X402_ESCROW_CONTRACT


def create_escrow(
    orchestrator_kp: Keypair,
    agent_public: str,
    amount_xlm: str,
    deadline_unix: int,
) -> tuple[str, Any]:
    """Create a Claimable Balance as escrow.

    Two claimants:
    - Agent: can claim before deadline
    - Orchestrator: can reclaim after deadline (auto-return)

    Returns the claimable balance ID.
    """
    server = Server(HORIZON_URL)
    account = server.load_account(orchestrator_kp.public_key)

    claimants = [
        Claimant(
            destination=agent_public,
            predicate=ClaimPredicate.predicate_before_absolute_time(deadline_unix),
        ),
        Claimant(
            destination=orchestrator_kp.public_key,
            predicate=ClaimPredicate.predicate_not(
                ClaimPredicate.predicate_before_absolute_time(deadline_unix)
            ),
        ),
    ]

    builder = TransactionBuilder(
        source_account=account,
        network_passphrase=NETWORK_PASSPHRASE,
        base_fee=100,
    )
    builder.append_create_claimable_balance_op(
        asset=Asset.native(),
        amount=amount_xlm,
        claimants=claimants,
    )
    builder.set_timeout(30)
    tx = builder.build()
    tx.sign(orchestrator_kp)

    resp = server.submit_transaction(tx)
    balance_id = _extract_balance_id(resp)
    tx_hash = resp.get("hash", "")
    return balance_id, tx_hash


def claim_escrow(agent_kp: Keypair, balance_id: str) -> str:
    """Agent claims the escrow claimable balance."""
    server = Server(HORIZON_URL)
    account = server.load_account(agent_kp.public_key)

    builder = TransactionBuilder(
        source_account=account,
        network_passphrase=NETWORK_PASSPHRASE,
        base_fee=100,
    )
    builder.append_claim_claimable_balance_op(balance_id=balance_id)
    builder.set_timeout(30)
    tx = builder.build()
    tx.sign(agent_kp)

    resp = server.submit_transaction(tx)
    return resp["hash"]


def reclaim_escrow(orchestrator_kp: Keypair, balance_id: str) -> str:
    """Orchestrator reclaims escrow after deadline has passed."""
    server = Server(HORIZON_URL)
    account = server.load_account(orchestrator_kp.public_key)

    builder = TransactionBuilder(
        source_account=account,
        network_passphrase=NETWORK_PASSPHRASE,
        base_fee=100,
    )
    builder.append_claim_claimable_balance_op(balance_id=balance_id)
    builder.set_timeout(30)
    tx = builder.build()
    tx.sign(orchestrator_kp)

    resp = server.submit_transaction(tx)
    return resp["hash"]


def _extract_balance_id(resp: dict) -> str:
    """Extract the claimable balance ID from a transaction response."""
    result_xdr = resp.get("result_xdr", "")
    if not result_xdr:
        raise RuntimeError("No result_xdr in response")

    tx_result = stellar_xdr.TransactionResult.from_xdr(result_xdr)
    results = tx_result.result.results
    if not results:
        raise RuntimeError("No operation results")

    op_result = results[0].tr
    if (
        op_result
        and op_result.type == stellar_xdr.OperationType.CREATE_CLAIMABLE_BALANCE
    ):
        balance_id_xdr = op_result.create_claimable_balance_result.balance_id
        return balance_id_xdr.to_xdr_bytes().hex()

    raise RuntimeError(f"Unexpected operation type: {op_result.type}")


def _wait_for_tx_confirmation(
    server: "SorobanServer",
    tx_hash: str,
    timeout_s: float = 30.0,
    poll_interval_s: float = 1.0,
) -> dict:
    """Poll `getTransaction(hash)` until terminal status.
    Returns
        `{"status": "SUCCESS"|"FAILED"|"TIMEOUT", "error": str|None}`.
    """
    import time as _time

    deadline = _time.time() + timeout_s
    last_err: str | None = None

    while _time.time() < deadline:
        try:
            resp = server.get_transaction(tx_hash)
            # Status is a stellar-sdk enum; compare via .name to avoid imports.
            status_name = getattr(resp.status, "name", str(resp.status)).upper()
            if status_name == "SUCCESS":
                return {"status": "SUCCESS", "error": None}
            if status_name == "FAILED":
                err = getattr(resp, "result_xdr", None) or getattr(resp, "error", None)
                return {"status": "FAILED", "error": str(err) if err else "tx failed"}
        except Exception as e:
            last_err = f"{type(e).__name__}: {e}"
        _time.sleep(poll_interval_s)

    return {
        "status": "TIMEOUT",
        "error": last_err or f"no terminal status in {timeout_s}s",
    }


class SigningMode(Enum):
    """
    - "single" (default): legacy testnet path. The orchestrator hot key
      signs and submits in one shot.
    - "external": multisig / hardware-wallet path. Auto-sign helpers raise;
      callers must use the `build_unsigned_*` family + an out-of-band
      co-signing flow + `submit_signed_xdr()`.
    """

    SINGLE = "single"
    EXTERNAL = "external"


signing_mode = SigningMode(
    os.environ.get("ORCHESTRATOR_SIGNING_MODE", "single").lower()
)


def _build_unsigned_escrow_invoke(
    source_public: str,
    fn_name: str,
    params: list,
    rpc_url: str | None = None,
    escrow_contract: str | None = None,
    timeout_seconds: int = 300,
) -> dict:
    """Build + simulate + prepare a Soroban escrow call, returning unsigned XDR.

    No signing, no submission. The caller is responsible for collecting the
    required signatures (off-line, hardware-wallet, or multisig co-sign) and
    submitting via `submit_signed_xdr()`.
    """
    rpc_url = rpc_url or RPC_URL
    escrow_contract = escrow_contract or X402_ESCROW_CONTRACT

    server = SorobanServer(rpc_url)
    source_account = server.load_account(source_public)

    builder = TransactionBuilder(
        source_account=source_account,
        network_passphrase=NETWORK_PASSPHRASE,
        base_fee=100,
    )
    builder.append_invoke_contract_function_op(
        contract_id=escrow_contract,
        function_name=fn_name,
        parameters=params,
    )
    builder.set_timeout(timeout_seconds)
    tx = builder.build()

    sim = server.simulate_transaction(tx)
    if sim.error:
        raise RuntimeError(f"Soroban simulate error for {fn_name}: {sim.error}")

    tx = server.prepare_transaction(tx, sim)

    return {
        "unsigned_xdr": tx.to_xdr(),
        "function": fn_name,
        "escrow_contract": escrow_contract,
        "min_resource_fee": getattr(sim, "min_resource_fee", None),
    }


def submit_signed_xdr(signed_xdr: str, rpc_url: str | None = None) -> str:
    """Submit a fully-signed transaction envelope. Used with multisig flow."""
    from stellar_sdk import TransactionEnvelope

    rpc_url = rpc_url or RPC_URL
    server = SorobanServer(rpc_url)
    envelope = TransactionEnvelope.from_xdr(signed_xdr, NETWORK_PASSPHRASE)
    resp = server.send_transaction(envelope)
    return resp.hash


def _submit_escrow_invoke(
    orchestrator_kp: Keypair,
    fn_name: str,
    params: list,
    rpc_url: str | None = None,
    escrow_contract: str | None = None,
    wait_for_confirmation: bool = True,
) -> dict:
    """Build, simulate, sign, submit and (by default) wait for on-chain
    confirmation of a Soroban escrow contract call.

    Returns `{"hash": str, "status": "SUCCESS"|"FAILED"|"TIMEOUT"|"SUBMITTED",
    "error": str|None}`. Callers that want fire-and-forget (e.g. an
    interactive UI flow that polls separately) can pass
    `wait_for_confirmation=False` and get `status="SUBMITTED"`.

    Refuses to run when ORCHESTRATOR_SIGNING_MODE=external — in that mode the
    caller must use `_build_unsigned_escrow_invoke()` + `submit_signed_xdr()`.
    """
    if signing_mode == SigningMode.EXTERNAL:
        raise RuntimeError(
            f"Auto-signing for '{fn_name}' is disabled because "
            "ORCHESTRATOR_SIGNING_MODE=external. Use the build_unsigned_* "
            "helpers and submit the co-signed XDR via submit_signed_xdr()."
        )

    rpc_url = rpc_url or RPC_URL
    escrow_contract = escrow_contract or X402_ESCROW_CONTRACT

    server = SorobanServer(rpc_url)
    source_account = server.load_account(orchestrator_kp.public_key)

    builder = TransactionBuilder(
        source_account=source_account,
        network_passphrase=NETWORK_PASSPHRASE,
        base_fee=1000,  # Soroban prepare_transaction bumps from this base
    )
    builder.append_invoke_contract_function_op(
        contract_id=escrow_contract,
        function_name=fn_name,
        parameters=params,
    )
    builder.set_timeout(60)
    tx = builder.build()

    sim = server.simulate_transaction(tx)
    if sim.error:
        raise RuntimeError(f"Soroban simulate error for {fn_name}: {sim.error}")

    tx = server.prepare_transaction(tx, sim)
    tx.sign(orchestrator_kp)

    resp = server.send_transaction(tx)
    tx_hash = resp.hash

    if not wait_for_confirmation:
        return {"hash": tx_hash, "status": "SUBMITTED", "error": None}

    confirmation = _wait_for_tx_confirmation(server, tx_hash)
    return {
        "hash": tx_hash,
        "status": confirmation["status"],
        "error": confirmation["error"],
    }


# Unsigned-XDR builders (mainnet / multisig path)


def build_unsigned_refund(
    source_public: str,
    task_id: str,
    rpc_url: str | None = None,
    escrow_contract: str | None = None,
) -> dict:
    """Build an unsigned refund_to_client(task_id) tx for multisig co-signing."""
    return _build_unsigned_escrow_invoke(
        source_public=source_public,
        fn_name="refund_to_client",
        params=[scval.to_string(task_id)],
        rpc_url=rpc_url,
        escrow_contract=escrow_contract,
    )


def build_unsigned_settle(
    source_public: str,
    task_id: str,
    agent_amount_stroops: int,
    rpc_url: str | None = None,
    escrow_contract: str | None = None,
) -> dict:
    """Build an unsigned settle(task_id, agent_amount) tx for multisig co-signing."""
    return _build_unsigned_escrow_invoke(
        source_public=source_public,
        fn_name="settle",
        params=[
            scval.to_string(task_id),
            scval.to_int128(agent_amount_stroops),
        ],
        rpc_url=rpc_url,
        escrow_contract=escrow_contract,
    )


def build_unsigned_settle_team(
    source_public: str,
    task_id: str,
    splits: list[tuple[str, int]],
    rpc_url: str | None = None,
    escrow_contract: str | None = None,
) -> dict:
    """Build an unsigned settle_team tx for multisig co-signing."""
    split_scvals = [
        scval.to_struct(
            {"addr": scval.to_address(addr), "amount": scval.to_int128(amount)}
        )
        for addr, amount in splits
    ]
    return _build_unsigned_escrow_invoke(
        source_public=source_public,
        fn_name="settle_team",
        params=[
            scval.to_string(task_id),
            scval.to_vec(split_scvals),
        ],
        rpc_url=rpc_url,
        escrow_contract=escrow_contract,
    )


def refund_escrow(
    orchestrator_kp: Keypair,
    task_id: str,
    **kwargs,
) -> dict:
    """Refund escrowed USDC back to the client (permissionless after deadline).

    Submits and waits for on-chain confirmation. Returns
    `{"hash": str, "status": "SUCCESS"|"FAILED"|"TIMEOUT"|"SUBMITTED",
    "error": str|None}`.
    """
    params = [scval.to_string(task_id)]
    return _submit_escrow_invoke(orchestrator_kp, "refund_to_client", params, **kwargs)


def get_escrow_status(
    task_id: str,
    rpc_url: str | None = None,
    escrow_contract: str | None = None,
) -> dict:
    """Read escrow entry from the Soroban contract (read-only simulate, no signing).

    Returns a dict with keys: task_id, status, amount, deadline, client, agent.
    Returns None if escrow not found.
    """
    rpc_url = rpc_url or RPC_URL
    escrow_contract = escrow_contract or X402_ESCROW_CONTRACT

    server = SorobanServer(rpc_url)

    # Build a read-only transaction (any valid account works as source)
    # Use a zero-account since we only simulate, never submit
    from stellar_sdk import Account

    source = Account("GAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAWHF", 0)

    builder = TransactionBuilder(
        source_account=source,
        network_passphrase=NETWORK_PASSPHRASE,
        base_fee=100,
    )
    builder.append_invoke_contract_function_op(
        contract_id=escrow_contract,
        function_name="get_escrow",
        parameters=[scval.to_string(task_id)],
    )
    builder.set_timeout(30)
    tx = builder.build()

    sim = server.simulate_transaction(tx)
    if sim.error:
        return {"task_id": task_id, "status": "not_found", "error": str(sim.error)}

    # Parse the result — returns raw XDR for now
    # Full parsing requires matching the EscrowEntry struct from the contract
    if sim.results and len(sim.results) > 0:
        return {"task_id": task_id, "status": "found", "raw_xdr": sim.results[0].xdr}

    return {"task_id": task_id, "status": "not_found"}


def build_escrow_tx(
    client_public: str,
    agent_public: str,
    amount_stroops: int,
    deadline_unix: int,
    task_id: str,
    rpc_url: str | None = None,
    escrow_contract: str | None = None,
) -> dict:
    """Build an unsigned create_escrow transaction for the client to sign.

    Returns dict with: unsigned_xdr, task_id, deadline, amount_stroops.
    The client signs this with Freighter, then submits to Horizon.
    """
    rpc_url = rpc_url or RPC_URL
    escrow_contract = escrow_contract or X402_ESCROW_CONTRACT

    server = SorobanServer(rpc_url)
    source_account = server.load_account(client_public)

    builder = TransactionBuilder(
        source_account=source_account,
        network_passphrase=NETWORK_PASSPHRASE,
        base_fee=100,
    )
    builder.append_invoke_contract_function_op(
        contract_id=escrow_contract,
        function_name="create_escrow",
        parameters=[
            scval.to_address(client_public),  # client
            scval.to_address(agent_public),  # agent (orchestrator for generic)
            scval.to_int128(amount_stroops),  # amount
            scval.to_uint64(deadline_unix),  # deadline
            scval.to_string(task_id),  # task_id
        ],
    )
    builder.set_timeout(300)
    tx = builder.build()

    sim = server.simulate_transaction(tx)
    if sim.error:
        raise RuntimeError(f"Escrow tx simulation failed: {sim.error}")

    tx = server.prepare_transaction(tx, sim)

    return {
        "unsigned_xdr": tx.to_xdr(),
        "task_id": task_id,
        "deadline": deadline_unix,
        "amount_stroops": amount_stroops,
    }


def settle_escrow(
    orchestrator_kp: Keypair,
    task_id: str,
    agent_amount_stroops: int,
    **kwargs,
) -> dict:
    """Settle a single-agent escrow: agent gets agent_amount, client gets refund.

    Submits and waits for on-chain confirmation. Returns
    `{"hash": str, "status": "SUCCESS"|"FAILED"|"TIMEOUT"|"SUBMITTED",
    "error": str|None}`.
    """
    params = [
        scval.to_string(task_id),
        scval.to_int128(agent_amount_stroops),
    ]
    return _submit_escrow_invoke(orchestrator_kp, "settle", params, **kwargs)


def settle_team_escrow(
    orchestrator_kp: Keypair,
    task_id: str,
    splits: list[tuple[str, int]],
    **kwargs,
) -> dict:
    """Settle a team escrow: each agent gets their share, client gets refund.

    Args:
        splits: list of (agent_stellar_address, amount_stroops) tuples

    Submits and waits for on-chain confirmation. Returns
    `{"hash": str, "status": "SUCCESS"|"FAILED"|"TIMEOUT"|"SUBMITTED",
    "error": str|None}`.
    """
    # Build Vec<Split> as a Soroban Vec of structs
    split_scvals = []
    for addr, amount in splits:
        split_scvals.append(
            scval.to_struct(
                {"addr": scval.to_address(addr), "amount": scval.to_int128(amount)}
            )
        )

    params = [
        scval.to_string(task_id),
        scval.to_vec(split_scvals),
    ]
    return _submit_escrow_invoke(orchestrator_kp, "settle_team", params, **kwargs)
