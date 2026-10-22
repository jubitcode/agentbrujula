from __future__ import annotations

import base64
import json
import os
from typing import Callable

import httpx
from fastapi import Request, Response, HTTPException

from research_agent.x402_config import (
    X402_FACILITATOR_URL,
    X402_NETWORK,
    X402_USDC_ADDRESS,
)

from research_agent.stellar_config import HORIZON_URL

BUDGET_MARGIN = float(os.getenv("BUDGET_MARGIN", "1.5"))

_store = None


def _get_store():
    """Lazy-init the TaskStore (avoids circular import at module load)."""
    global _store
    if _store is None:
        from research_agent.task_store import TaskStore

        _store = TaskStore()
    return _store


def _check_and_record_nonce(nonce: str | None) -> bool:
    if not nonce:
        return True

    store = _get_store()
    return store.claim_nonce(nonce)


async def _call_facilitator_verify(
    payment_signature: str,
    payment_required: str,
) -> dict:
    """
    Call the x402 facilitator's /verify endpoint.
    Returns dict with at least {success: bool}.
    Raises on network errors (caller handles).
    """
    async with httpx.AsyncClient(timeout=5.0) as client:
        resp = await client.post(
            f"{X402_FACILITATOR_URL}/verify",
            json={
                "paymentSignature": payment_signature,
                "paymentRequired": payment_required,
            },
        )
        resp.raise_for_status()
        return resp.json()


def _transfer_to_recipient(
    op: dict, pay_to: str, expected_amount: float, expected_asset: str
) -> dict | None:
    """Find a settled transfer to `pay_to` inside one operation.

    Horizon reports what a Soroban invocation actually moved in
    `asset_balance_changes`: the asset, the amount, and both ends of the
    transfer. That record is produced from the transaction's own result, so it
    is the right place to read an amount from — unlike the request body, which
    the payer controls.

    Handles both shapes the escrow flow produces:
      - `invoke_host_function` (type_i 24), where USDC moves into the escrow
        contract and the amount appears in `asset_balance_changes`;
      - the classic `payment` operation, kept for flat x402 payments that do
        not go through a contract.

    Returns the matching transfer, or None.
    """
    # Soroban: read the balance change the ledger recorded.
    for change in op.get("asset_balance_changes") or []:
        if change.get("type") != "transfer":
            continue
        if change.get("to") != pay_to:
            continue
        if expected_asset and change.get("asset_code") != expected_asset:
            continue
        try:
            moved = float(change.get("amount", "0"))
        except (TypeError, ValueError):
            continue
        if moved < expected_amount:
            continue
        return {"amount": moved, "payer": change.get("from", "")}

    # Classic payment operation.
    if op.get("type") == "payment" and op.get("to") == pay_to:
        if not expected_asset or op.get("asset_code") == expected_asset:
            try:
                moved = float(op.get("amount", "0"))
            except (TypeError, ValueError):
                return None
            if moved >= expected_amount:
                return {"amount": moved, "payer": op.get("from", "")}

    return None


async def _verify_tx_on_horizon(
    tx_hash: str, pay_to: str, expected_amount: str, expected_asset: str = "USDC"
) -> dict:
    """Verify a payment transaction on Stellar Horizon. Fail-closed design.

    Used as the fallback when the facilitator is unreachable. Validates:
    - the transaction exists and succeeded;
    - it moved `expected_asset` to `pay_to`;
    - the amount that actually moved meets or exceeds `expected_amount`.

    **Every one of those facts is read from Horizon**, never from the caller's
    headers. The caller supplies only the transaction hash, which is useless
    unless the transaction on the network says what the caller claims.

    Returns `wallet_address` — the account the funds came from — for auth
    binding. That is the payer, which is not always the transaction's source
    account: a sponsored or multi-operation transaction can be submitted by a
    different account than the one being debited.

    Horizon is network-specific by URL, so a transaction from the other network
    simply is not found here. That is the network separation; see x402_config
    for the check that keeps the two settings from disagreeing.
    """
    async with httpx.AsyncClient(timeout=10.0) as client:
        # Get transaction details
        resp = await client.get(f"{HORIZON_URL}/transactions/{tx_hash}")
        if resp.status_code != 200:
            return {"success": False}

        data = resp.json()
        if not data.get("successful", False):
            return {"success": False}

        # Get transaction operations to validate payment details
        ops_resp = await client.get(f"{HORIZON_URL}/transactions/{tx_hash}/operations")
        if ops_resp.status_code != 200:
            return {"success": False}

        ops_data = ops_resp.json()
        records = ops_data.get("_embedded", {}).get("records", [])

        try:
            required = float(expected_amount)
        except (TypeError, ValueError):
            # An unparseable price must not become "no minimum".
            return {"success": False, "reason": "bad_expected_amount"}

        for op in records:
            transfer = _transfer_to_recipient(op, pay_to, required, expected_asset)
            if transfer is None:
                continue
            return {
                "success": True,
                "txHash": tx_hash,
                "nonce": tx_hash,
                # The payer, falling back to the submitter when Horizon did not
                # report one (classic payments always do; be defensive anyway).
                "wallet_address": transfer["payer"] or data.get("source_account", ""),
                "amount_paid": f"{transfer['amount']:.7f}",
            }

        return {"success": False, "reason": "no_matching_transfer"}


def x402_gate(
    price_usdc: str,
    pay_to: str,
    estimate_fn: Callable | None = None,
) -> Callable:
    """
    Returns a FastAPI Depends-compatible callable that gates the endpoint.

    Args:
        price_usdc: Flat price in USDC (used when estimate_fn is None)
        pay_to: Stellar address to receive payment (escrow contract)
        estimate_fn: Optional callable(request) -> dict with keys:
            estimated_cost: str (USDC), rate_breakdown: list[dict]
            If provided, 402 includes budget ceiling instead of flat price.
    """

    async def _gate(request: Request, response: Response):
        from research_agent.x402_config import X402_ENABLED as enabled

        if not enabled:
            return

        if estimate_fn is not None:
            try:
                body = await request.json()
                estimate = estimate_fn(body)
                est_cost = float(estimate["estimated_cost"])
                ceiling = str(round(est_cost * BUDGET_MARGIN, 7))
                payment_required_payload = {
                    # `price` is the field a generic x402 client reads, so it is
                    # always present. With an estimator it carries the ceiling —
                    # the most the caller can be charged — while the estimate and
                    # the per-tool breakdown go alongside as detail. Leaving it
                    # out would make this endpoint unreadable to any x402 client
                    # that does not know about Brújula's own fields.
                    "price": ceiling,
                    "estimated_cost": estimate["estimated_cost"],
                    "budget_ceiling": ceiling,
                    "rate_breakdown": estimate.get("rate_breakdown", []),
                    "network": X402_NETWORK,
                    "payTo": pay_to,
                    "token": X402_USDC_ADDRESS,
                    "facilitatorUrl": X402_FACILITATOR_URL,
                }
            except Exception:
                payment_required_payload = {
                    "price": price_usdc,
                    "network": X402_NETWORK,
                    "payTo": pay_to,
                    "token": X402_USDC_ADDRESS,
                    "facilitatorUrl": X402_FACILITATOR_URL,
                }
        else:
            payment_required_payload = {
                "price": price_usdc,
                "network": X402_NETWORK,
                "payTo": pay_to,
                "token": X402_USDC_ADDRESS,
                "facilitatorUrl": X402_FACILITATOR_URL,
            }
        payment_required_b64 = base64.b64encode(
            json.dumps(payment_required_payload).encode()
        ).decode()

        # Verify against what this request was actually quoted. When an
        # estimator ran, the client was told to lock `budget_ceiling`, which is
        # higher than the flat `price_usdc` floor — checking the floor instead
        # would accept a payment smaller than the one we asked for.
        quoted_minimum = payment_required_payload.get(
            "budget_ceiling", payment_required_payload.get("price", price_usdc)
        )

        payment_sig = request.headers.get("PAYMENT-SIGNATURE")

        if not payment_sig:
            raise HTTPException(
                status_code=402,
                detail="Payment Required",
                headers={"PAYMENT-REQUIRED": payment_required_b64},
            )

        try:
            base64.b64decode(payment_sig)
        except Exception:
            raise HTTPException(
                status_code=400, detail="Malformed PAYMENT-SIGNATURE header"
            )

        result = None
        try:
            result = await _call_facilitator_verify(payment_sig, payment_required_b64)
        except Exception:
            try:
                sig_data = json.loads(base64.b64decode(payment_sig))
                tx_hash = sig_data.get("txHash", "")
                if tx_hash:
                    result = await _verify_tx_on_horizon(
                        tx_hash,
                        pay_to=pay_to,
                        expected_amount=quoted_minimum,
                    )
                    if result and not result.get("success"):
                        import asyncio as _aio

                        await _aio.sleep(3)
                        result = await _verify_tx_on_horizon(
                            tx_hash,
                            pay_to=pay_to,
                            expected_amount=quoted_minimum,
                        )
            except (json.JSONDecodeError, Exception):
                pass

        if result is None:
            raise HTTPException(
                status_code=502,
                detail="Payment verification failed — facilitator and Horizon both unreachable",
            )

        if not result.get("success"):
            raise HTTPException(
                status_code=402,
                detail="Payment rejected",
                headers={"PAYMENT-REQUIRED": payment_required_b64},
            )

        nonce = result.get("nonce")
        if not _check_and_record_nonce(nonce):
            raise HTTPException(status_code=403, detail="Replay detected")

        try:
            sig_data = json.loads(base64.b64decode(payment_sig))
            request.state.escrow_task_id = sig_data.get("task_id", "")
        except Exception:
            request.state.escrow_task_id = ""
        request.state.wallet_address = result.get("wallet_address", "")
        request.state.tx_hash = result.get("txHash", "")

        payment_response = base64.b64encode(
            json.dumps(
                {
                    "success": True,
                    "txHash": result.get("txHash", ""),
                }
            ).encode()
        ).decode()
        response.headers["PAYMENT-RESPONSE"] = payment_response

    return _gate
