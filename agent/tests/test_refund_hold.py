"""A refund is a transfer of somebody else's money, so it needs a reason.

Two properties, both learned from one task:
`40f6f3daf4c546fda37c19afbe8bcc5f` holds 0.675 USDC in a live contract entry,
and on 2026-10-20 the only thing keeping the sweeper away from it was a *false*
`refunded = 1` that a test fixture had written onto the row. Every other
condition the sweeper checks was already true. Correcting the record — the
obviously right thing to do — would have armed the refund, and the money would
have left within one 60-second sweep of `ORCHESTRATOR_SECRET_KEY` being loaded.

1. `TaskStore.hold_refund` — a protection that is a fact on the row, not a
   side effect of a missing key, an unexpired deadline or a wrong status.
2. `refund_authorisation` — the sweeper's own refusal, re-checked per task, so
   that the absence of a business condition is never read as permission to act.

SQLite and doubles for the chain: both properties are about our bookkeeping and
the decision taken before any network call, so testnet would add minutes and
flakiness and verify nothing extra. The same SQL is exercised against
PostgreSQL in test_task_store_pg.py.
"""

import time

import pytest

from research_agent.task_store import TaskStore
from research_agent.refund_sweeper import (
    OUTCOME_REFUSED,
    STATUS_ENTRY_UNREADABLE,
    process_one_refund,
    refund_authorisation,
    sweep_expired_refunds,
)

#: The real escrow transaction of the task this file exists because of.
REAL_TX = "e6d6a1a06f1fd7b57d57aa8f1c96a63dae167f256ee2ecba343f2f20b08a7966"
REAL_TASK_ID = "40f6f3daf4c546fda37c19afbe8bcc5f"
REAL_WALLET = "GCKRS77C3OEUFPE4UEDYPPVS2I7357QNCBUMYTJDLR5PNQNBU2S26RNL"

DAY = 86_400


class FakeKeypair:
    """Present, loaded, usable. The protection must not need it to be absent."""

    public_key = "GAT6TN4C2ECKUMKZTTRQS65RATLH7W7FG3UYPBQEWTEWL5PET2WR3F6X"


@pytest.fixture
def store(tmp_path):
    s = TaskStore(db_path=str(tmp_path / "tasks.db"))
    yield s
    s.close()


def add_task(store, task_id, *, escrow_tx=REAL_TX, settled=0, refunded=0,
             status="pending", deadline_offset=-60, age_seconds=0):
    """A task the sweeper would pick up, unless a field says otherwise."""
    store.create_task(task_id, wallet_address=REAL_WALLET)
    store.update_task(
        task_id,
        escrow_tx=escrow_tx,
        settled=settled,
        refunded=refunded,
        status=status,
        deadline_unix=int(time.time()) + deadline_offset,
    )
    if age_seconds:
        store._execute(
            "UPDATE tasks SET created_at = ? WHERE task_id = ?",
            (time.time() - age_seconds, task_id),
        )
        store._commit()
    return store.get_task(task_id)


def never_called(*_args, **_kwargs):
    raise AssertionError("moved money on a task that was not authorised")


def chain_found(task_id):
    return {"task_id": task_id, "status": "found"}


def chain_gone(task_id):
    return {"task_id": task_id, "status": "not_found"}


# ---------------------------------------------------------------------------
# 1. The hold
# ---------------------------------------------------------------------------

class TestHoldKeepsTheSweeperAway:
    def test_a_held_task_never_enters_the_expired_queue(self, store):
        add_task(store, "held")
        assert "held" in [t["task_id"] for t in store.list_expired()]

        store.hold_refund("held", "0.675 USDC under investigation")

        assert "held" not in [t["task_id"] for t in store.list_expired()], (
            "a held escrow was offered to the sweeper"
        )

    def test_the_sweeper_does_not_touch_it_even_with_a_key_loaded(self, store):
        add_task(store, "held")
        store.hold_refund("held", "keep for the demo")

        result = sweep_expired_refunds(
            store, FakeKeypair(),
            refund_fn=never_called,
            status_fn=chain_found,
            retry_interval_seconds=0,
        )

        assert result["refunded"] == 0
        row = store.get_task("held")
        assert row["refunded"] == 0
        assert not row["refund_tx_hash"]
        assert row["refund_attempts"] == 0

    def test_process_one_refund_refuses_a_held_task_handed_to_it_directly(self, store):
        """Defence in depth: the filter and the payment are in different
        places, and only one of them moves money."""
        task = add_task(store, "held")
        store.hold_refund("held", "investigation")

        report = process_one_refund(
            FakeKeypair(),
            store.get_task("held"),
            store,
            refund_fn=never_called,
            status_fn=chain_found,
        )

        assert report["outcome"] == OUTCOME_REFUSED
        assert "refund hold is set" in report["reason"]
        assert "investigation" in report["reason"]

    def test_a_hold_outranks_an_unreadable_entry(self, store):
        """`not_found` is not authorisation either — not even to rewrite the
        row's status. The hold is checked before the chain is consulted."""
        add_task(store, "held")
        store.hold_refund("held", "investigation")

        report = process_one_refund(
            FakeKeypair(),
            store.get_task("held"),
            store,
            refund_fn=never_called,
            status_fn=chain_gone,
        )

        assert report["outcome"] == OUTCOME_REFUSED
        assert store.get_task("held")["status"] != STATUS_ENTRY_UNREADABLE

    def test_a_held_row_survives_cleanup_however_old(self, store):
        # And specifically: a held row whose own fields say the money already
        # went back. `_FINANCIALLY_OPEN_SQL` does not match it, so the hold is
        # the only thing preserving it.
        add_task(store, "held", age_seconds=30 * DAY, refunded=1, status="refunded")
        store.hold_refund("held", "false refunded=1, real money on chain")

        result = store.cleanup(max_age_seconds=DAY)

        assert store.get_task("held") is not None
        assert result["deleted"] == 0
        assert result["preserved_open"] == 1

    def test_the_hold_survives_reopening_the_database(self, store, tmp_path):
        """It is a column, not process state: a restart, a backup restore or a
        migration carries it."""
        add_task(store, "held")
        store.hold_refund("held", "investigation")

        reopened = TaskStore(db_path=str(tmp_path / "tasks.db"))
        try:
            assert reopened.is_refund_held("held") is True
            assert "held" not in [t["task_id"] for t in reopened.list_expired()]
        finally:
            reopened.close()

    def test_a_hold_is_visible(self, store):
        add_task(store, "held")
        add_task(store, "ordinary")
        store.hold_refund("held", "0.675 USDC, investigation open")

        holds = store.list_refund_holds()

        assert [h["task_id"] for h in holds] == ["held"]
        assert holds[0]["refund_hold_reason"] == "0.675 USDC, investigation open"
        assert holds[0]["escrow_tx"] == REAL_TX

    def test_it_also_shows_up_in_the_reconciliation_queue(self, store):
        add_task(store, "held", age_seconds=2 * DAY, refunded=1, status="refunded")
        store.hold_refund("held", "investigation")

        assert "held" in [
            t["task_id"] for t in store.list_financially_open(older_than_seconds=DAY)
        ]


class TestHoldRequiresAReason:
    @pytest.mark.parametrize("reason", ["", "   ", None])
    def test_holding_without_one_is_refused(self, store, reason):
        add_task(store, "t")
        with pytest.raises(ValueError):
            store.hold_refund("t", reason)
        assert store.is_refund_held("t") is False

    @pytest.mark.parametrize("reason", ["", "   ", None])
    def test_releasing_without_one_is_refused(self, store, reason):
        add_task(store, "t")
        store.hold_refund("t", "investigation")
        with pytest.raises(ValueError):
            store.release_refund_hold("t", reason)
        assert store.is_refund_held("t") is True, "a hold was lifted with no record"

    def test_a_missing_task_reports_it_rather_than_pretending(self, store):
        assert store.hold_refund("does-not-exist", "x") is False
        assert store.release_refund_hold("does-not-exist", "x") is False


class TestReleaseIsDeliberate:
    def test_releasing_re_arms_the_sweeper(self, store):
        add_task(store, "t")
        store.hold_refund("t", "investigation")
        assert "t" not in [x["task_id"] for x in store.list_expired()]

        store.release_refund_hold("t", "investigation closed, refund approved")

        assert "t" in [x["task_id"] for x in store.list_expired()]
        result = sweep_expired_refunds(
            store, FakeKeypair(),
            refund_fn=lambda kp, tid: {"hash": REAL_TX, "status": "SUCCESS", "error": None},
            status_fn=chain_found,
            retry_interval_seconds=0,
        )
        assert result["refunded"] == 1

    def test_the_row_keeps_the_last_thing_a_person_said(self, store):
        add_task(store, "t")
        store.hold_refund("t", "investigation")
        store.release_refund_hold("t", "Andrea approved on 2026-10-21")

        row = store.get_task("t")
        assert row["refund_hold"] == 0
        assert row["refund_hold_reason"] == "released: Andrea approved on 2026-10-21"

    def test_nothing_else_lifts_a_hold(self, store):
        """Not a status change, not a settle flag, not housekeeping."""
        add_task(store, "t")
        store.hold_refund("t", "investigation")

        store.update_task("t", status="pending", refunded=0, settled=0,
                          refund_attempts=0, refund_last_error="")
        store.cleanup(max_age_seconds=0)
        store.mark_refund_attempted("t")

        assert store.is_refund_held("t") is True


# ---------------------------------------------------------------------------
# 2. The business condition
# ---------------------------------------------------------------------------

class TestAbsenceIsNotAuthorisation:
    """Each case: a condition that is missing, not a condition that forbids."""

    def test_no_deadline_at_all(self, store):
        task = add_task(store, "t")
        store.update_task("t", deadline_unix=0)
        assert "no deadline" in refund_authorisation(store.get_task("t"))

    def test_a_deadline_still_in_the_future(self, store):
        add_task(store, "t", deadline_offset=3600)
        assert "deadline has not passed" in refund_authorisation(store.get_task("t"))

    def test_no_escrow_transaction(self, store):
        add_task(store, "t", escrow_tx="")
        assert "no funds were ever locked" in refund_authorisation(store.get_task("t"))

    def test_an_escrow_transaction_that_is_not_one(self, store):
        # The exact values left on rows by old test runs.
        for bogus in ("tx", "abc", "mock_escrow_tx"):
            add_task(store, bogus, escrow_tx=bogus)
            blocked = refund_authorisation(store.get_task(bogus))
            assert blocked is not None and "transaction hash" in blocked

    def test_already_settled(self, store):
        add_task(store, "t", settled=1, status="pending")
        assert "settled" in refund_authorisation(store.get_task("t"))

    def test_already_refunded(self, store):
        add_task(store, "t", refunded=1, status="pending")
        assert "already marked refunded" in refund_authorisation(store.get_task("t"))

    @pytest.mark.parametrize(
        "status",
        ["refunded", "settled", "complete", "refund_failed", STATUS_ENTRY_UNREADABLE],
    )
    def test_a_terminal_status(self, store, status):
        add_task(store, "t", status=status)
        assert "terminal status" in refund_authorisation(store.get_task("t"))

    def test_an_empty_row_is_refused_rather_than_accepted_by_default(self):
        """The shape a bug upstream would hand it. Nothing present, nothing
        authorised."""
        assert refund_authorisation({"task_id": "x"}) is not None

    def test_a_refusal_costs_nothing(self, store):
        """Not a failure: no attempt counter, no dead-letter, no stamp."""
        add_task(store, "t", deadline_offset=3600)

        result = sweep_expired_refunds(
            store, FakeKeypair(),
            refund_fn=never_called,
            status_fn=chain_found,
            retry_interval_seconds=0,
        )

        row = store.get_task("t")
        assert result["failed"] == 0
        assert result["dead_letter"] == 0
        assert row["refund_attempts"] == 0
        assert row["status"] == "pending"
        assert not row["refund_attempted_at"]

    def test_the_guard_is_narrow(self, store):
        """All conditions present → the refund happens. A guard that blocks
        everything protects nothing worth protecting."""
        add_task(store, "ordinary")
        assert refund_authorisation(store.get_task("ordinary")) is None

        result = sweep_expired_refunds(
            store, FakeKeypair(),
            refund_fn=lambda kp, tid: {"hash": REAL_TX, "status": "SUCCESS", "error": None},
            status_fn=chain_found,
            retry_interval_seconds=0,
        )

        assert result == {
            "refunded": 1, "unreadable": 0, "refused": 0,
            "failed": 0, "dead_letter": 0, "skipped": 0,
        }


# ---------------------------------------------------------------------------
# The reason this file exists
# ---------------------------------------------------------------------------

def test_correcting_the_contaminated_row_no_longer_arms_the_refund(store):
    """The decision that was blocked, now safe to take.

    The real row carries `refunded = 1` and `status = 'refunded'`, written by a
    test fixture. The contract says otherwise: entry `40f6f3da…` is live and
    holds 0.675 USDC. Correcting the row is right — a database that claims a
    refund nobody can show is worse than one that admits it does not know.

    But under the old code, those two false fields were the *only* unmet
    condition. Correcting them armed an automated transfer. That is the
    contradiction this test closes: hold first, then correct, and the sweeper
    still declines — on the hold, for a stated reason, not by accident.
    """
    add_task(
        store, REAL_TASK_ID,
        age_seconds=5 * DAY,
        refunded=1,            # false: contract entry is live
        status="refunded",     # false
    )

    # Step 1, the protection — before touching anything else.
    assert store.hold_refund(
        REAL_TASK_ID,
        "0.675 USDC live on chain; row contaminated by a test fixture; "
        "investigation open, no refund authorised",
    ) is True

    # Step 2, the correction Andrea has to approve.
    store.update_task(REAL_TASK_ID, refunded=0, status="pending")

    # Every other condition the sweeper checks is now true.
    row = store.get_task(REAL_TASK_ID)
    assert row["settled"] == 0 and row["refunded"] == 0
    assert row["deadline_unix"] < time.time()
    assert len(row["escrow_tx"]) == 64

    # And it is still not touched.
    assert REAL_TASK_ID not in [t["task_id"] for t in store.list_expired()]
    result = sweep_expired_refunds(
        store, FakeKeypair(),
        refund_fn=never_called,
        status_fn=chain_found,
        retry_interval_seconds=0,
    )
    assert result["refunded"] == 0

    # Housekeeping does not delete it either, five days old and corrected.
    store.cleanup(max_age_seconds=DAY)
    assert store.get_task(REAL_TASK_ID) is not None

    # Handed straight to the payment function, it is refused by name.
    report = process_one_refund(
        FakeKeypair(), store.get_task(REAL_TASK_ID), store,
        refund_fn=never_called, status_fn=chain_found,
    )
    assert report["outcome"] == OUTCOME_REFUSED
    assert "investigation open" in report["reason"]
