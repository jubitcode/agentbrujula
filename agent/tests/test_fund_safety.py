"""Money must never be lost by housekeeping, and never be claimed as returned.

These reproduce the three situations that cost 0.070 USDC on testnet — an
amount the contract still holds and that no task row can account for, because
the rows that would have named them were deleted by age.

Each test fixes one property:

1. A hire whose escrow is still live survives cleanup, however old it is.
2. An entry the contract can no longer read is never recorded as refunded.
3. A task marked `escrow_entry_unreadable` stops being retried, and stays.

They use a temporary SQLite store and doubles for the chain: the properties are
about our own bookkeeping, and tying them to testnet would make them slow and
non-deterministic for no gain.
"""

import time

import pytest

from research_agent.task_store import TaskStore
from research_agent.refund_sweeper import process_one_refund, sweep_expired_refunds

#: A real Stellar transaction hash: 64 hex characters. The shape is what tells
#: a hire that actually locked funds from a row left behind by a test.
REAL_TX = "e6d6a1a06f1fd7b57d57aa8f1c96a63dae167f256ee2ecba343f2f20b08a7966"
REAL_WALLET = "GCKRS77C3OEUFPE4UEDYPPVS2I7357QNCBUMYTJDLR5PNQNBU2S26RNL"
#: The task whose row is contaminated and whose escrow is live.
REAL_TASK_ID_040 = "40f6f3daf4c546fda37c19afbe8bcc5f"

DAY = 86_400


class FakeKeypair:
    public_key = "GORCHESTRATOR"


@pytest.fixture
def store(tmp_path):
    s = TaskStore(db_path=str(tmp_path / "tasks.db"))
    yield s
    s.close()


def add_task(store, task_id, *, age_seconds=0, escrow_tx=REAL_TX, settled=0,
             refunded=0, status="pending", deadline_offset=-60):
    """Insert a task directly, so its age and state can be set exactly."""
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


# ---------------------------------------------------------------------------
# 1. A live escrow older than the cleanup window
# ---------------------------------------------------------------------------

class TestCleanupNeverDropsOpenEscrows:
    def test_a_three_day_old_unsettled_hire_survives(self, store):
        # The exact shape that lost the 0.070 USDC: a real escrow transaction,
        # neither settled nor refunded, older than the 24h window.
        add_task(store, "live-escrow", age_seconds=3 * DAY)

        result = store.cleanup(max_age_seconds=DAY)

        assert store.get_task("live-escrow") is not None, (
            "deleted the only local record of an escrow that may still hold funds"
        )
        assert result["deleted"] == 0
        assert result["preserved_open"] == 1

    def test_settled_and_refunded_tasks_are_still_cleaned(self, store):
        # The money moved, so the row is ordinary housekeeping. Preserving
        # everything forever would just be a different bug.
        #
        # The refunded one carries a real transaction hash, because that is
        # what a row looks like after an actual refund — and because without
        # one it is preserved instead, by `_UNPROVEN_REFUND_SQL`. A fixture
        # that left it empty was testing the deletion of a claim nobody can
        # demonstrate.
        add_task(store, "settled-old", age_seconds=3 * DAY, settled=1)
        add_task(store, "refunded-old", age_seconds=3 * DAY, refunded=1)
        store.update_task("refunded-old", refund_tx_hash=REAL_TX)

        result = store.cleanup(max_age_seconds=DAY)

        assert result["deleted"] == 2
        assert store.get_task("settled-old") is None
        assert store.get_task("refunded-old") is None

    def test_a_task_that_never_locked_funds_is_cleaned(self, store):
        # No transaction hash means nothing ever reached the network, so there
        # is no escrow to protect.
        add_task(store, "never-paid", age_seconds=3 * DAY, escrow_tx="")

        assert store.cleanup(max_age_seconds=DAY)["deleted"] == 1
        assert store.get_task("never-paid") is None

    def test_a_fabricated_hash_does_not_buy_protection(self, store):
        # Rows left by tests carry values like "tx" or "mock_escrow_tx". If
        # those counted as escrows, the bad rows would become immortal.
        add_task(store, "fake-hash", age_seconds=3 * DAY, escrow_tx="mock_escrow_tx")

        assert store.cleanup(max_age_seconds=DAY)["deleted"] == 1

    def test_recent_open_escrows_are_not_counted_as_preserved(self, store):
        # Only tasks old enough to be cleanup candidates count, otherwise the
        # warning number is noise on every sweep.
        add_task(store, "fresh", age_seconds=60)

        result = store.cleanup(max_age_seconds=DAY)
        assert result["preserved_open"] == 0

    def test_open_escrows_are_listable_for_reconciliation(self, store):
        # Preserving rows is only useful if somebody can find them.
        add_task(store, "old-open", age_seconds=3 * DAY)
        add_task(store, "old-settled", age_seconds=3 * DAY, settled=1)

        open_tasks = store.list_financially_open(older_than_seconds=DAY)

        assert [t["task_id"] for t in open_tasks] == ["old-open"]


# ---------------------------------------------------------------------------
# 2. An entry archived by TTL
# ---------------------------------------------------------------------------

class TestTtlArchivedEntry:
    def test_funds_are_never_marked_returned(self, store):
        """`get_escrow` traps the same way for a resolved entry and an evicted
        one, so `not_found` is not evidence that anybody was paid back."""
        add_task(store, "ttl-gone", age_seconds=3 * DAY)

        process_one_refund(
            FakeKeypair(),
            store.get_task("ttl-gone"),
            store,
            refund_fn=lambda kp, tid: {"hash": "x", "status": "SUCCESS", "error": None},
            status_fn=lambda tid: {"status": "not_found"},
        )

        fresh = store.get_task("ttl-gone")
        assert fresh["refunded"] == 0, "claimed a refund that cannot be demonstrated"
        assert fresh["status"] == "escrow_entry_unreadable"
        assert not fresh["refund_tx_hash"], "recorded a transaction hash for a refund that never happened"

    def test_the_record_survives_cleanup_afterwards(self, store):
        # The whole sequence: escrow goes unreadable, then housekeeping runs.
        # This is where the 0.070 USDC disappeared from the books.
        add_task(store, "ttl-gone", age_seconds=3 * DAY)
        process_one_refund(
            FakeKeypair(),
            store.get_task("ttl-gone"),
            store,
            refund_fn=lambda kp, tid: {"hash": "x", "status": "SUCCESS", "error": None},
            status_fn=lambda tid: {"status": "not_found"},
        )

        store.cleanup(max_age_seconds=DAY)

        assert store.get_task("ttl-gone") is not None, (
            "the task flagged for manual reconciliation was deleted by age"
        )

    def test_it_leaves_an_actionable_trail(self, store):
        add_task(store, "ttl-gone", age_seconds=3 * DAY)
        process_one_refund(
            FakeKeypair(),
            store.get_task("ttl-gone"),
            store,
            refund_fn=lambda kp, tid: {"hash": "x", "status": "SUCCESS", "error": None},
            status_fn=lambda tid: {"status": "not_found"},
        )

        fresh = store.get_task("ttl-gone")
        assert "manual reconciliation" in fresh["refund_last_error"]
        assert fresh["escrow_tx"] == REAL_TX, "lost the link to the on-chain escrow"


# ---------------------------------------------------------------------------
# 3. No infinite retries
# ---------------------------------------------------------------------------

class TestUnreadableStopsRetrying:
    def test_it_drops_out_of_the_expired_queue(self, store):
        add_task(store, "unreadable", age_seconds=3 * DAY,
                 status="escrow_entry_unreadable")

        eligible = [t["task_id"] for t in store.list_expired()]

        assert "unreadable" not in eligible, (
            "would be retried every sweep cycle, forever"
        )

    def test_a_second_sweep_does_not_pick_it_up_again(self, store):
        """End to end: one sweep marks it, the next must leave it alone."""
        add_task(store, "ttl-gone", age_seconds=3 * DAY)

        calls = {"n": 0}

        def refund_fn(kp, tid):
            calls["n"] += 1
            return {"hash": "x", "status": "SUCCESS", "error": None}

        first = sweep_expired_refunds(
            store, FakeKeypair(),
            refund_fn=refund_fn,
            status_fn=lambda tid: {"status": "not_found"},
            retry_interval_seconds=0,
        )
        second = sweep_expired_refunds(
            store, FakeKeypair(),
            refund_fn=refund_fn,
            status_fn=lambda tid: {"status": "not_found"},
            retry_interval_seconds=0,
        )

        assert first["unreadable"] == 1
        assert second["unreadable"] == 0, "picked the task up again on the next cycle"
        assert second["refunded"] == 0
        assert second["failed"] == 0
        assert calls["n"] == 0, "called refund on an entry the contract cannot read"

    def test_ordinary_expired_tasks_are_still_swept(self, store):
        # The exclusion must be narrow: a normal expired escrow still gets its
        # refund attempt.
        add_task(store, "ordinary", age_seconds=3 * DAY)

        result = sweep_expired_refunds(
            store, FakeKeypair(),
            refund_fn=lambda kp, tid: {"hash": REAL_TX, "status": "SUCCESS", "error": None},
            status_fn=lambda tid: {"status": "found"},
            retry_interval_seconds=0,
        )

        assert result["refunded"] == 1
        assert store.get_task("ordinary")["refunded"] == 1


# ---------------------------------------------------------------------------
# The combination that actually happened
# ---------------------------------------------------------------------------

def test_the_070_usdc_scenario_end_to_end(store):
    """Replays how money became unattributable, and shows it no longer does.

    Timeline on testnet: a hire locked USDC; it was never settled; 24 hours
    later housekeeping deleted the row; later still, Soroban evicted the
    contract entry. From then on the contract held funds that no task named
    and no entry described.
    """
    add_task(store, "orphan-candidate", age_seconds=2 * DAY)

    # Housekeeping runs first — under the old code this deleted the row.
    store.cleanup(max_age_seconds=DAY)
    assert store.get_task("orphan-candidate") is not None

    # Then the entry is evicted and the sweeper finds it unreadable.
    sweep_expired_refunds(
        store, FakeKeypair(),
        refund_fn=lambda kp, tid: {"hash": "x", "status": "SUCCESS", "error": None},
        status_fn=lambda tid: {"status": "not_found"},
        retry_interval_seconds=0,
    )

    # Housekeeping runs again, and again, and again.
    for _ in range(3):
        store.cleanup(max_age_seconds=DAY)

    survivor = store.get_task("orphan-candidate")
    assert survivor is not None, "the escrow became unattributable again"
    assert survivor["escrow_tx"] == REAL_TX
    assert survivor["refunded"] == 0
    assert survivor["status"] == "escrow_entry_unreadable"
    # And it is in the queue a person reconciles.
    assert "orphan-candidate" in [
        t["task_id"] for t in store.list_financially_open()
    ]


# ---------------------------------------------------------------------------
# 4. A refund the database claims but cannot show
# ---------------------------------------------------------------------------

class TestUnprovenRefundsSurviveCleanup:
    """`refunded = 1` with no transaction hash is an assertion, not a record.

    Task 40f6f3daf4c546fda37c19afbe8bcc5f says refunded = 1 with
    refund_tx_hash = 'h' — a fixture's literal — while the contract entry is
    live and holds 0.675 USDC. Measured on a throwaway copy of the real
    database with every row past the threshold, `cleanup` deleted all 32 rows
    including that one. It runs from the sweeper loop OUTSIDE the
    `if orch_kp:` guard, so starting the API was enough; no key, no refund.
    """

    def test_a_refund_with_no_hash_is_kept(self, store):
        add_task(store, "claimed", age_seconds=3 * DAY,
                 refunded=1, status="refunded")
        store.update_task("claimed", refund_tx_hash="")

        result = store.cleanup(max_age_seconds=DAY)

        assert store.get_task("claimed") is not None, (
            "deleted the only local record of a refund nobody can demonstrate"
        )
        assert result["deleted"] == 0
        assert result["preserved_open"] == 1

    def test_a_fixture_literal_does_not_count_as_a_hash(self, store):
        # The exact value found on the real row, and the others the suite
        # leaves behind. Each of these rows has a real escrow_tx, so each one
        # claims a refund of money that actually moved.
        for i, bogus in enumerate(("h", "x", "ok", "hash-task-2", "task-a-hash")):
            tid = f"claimed-{i}"
            add_task(store, tid, age_seconds=3 * DAY, refunded=1,
                     status="refunded")
            store.update_task(tid, refund_tx_hash=bogus)

        assert store.cleanup(max_age_seconds=DAY)["deleted"] == 0
        assert len(store.list_financially_open()) == 5

    def test_a_claim_with_no_escrow_behind_it_is_cleaned(self, store):
        """The other half of the rule: no escrow transaction means no funds
        were ever locked, so the refund claim has no money behind it.

        Without this, the predicate kept all 20 rows marked refunded in the
        real database — 19 of them fixtures with escrow_tx = 'abc' or empty —
        and housekeeping that never deletes anything is a different bug.
        """
        add_task(store, "no-escrow", age_seconds=3 * DAY, escrow_tx="",
                 refunded=1, status="refunded")
        store.update_task("no-escrow", refund_tx_hash="h")
        add_task(store, "fake-escrow", age_seconds=3 * DAY, escrow_tx="abc",
                 refunded=1, status="refunded")
        store.update_task("fake-escrow", refund_tx_hash="h")

        assert store.cleanup(max_age_seconds=DAY)["deleted"] == 2

    def test_a_demonstrable_refund_is_still_cleaned(self, store):
        # The money moved and there is a transaction to point at, so the row is
        # ordinary housekeeping. Keeping everything forever is a different bug.
        add_task(store, "proven", age_seconds=3 * DAY, refunded=1,
                 status="refunded")
        store.update_task("proven", refund_tx_hash=REAL_TX)

        assert store.cleanup(max_age_seconds=DAY)["deleted"] == 1
        assert store.get_task("proven") is None

    def test_it_shows_up_in_the_reconciliation_queue(self, store):
        add_task(store, "claimed", age_seconds=2 * DAY, refunded=1,
                 status="refunded")
        store.update_task("claimed", refund_tx_hash="h")

        assert "claimed" in [
            t["task_id"] for t in store.list_financially_open(older_than_seconds=DAY)
        ]

    def test_the_real_rows_shape_survives_a_cold_start(self, store):
        """End to end, the measured scenario: the row as it exists in
        `marketplace`, aged past the threshold, with housekeeping running the
        way the API's sweeper loop runs it — no orchestrator key involved."""
        add_task(store, REAL_TASK_ID_040, age_seconds=2 * DAY,
                 refunded=1, status="refunded")
        store.update_task(REAL_TASK_ID_040, refund_tx_hash="h",
                          refund_reason="deadline_expired")

        for _ in range(5):
            store.cleanup(max_age_seconds=DAY)

        row = store.get_task(REAL_TASK_ID_040)
        assert row is not None, "the 0.675 USDC row was deleted again"
        assert row["escrow_tx"] == REAL_TX, "lost the link to the chain"
