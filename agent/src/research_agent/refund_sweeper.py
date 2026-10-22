from __future__ import annotations

import time
from typing import Any, Callable

from .task_store import TaskStore


# Bounded retries so a permanently-broken refund does not get attempted on every 60s sweep until the heat death of the universe.
MAX_REFUND_ATTEMPTS = 5


RefundFn = Callable[[Any, str], dict]
StatusFn = Callable[[str], dict]


REASON_DEADLINE_EXPIRED = "deadline_expired"
REASON_RECONCILED_GONE = "reconciled_chain_gone"

#: Terminal state for a task whose escrow entry can no longer be read on-chain.
#:
#: NOT the same as refunded. `get_escrow` traps identically whether the entry
#: was resolved and removed or merely **evicted because its TTL ran out**, and
#: an evicted entry can still have funds sitting in the contract. Marking those
#: "refunded" asserts that money went back to somebody when nobody can show
#: that it did.
#:
#: Measured, 2026-10-20: escrows created in May 2026 return `not_found` while
#: `usdc-escrow` still holds 0.745 USDC.
#:
#: The sweeper stops retrying these — repeated calls against an evicted entry
#: cannot succeed — but it records them as needing a human rather than as
#: settled business. Recovery means restoring the archived entry first, which
#: is a separate operation with its own fees.
STATUS_ENTRY_UNREADABLE = "escrow_entry_unreadable"

#: Outcome for a task the sweeper declined to act on. Not a failure: nothing
#: was attempted, nothing is retried, no counter moves.
OUTCOME_REFUSED = "refused"

#: A Stellar transaction hash is 64 hex characters. A row carrying anything
#: else never locked funds, so there is no escrow for a refund to return.
_TX_HASH_LENGTH = 64


def refund_authorisation(task: dict, now: float | None = None) -> str | None:
    """Return why this task must NOT be refunded, or None if it may be.

    The guard the sweeper applies to every task before it touches the network,
    regardless of which query produced the task. `list_expired` already filters
    on these conditions; this re-checks them on the row itself, because the
    filter and the payment live in different places and only one of them moves
    money.

    What it is defending against, concretely:

    * **A hold.** An operator said hands off this escrow. That is an explicit
      instruction and it outranks every automated condition below.
    * **An absent business condition.** A refund is authorised by *one* thing:
      a deadline that exists and has passed, on a task that locked real funds
      and has neither settled nor refunded. A row with no deadline, a deadline
      in the future, or no escrow transaction has nothing to authorise a
      transfer — and the absence of a condition is not permission to act. An
      unreadable entry is likewise not permission: it is handled separately,
      and never recorded as a refund.
    * **A terminal state.** Already refunded, already settled, dead-lettered,
      or flagged for manual reconciliation.

    Returned as a string so the caller can record exactly which condition was
    missing instead of logging "skipped".
    """
    now = time.time() if now is None else now

    if task.get("refund_hold"):
        reason = (task.get("refund_hold_reason") or "no reason recorded").strip()
        return f"refund hold is set: {reason}"

    status = (task.get("status") or "").strip()
    if status in (
        "refunded",
        "settled",
        "complete",
        "refund_failed",
        STATUS_ENTRY_UNREADABLE,
    ):
        return f"terminal status {status!r}"

    if int(task.get("settled") or 0):
        return "task is marked settled: the money went to the agent"
    if int(task.get("refunded") or 0):
        return "task is already marked refunded"

    escrow_tx = (task.get("escrow_tx") or "").strip()
    if len(escrow_tx) != _TX_HASH_LENGTH:
        return (
            "no escrow transaction on the row, so no funds were ever locked"
            if not escrow_tx
            else f"escrow_tx is not a transaction hash ({len(escrow_tx)} chars)"
        )

    deadline = float(task.get("deadline_unix") or 0)
    if deadline <= 0:
        return "no deadline recorded: nothing has expired"
    if deadline >= now:
        return f"deadline has not passed ({int(deadline - now)}s remaining)"

    return None


def process_one_refund(
    orchestrator_kp: Any,
    task: dict,
    store: Any,
    refund_fn: RefundFn,
    status_fn: StatusFn,
    max_attempts: int = MAX_REFUND_ATTEMPTS,
    reason: str = REASON_DEADLINE_EXPIRED,
) -> dict:
    """Attempt to refund a single expired task and reconcile the local DB.

    `reason` is recorded on the row (in `refund_reason`) when the refund
    succeeds or the task is reconciled. Defaults to `deadline_expired` since
    that is the path the sweeper uses; callers triggering a manual refund
    should pass a different reason.

    Returns a small report dict for the caller to log. Never raises — this
    function is a leaf called per-task by a long-running sweeper, so it must
    swallow per-task errors and keep going.
    """
    task_id = task["task_id"]
    attempts = int(task.get("refund_attempts") or 0)

    # Before the network, before any write to the row. A task that reaches here
    # without a valid business condition is a bug upstream, and the right
    # response to that bug is not to move somebody's money.
    blocked = refund_authorisation(task)
    if blocked is not None:
        return {
            "task_id": task_id,
            "outcome": OUTCOME_REFUSED,
            "reason": blocked,
            "hash": None,
        }

    try:
        chain_status = status_fn(task_id) or {}
    except Exception as e:
        chain_status = {"status": "unknown", "error": f"status_fn: {e}"}

    if chain_status.get("status") == "not_found":
        # The entry is unreadable. That is NOT proof it was refunded — see
        # STATUS_ENTRY_UNREADABLE. Stop retrying, because a call against an
        # entry the contract cannot read will not start working; but do not
        # record a payment nobody can point at.
        store.update_task(
            task_id,
            status=STATUS_ENTRY_UNREADABLE,
            refund_last_error=(
                "escrow entry not readable on-chain: resolved out-of-band, or "
                "evicted when its TTL expired. Funds may still be held by the "
                "contract. Needs manual reconciliation."
            ),
            refund_reason=REASON_RECONCILED_GONE,
        )
        return {
            "task_id": task_id,
            "outcome": "unreadable",
            "reason": REASON_RECONCILED_GONE,
            "hash": None,
        }

    try:
        result = refund_fn(orchestrator_kp, task_id) or {}
    except Exception as e:
        return _record_failure(
            store,
            task_id,
            attempts,
            max_attempts,
            error=f"{type(e).__name__}: {e}",
        )

    status = result.get("status")
    tx_hash = result.get("hash") or ""
    err = result.get("error")

    if status == "SUCCESS":
        store.update_task(
            task_id,
            refunded=1,
            status="refunded",
            refund_tx_hash=tx_hash,
            refund_last_error="",
            refund_reason=reason,
        )
        return {
            "task_id": task_id,
            "outcome": "refunded",
            "reason": reason,
            "hash": tx_hash,
        }

    return _record_failure(
        store,
        task_id,
        attempts,
        max_attempts,
        error=f"status={status} hash={tx_hash} error={err}",
    )


def _record_failure(
    store, task_id: str, attempts: int, max_attempts: int, error: str
) -> dict:
    new_attempts = attempts + 1
    fields: dict = {
        "refund_attempts": new_attempts,
        "refund_last_error": error[:500],
    }
    if new_attempts >= max_attempts:
        fields["status"] = "refund_failed"

    store.update_task(task_id, **fields)
    return {
        "task_id": task_id,
        "outcome": "dead_letter" if new_attempts >= max_attempts else "retry_scheduled",
        "attempts": new_attempts,
        "error": error,
    }


def sweep_expired_refunds(
    store: TaskStore,
    orchestrator_kp: Any,
    refund_fn: RefundFn,
    status_fn: StatusFn,
    max_per_sweep: int = 10,
    retry_interval_seconds: float = 600.0,
    max_attempts: int = MAX_REFUND_ATTEMPTS,
) -> dict:
    """Process expired escrow refunds for one sweep cycle.

    For each eligible task:
      1. Stamps `refund_attempted_at = now()` BEFORE calling the per-task
         processor. If the process crashes here, the next sweep cycle's
         `list_expired(refund_retry_after=retry_interval_seconds)` filter
         will exclude this task for `retry_interval_seconds`.
      2. Delegates the network-touching work to `process_one_refund`, which
         handles reconciliation + dead-letter + structured outcome.

    Caps total refunds attempted per call at `max_per_sweep` so one cycle
    can't exhaust the orchestrator's XLM if many escrows expire at once.

    Returns:
        {
          "refunded":   int,  # process_one_refund outcome=refunded
          "unreadable": int,  # entry not readable on-chain; NOT a refund
          "refused":    int,  # held, or no valid business condition
          "failed":     int,  # any non-success outcome (includes dead-letter)
          "dead_letter":int,  # subset of failed that hit max_attempts
          "skipped":    int,  # eligible tasks beyond max_per_sweep
        }
    """
    eligible = store.list_expired(refund_retry_after=retry_interval_seconds)
    to_process = eligible[:max_per_sweep]
    skipped = max(0, len(eligible) - max_per_sweep)

    refunded = 0
    unreadable = 0
    refused = 0
    failed = 0
    dead_letter = 0

    for task in to_process:
        task_id = task["task_id"]

        # Checked before the attempt is stamped: a refused task was never
        # attempted, and stamping it would push its (hypothetical) next attempt
        # behind a retry window it does not need.
        blocked = refund_authorisation(task)
        if blocked is not None:
            refused += 1
            continue

        store.mark_refund_attempted(task_id)

        report = process_one_refund(
            orchestrator_kp,
            task,
            store,
            refund_fn=refund_fn,
            status_fn=status_fn,
            max_attempts=max_attempts,
        )

        outcome = report.get("outcome")
        if outcome == "refunded":
            refunded += 1
        elif outcome == "unreadable":
            unreadable += 1
        elif outcome == OUTCOME_REFUSED:
            refused += 1
        elif outcome == "dead_letter":
            dead_letter += 1
            failed += 1
        else:
            failed += 1

    return {
        "refunded": refunded,
        # Counted apart from `refunded` on purpose: these are tasks whose money
        # nobody can account for, and folding them into a refund total is how
        # that stops being visible.
        "unreadable": unreadable,
        # Tasks the guard declined — a hold, or a missing business condition.
        # Not failures: nothing was attempted. A nonzero count on a quiet
        # system means something upstream is offering tasks it should not.
        "refused": refused,
        "failed": failed,
        "dead_letter": dead_letter,
        "skipped": skipped,
    }
