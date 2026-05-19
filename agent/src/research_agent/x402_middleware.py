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


async def _verify_tx_on_horizon(
    tx_hash: str, pay_to: str, expected_amount: str, expected_asset: str = "USDC"
) -> dict:
    """Verify a payment transaction on Stellar Horizon. Fail-closed design.

    Only used as fallback when facilitator is down. Validates:
    - Transaction exists and succeeded
    - Contains a payment operation to the expected recipient
    - Amount meets or exceeds the expected amount
    - Asset matches (USDC)

    Returns wallet_address (source_account) from the transaction for auth binding.
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

        # Find a payment operation to the expected recipient
        for op in records:
            op_type = op.get("type", "")
            if op_type != "payment":
                continue

            # Classic payment op: validate destination, amount, asset
            if op.get("to") != pay_to:
                continue
            op_amount = float(op.get("amount", "0"))
            if op_amount < float(expected_amount):
                continue
            asset_code = op.get("asset_code", "")
            if expected_asset and asset_code != expected_asset:
                continue
            return {
                "success": True,
                "txHash": tx_hash,
                "nonce": tx_hash,
                "wallet_address": data.get("source_account", ""),
            }

        return {"success": False}


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
                        expected_amount=price_usdc,
                    )
                    if result and not result.get("success"):
                        import asyncio as _aio

                        await _aio.sleep(3)
                        result = await _verify_tx_on_horizon(
                            tx_hash,
                            pay_to=pay_to,
                            expected_amount=price_usdc,
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
