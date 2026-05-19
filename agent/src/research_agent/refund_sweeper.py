from __future__ import annotations

from typing import Any, Callable

from .task_store import TaskStore


# Bounded retries so a permanently-broken refund does not get attempted on every 60s sweep until the heat death of the universe.
MAX_REFUND_ATTEMPTS = 5


RefundFn = Callable[[Any, str], dict]
StatusFn = Callable[[str], dict]


REASON_DEADLINE_EXPIRED = "deadline_expired"
REASON_RECONCILED_GONE = "reconciled_chain_gone"


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

    try:
        chain_status = status_fn(task_id) or {}
    except Exception as e:
        chain_status = {"status": "unknown", "error": f"status_fn: {e}"}

    if chain_status.get("status") == "not_found":
        store.update_task(
            task_id,
            refunded=1,
            status="refunded",
            refund_last_error="",
            refund_reason=REASON_RECONCILED_GONE,
        )
        return {
            "task_id": task_id,
            "outcome": "reconciled",
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
          "reconciled": int,  # process_one_refund outcome=reconciled
          "failed":     int,  # any non-success outcome (includes dead-letter)
          "dead_letter":int,  # subset of failed that hit max_attempts
          "skipped":    int,  # eligible tasks beyond max_per_sweep
        }
    """
    eligible = store.list_expired(refund_retry_after=retry_interval_seconds)
    to_process = eligible[:max_per_sweep]
    skipped = max(0, len(eligible) - max_per_sweep)

    refunded = 0
    reconciled = 0
    failed = 0
    dead_letter = 0

    for task in to_process:
        task_id = task["task_id"]
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
        elif outcome == "reconciled":
            reconciled += 1
        elif outcome == "dead_letter":
            dead_letter += 1
            failed += 1
        else:
            failed += 1

    return {
        "refunded": refunded,
        "reconciled": reconciled,
        "failed": failed,
        "dead_letter": dead_letter,
        "skipped": skipped,
    }
