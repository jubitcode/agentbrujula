from __future__ import annotations

import os
import sqlite3
import threading
import time
from pathlib import Path


_SCHEMA_SQLITE = """
CREATE TABLE IF NOT EXISTS tasks (
    task_id TEXT PRIMARY KEY,
    wallet_address TEXT NOT NULL DEFAULT '',
    agent_handle TEXT DEFAULT '',
    task_text TEXT DEFAULT '',
    escrow_tx TEXT DEFAULT '',
    deadline_unix INTEGER DEFAULT 0,
    status TEXT DEFAULT 'pending',
    result_json TEXT DEFAULT '',
    created_at REAL NOT NULL,
    settled INTEGER DEFAULT 0,
    refunded INTEGER DEFAULT 0,
    refund_attempted_at REAL DEFAULT 0,
    refund_attempts INTEGER DEFAULT 0,
    refund_tx_hash TEXT DEFAULT '',
    refund_last_error TEXT DEFAULT '',
    refund_reason TEXT DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_tasks_wallet ON tasks(wallet_address);
CREATE INDEX IF NOT EXISTS idx_tasks_deadline ON tasks(deadline_unix);

CREATE TABLE IF NOT EXISTS nonces (
    nonce TEXT PRIMARY KEY,
    seen_at REAL NOT NULL
);

CREATE TABLE IF NOT EXISTS mint_history (
    public_key TEXT PRIMARY KEY,
    last_mint_at REAL NOT NULL
);

CREATE TABLE IF NOT EXISTS accounts (
    wallet_address TEXT PRIMARY KEY,
    display_name TEXT DEFAULT '',
    email TEXT DEFAULT '',
    role TEXT DEFAULT 'client',
    created_at REAL NOT NULL,
    updated_at REAL NOT NULL
);
"""

_SCHEMA_PG = """
CREATE TABLE IF NOT EXISTS tasks (
    task_id TEXT PRIMARY KEY,
    wallet_address TEXT NOT NULL DEFAULT '',
    agent_handle TEXT DEFAULT '',
    task_text TEXT DEFAULT '',
    escrow_tx TEXT DEFAULT '',
    deadline_unix BIGINT DEFAULT 0,
    status TEXT DEFAULT 'pending',
    result_json TEXT DEFAULT '',
    created_at DOUBLE PRECISION NOT NULL,
    settled INTEGER DEFAULT 0,
    refunded INTEGER DEFAULT 0,
    refund_attempted_at DOUBLE PRECISION DEFAULT 0,
    refund_attempts INTEGER DEFAULT 0,
    refund_tx_hash TEXT DEFAULT '',
    refund_last_error TEXT DEFAULT '',
    refund_reason TEXT DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_tasks_wallet ON tasks(wallet_address);
CREATE INDEX IF NOT EXISTS idx_tasks_deadline ON tasks(deadline_unix);

CREATE TABLE IF NOT EXISTS nonces (
    nonce TEXT PRIMARY KEY,
    seen_at DOUBLE PRECISION NOT NULL
);

CREATE TABLE IF NOT EXISTS mint_history (
    public_key TEXT PRIMARY KEY,
    last_mint_at DOUBLE PRECISION NOT NULL
);

CREATE TABLE IF NOT EXISTS accounts (
    wallet_address TEXT PRIMARY KEY,
    display_name TEXT DEFAULT '',
    email TEXT DEFAULT '',
    role TEXT DEFAULT 'client',
    created_at DOUBLE PRECISION NOT NULL,
    updated_at DOUBLE PRECISION NOT NULL
);
"""

_REQUIRED_TASK_COLUMNS_SQLITE: dict[str, str] = {
    "refund_attempted_at": "REAL DEFAULT 0",
    "refund_attempts": "INTEGER DEFAULT 0",
    "refund_tx_hash": "TEXT DEFAULT ''",
    "refund_last_error": "TEXT DEFAULT ''",
    "refund_reason": "TEXT DEFAULT ''",
    # Explicit, deliberate protection against automated refunds. See
    # TaskStore.hold_refund.
    "refund_hold": "INTEGER DEFAULT 0",
    "refund_hold_reason": "TEXT DEFAULT ''",
}
_REQUIRED_TASK_COLUMNS_PG: dict[str, str] = {
    "refund_attempted_at": "DOUBLE PRECISION DEFAULT 0",
    "refund_attempts": "INTEGER DEFAULT 0",
    "refund_tx_hash": "TEXT DEFAULT ''",
    "refund_last_error": "TEXT DEFAULT ''",
    "refund_reason": "TEXT DEFAULT ''",
    "refund_hold": "INTEGER DEFAULT 0",
    "refund_hold_reason": "TEXT DEFAULT ''",
}


class TaskStore:
    """Thread-safe task/nonce store with SQLite or PostgreSQL backend."""

    def __init__(self, db_path: str | None = None, database_url: str | None = None):
        self._lock = threading.Lock()
        self._use_pg = False

        # An explicit db_path means SQLite, and wins over the environment.
        #
        # It used to be the other way round: `database_url or
        # os.environ["DATABASE_URL"]` was consulted first, so `TaskStore(
        # db_path=tmp/"test.db")` silently connected to PostgreSQL whenever
        # DATABASE_URL was set, ignoring the path entirely. A caller asking for
        # a file and getting the application's database is the exact shape of
        # the accident that wrote test fixtures over a live escrow row — and it
        # is worst in the place it is most used, a test that believes it is
        # isolated.
        if db_path is not None:
            self._init_sqlite(db_path)
            return

        database_url = database_url or os.environ.get("DATABASE_URL", "")

        if database_url and database_url.startswith("postgres"):
            self._init_pg(database_url)
        else:
            self._init_sqlite(db_path)

    def _init_sqlite(self, db_path: str | None = None):
        if db_path is None:
            data_dir = Path.home() / ".mcp-meta"
            data_dir.mkdir(parents=True, exist_ok=True)
            db_path = str(data_dir / "marketplace.db")

        self._db_path = db_path
        self._conn = sqlite3.connect(db_path, check_same_thread=False)
        self._conn.row_factory = sqlite3.Row
        self._conn.execute("PRAGMA journal_mode=WAL")
        self._conn.execute("PRAGMA busy_timeout=5000")
        self._conn.executescript(_SCHEMA_SQLITE)
        cols = {
            row[1] for row in self._conn.execute("PRAGMA table_info(tasks)").fetchall()
        }
        for col, ddl in _REQUIRED_TASK_COLUMNS_SQLITE.items():
            if col not in cols:
                self._conn.execute(f"ALTER TABLE tasks ADD COLUMN {col} {ddl}")
        self._conn.commit()

    def _init_pg(self, database_url: str):
        import psycopg2

        self._use_pg = True
        self._pg_dsn = database_url
        self._conn = psycopg2.connect(database_url)
        self._conn.autocommit = False
        with self._conn.cursor() as cur:
            for stmt in _SCHEMA_PG.strip().split(";"):
                stmt = stmt.strip()
                if stmt:
                    cur.execute(stmt)
            for col, ddl in _REQUIRED_TASK_COLUMNS_PG.items():
                cur.execute(f"ALTER TABLE tasks ADD COLUMN IF NOT EXISTS {col} {ddl}")
            self._conn.commit()

    def _execute(self, sql: str, params: tuple = ()):
        """Execute SQL with backend-appropriate parameterization."""
        if self._use_pg:
            sql = sql.replace("?", "%s")
            sql = sql.replace("INSERT OR IGNORE", "INSERT")
            if "INSERT" in sql and "ON CONFLICT" not in sql:
                sql += " ON CONFLICT DO NOTHING"
        return (
            self._conn.execute(sql, params)
            if not self._use_pg
            else self._pg_execute(sql, params)
        )

    def _pg_execute(self, sql: str, params: tuple = ()):
        """Execute on PostgreSQL and return a cursor-like result."""
        cur = self._conn.cursor()
        cur.execute(sql, params)
        return cur

    def _fetchone(self, sql: str, params: tuple = ()) -> dict | None:
        if self._use_pg:
            sql = sql.replace("?", "%s")
            with self._conn.cursor() as cur:
                cur.execute(sql, params)
                row = cur.fetchone()
                if row is None:
                    return None
                cols = [desc[0] for desc in cur.description]
                return dict(zip(cols, row))
        else:
            row = self._conn.execute(sql, params).fetchone()
            return dict(row) if row else None

    def _fetchall(self, sql: str, params: tuple = ()) -> list[dict]:
        if self._use_pg:
            sql = sql.replace("?", "%s")
            with self._conn.cursor() as cur:
                cur.execute(sql, params)
                rows = cur.fetchall()
                cols = [desc[0] for desc in cur.description]
                return [dict(zip(cols, row)) for row in rows]
        else:
            rows = self._conn.execute(sql, params).fetchall()
            return [dict(r) for r in rows]

    def _commit(self):
        self._conn.commit()

    def close(self):
        self._conn.close()

    def create_task(
        self,
        task_id: str,
        wallet_address: str = "",
        agent_handle: str = "",
        task_text: str = "",
        escrow_tx: str = "",
        deadline_unix: int = 0,
        status: str = "pending",
    ) -> dict:
        with self._lock:
            self._execute(
                """INSERT INTO tasks (task_id, wallet_address, agent_handle, task_text,
                   escrow_tx, deadline_unix, status, created_at)
                   VALUES (?, ?, ?, ?, ?, ?, ?, ?)""",
                (
                    task_id,
                    wallet_address,
                    agent_handle,
                    task_text,
                    escrow_tx,
                    deadline_unix,
                    status,
                    time.time(),
                ),
            )
            self._commit()
        return self.get_task(task_id)

    def get_task(self, task_id: str, wallet_address: str | None = None) -> dict | None:
        if wallet_address:
            return self._fetchone(
                "SELECT * FROM tasks WHERE task_id = ? AND wallet_address = ?",
                (task_id, wallet_address),
            )
        return self._fetchone("SELECT * FROM tasks WHERE task_id = ?", (task_id,))

    def update_task(self, task_id: str, **fields) -> None:
        if not fields:
            return
        allowed = {
            "status",
            "result_json",
            "settled",
            "refunded",
            "escrow_tx",
            "agent_handle",
            "deadline_unix",
            "refund_attempts",
            "refund_tx_hash",
            "refund_last_error",
            "refund_reason",
            "refund_hold",
            "refund_hold_reason",
        }
        updates = {k: v for k, v in fields.items() if k in allowed}
        if not updates:
            return
        set_clause = ", ".join(f"{k} = ?" for k in updates)
        values = list(updates.values()) + [task_id]
        with self._lock:
            self._execute(
                f"UPDATE tasks SET {set_clause} WHERE task_id = ?", tuple(values)
            )
            self._commit()

    def list_expired(self, refund_retry_after: float = 0) -> list[dict]:
        """Return tasks past deadline that are not settled, refunded, complete,
        or in the dead-letter `refund_failed` state.

        `refund_retry_after`: seconds since the last refund attempt before
        a task becomes eligible again. Default 0 (always eligible).
        Pass a positive value (e.g. 600) to back off on tasks that have already
        had a refund attempted recently this is the idempotency guard for the sweeper.
        """
        now = time.time()
        cutoff = now - refund_retry_after
        # `escrow_entry_unreadable` has to be here even though those rows keep
        # refunded = 0. The sweeper reaches that state when the contract can no
        # longer read the entry, and a refund call against an entry `get_escrow`
        # traps on will not start working — so without this the task comes back
        # every cycle, forever.
        #
        # The previous code avoided the loop by writing refunded = 1, which
        # silenced it by claiming the money had gone back. Excluding the status
        # stops the retries without making that claim.
        #
        # `refund_hold` is the deliberate, operator-set protection: see
        # `hold_refund`. It is checked here so held tasks never enter the
        # queue, and again inside `process_one_refund` so a caller that builds
        # its own list cannot step around it.
        return self._fetchall(
            f"""SELECT * FROM tasks
               WHERE deadline_unix > 0 AND deadline_unix < ?
               AND settled = 0 AND refunded = 0
               AND {self._NOT_HELD_SQL}
               AND status NOT IN ('complete', 'refunded', 'settled',
                                  'refund_failed', 'escrow_entry_unreadable')
               AND refund_attempted_at <= ?""",
            (now, cutoff),
        )

    #: True for a row the sweeper is allowed to act on.
    #:
    #: `COALESCE` because the column arrives by migration on tables that
    #: already have rows. A plain `refund_hold = 0` evaluates to NULL — not
    #: true, not false — on any row where the value is NULL, which would drop
    #: it out of the expired queue and out of `cleanup`'s delete at the same
    #: time. Held rows always carry an explicit 1 (`hold_refund` writes it), so
    #: reading NULL as "not held" is the correct and safe direction.
    _NOT_HELD_SQL = "COALESCE(refund_hold, 0) = 0"

    def hold_refund(self, task_id: str, reason: str) -> bool:
        """Protect one task's escrow from every automated refund path.

        This exists because of what happened to task
        `40f6f3daf4c546fda37c19afbe8bcc5f`: 0.675 USDC sitting in a live
        contract entry, and the only thing keeping the sweeper away from it was
        a *false* `refunded = 1` written onto the row by a test fixture. The
        protection was an accident, and correcting the record would have
        removed it.

        A hold does not depend on an accident. It does not depend on
        `ORCHESTRATOR_SECRET_KEY` being absent, on a deadline being in the
        future, or on a status value meaning something it does not. It is a
        column on the row, it survives restarts, backups and schema
        migrations, it is visible to anybody reading the table, and it is
        removed only by `release_refund_hold`.

        `reason` is required and must say something. A hold nobody can explain
        later is a hold somebody will clear.

        Returns False if there is no such task.
        """
        reason = (reason or "").strip()
        if not reason:
            raise ValueError(
                "a refund hold needs a reason: it is a financial control, and "
                "whoever finds it months from now has to be able to read why"
            )
        with self._lock:
            cur = self._execute(
                "UPDATE tasks SET refund_hold = 1, refund_hold_reason = ? "
                "WHERE task_id = ?",
                (reason[:500], task_id),
            )
            held = cur.rowcount > 0
            self._commit()
        return held

    def release_refund_hold(self, task_id: str, reason: str) -> bool:
        """Lift a hold. Separate call, separate reason, on purpose.

        Releasing is the step that re-arms an automated transfer of money, so
        it is never a side effect of anything else — not of a status change,
        not of a cleanup, not of a migration. The reason replaces the hold's
        own text so the row keeps the last thing a person said about it.
        """
        reason = (reason or "").strip()
        if not reason:
            raise ValueError(
                "releasing a refund hold re-arms an automated payment: say why"
            )
        with self._lock:
            cur = self._execute(
                "UPDATE tasks SET refund_hold = 0, refund_hold_reason = ? "
                "WHERE task_id = ?",
                (f"released: {reason}"[:500], task_id),
            )
            released = cur.rowcount > 0
            self._commit()
        return released

    def is_refund_held(self, task_id: str) -> bool:
        row = self._fetchone(
            "SELECT refund_hold FROM tasks WHERE task_id = ?", (task_id,)
        )
        return bool(row and row.get("refund_hold"))

    def list_refund_holds(self) -> list[dict]:
        """Every held task, so a hold is something an operator can see and
        account for rather than something buried in a column."""
        return self._fetchall(
            f"""SELECT task_id, escrow_tx, refund_hold_reason, status, settled,
                      refunded, deadline_unix, created_at
               FROM tasks
               WHERE NOT ({self._NOT_HELD_SQL})
               ORDER BY created_at DESC"""
        )

    def list_with_escrow(self, limit: int = 50) -> list[dict]:
        """Return the most recent tasks that locked money, newest first.

        Only tasks with an escrow transaction: a task without one never reached
        the chain and has nothing for a reader to verify.

        This is the local half of the public escrow view. The authoritative
        amounts and statuses come from the contract — see
        `research_agent.escrow.get_escrow_status` — because what a dashboard
        asserts about money should be readable from the ledger, not from our
        own database.
        """
        # A Stellar transaction hash is 64 hex characters, always. Rows holding
        # anything else ("tx", "mock_escrow_tx") are leftovers from test runs,
        # and presenting one as a link to an explorer would be presenting a dead
        # link as evidence. Length is the portable check across both backends.
        return self._fetchall(
            """SELECT task_id, agent_handle, escrow_tx, deadline_unix, status,
                      settled, refunded, created_at, refund_tx_hash
               FROM tasks
               WHERE escrow_tx IS NOT NULL AND length(escrow_tx) = 64
               ORDER BY created_at DESC
               LIMIT ?""",
            (limit,),
        )

    def mark_refund_attempted(self, task_id: str) -> None:
        """Stamp `refund_attempted_at = now()` so a crash mid-refund won't
        cause the next sweep cycle to retry within the retry window."""
        with self._lock:
            self._execute(
                "UPDATE tasks SET refund_attempted_at = ? WHERE task_id = ?",
                (time.time(), task_id),
            )
            self._commit()

    #: A task is "financially open" while the contract may still be holding its
    #: money: it locked funds in a real transaction and neither settle nor
    #: refund has been recorded. Deleting one of these loses the only local link
    #: between an escrow on the chain and the task, agent and client it belongs
    #: to — and an escrow nobody can attribute is money nobody will recover.
    #:
    #: A 64-character hex string is the shape of a Stellar transaction hash.
    #: Anything else never reached the network, so no escrow exists for it.
    _FINANCIALLY_OPEN_SQL = (
        "COALESCE(escrow_tx, '') <> '' AND length(escrow_tx) = 64 "
        "AND settled = 0 AND refunded = 0"
    )

    #: Every class of row housekeeping must leave alone, in one place so that
    #: `cleanup` and `list_financially_open` cannot drift apart — a row that is
    #: preserved but not listed is a row nobody reconciles.
    #:
    #: A held row is here on its own merit, not because it also happens to look
    #: financially open. The 0.675 USDC row carries a false `refunded = 1`, so
    #: `_FINANCIALLY_OPEN_SQL` does **not** match it; its hold is what keeps it.
    #: A refund this database claims but cannot demonstrate. `refunded = 1`
    #: with no transaction hash to point at is an assertion, not a record, and
    #: deleting it destroys the only local trace of an escrow whose money may
    #: still be in the contract.
    #:
    #: This is not hypothetical. Task `40f6f3daf4c546fda37c19afbe8bcc5f` says
    #: refunded = 1 with `refund_tx_hash = 'h'` — a test fixture's literal —
    #: while the contract entry is live and holds 0.675 USDC. Measured against
    #: a throwaway copy of the real database, with every row past the 24h
    #: threshold, housekeeping deleted it: {'deleted': 32}. `cleanup` runs from
    #: the sweeper loop OUTSIDE the `if orch_kp:` guard (api/server.py:124), so
    #: it needs no orchestrator key — simply starting the API was enough.
    #:
    #: `settled = 1` is not checked the same way: the settle transaction hash
    #: lives inside `result_json`, not in a column, so there is nothing to
    #: measure a length against portably. Worth a column of its own.
    #: A real escrow transaction is part of it: a row with `escrow_tx = 'abc'`
    #: never locked funds, so there is no money behind its refund claim and
    #: nothing to preserve. Without that clause this predicate kept all 20
    #: rows marked refunded in the real database — 19 of them fixtures — and
    #: preserving everything forever is a different bug.
    _UNPROVEN_REFUND_SQL = (
        "length(COALESCE(escrow_tx, '')) = 64 "
        "AND refunded = 1 "
        "AND length(COALESCE(refund_tx_hash, '')) <> 64"
    )

    _PRESERVE_SQL = (
        f"({_FINANCIALLY_OPEN_SQL})"
        f" OR ({_UNPROVEN_REFUND_SQL})"
        " OR COALESCE(status, '') = 'escrow_entry_unreadable'"
        " OR COALESCE(refund_hold, 0) = 1"
    )

    def cleanup(self, max_age_seconds: int = 86400) -> dict:
        """Delete old tasks, except any that may still have money on the chain.

        Housekeeping used to be `DELETE FROM tasks WHERE created_at < ?`, with
        no regard for whether the task had been resolved. A hire whose escrow
        was never settled lost its local record after 24 hours while the funds
        stayed in the contract — and once Soroban evicted the contract entry at
        the end of its TTL, the money was invisible from both sides at once.

        That is not hypothetical. On 2026-10-21 `usdc-escrow` held 0.745 USDC
        of which **0.070 could not be attributed to any task**, because the rows
        that would have named them had already been deleted here.

        Four classes are now never deleted, however old — see
        `_PRESERVE_SQL`:

        * **Financially open** tasks — see `_FINANCIALLY_OPEN_SQL`.
        * Tasks claiming a refund they cannot demonstrate — see
          `_UNPROVEN_REFUND_SQL`.
        * Tasks marked `escrow_entry_unreadable`, which are precisely the ones
          flagged for a human to reconcile.
        * Tasks under a refund hold — see `hold_refund`.

        Returns `{"deleted": int, "preserved_open": int}`. The second number is
        the point: housekeeping that silently declines to clean is worth
        knowing about, and a growing count means escrows are not being resolved.
        """
        cutoff = time.time() - max_age_seconds
        with self._lock:
            preserved_rows = self._fetchall(
                f"""SELECT count(*) AS n FROM tasks
                     WHERE created_at < ?
                       AND ({self._PRESERVE_SQL})""",
                (cutoff,),
            )
            preserved = int(preserved_rows[0]["n"]) if preserved_rows else 0
            cur = self._execute(
                f"""DELETE FROM tasks
                     WHERE created_at < ?
                       AND NOT ({self._PRESERVE_SQL})""",
                (cutoff,),
            )
            deleted = cur.rowcount
            self._commit()
            return {"deleted": deleted, "preserved_open": preserved}

    def list_financially_open(self, older_than_seconds: float = 0) -> list[dict]:
        """Tasks that may still have funds in the contract.

        The queue a person works through when reconciling. Pass
        `older_than_seconds` to see only the ones old enough that they should
        have resolved by now.
        """
        cutoff = time.time() - older_than_seconds
        return self._fetchall(
            f"""SELECT * FROM tasks
                 WHERE created_at < ?
                   AND ({self._PRESERVE_SQL})
                 ORDER BY created_at""",
            (cutoff,),
        )

    def claim_nonce(self, nonce: str) -> bool:
        """Atomically claim a nonce. Returns True if fresh (claim succeeded), False if already used (replay)."""
        with self._lock:
            cur = self._execute(
                "INSERT OR IGNORE INTO nonces (nonce, seen_at) VALUES (?, ?)",
                (nonce, time.time()),
            )
            self._commit()
            return cur.rowcount > 0

    def check_nonce(self, nonce: str) -> bool:
        """DEPRECATED — use `claim_nonce` for atomic check-and-claim."""
        row = self._fetchone("SELECT 1 FROM nonces WHERE nonce = ?", (nonce,))
        return row is None

    def record_nonce(self, nonce: str) -> None:
        """DEPRECATED — use `claim_nonce` for atomic check-and-claim."""
        with self._lock:
            self._execute(
                "INSERT OR IGNORE INTO nonces (nonce, seen_at) VALUES (?, ?)",
                (nonce, time.time()),
            )
            self._commit()

    def cleanup_nonces(self, ttl: int = 300) -> int:
        """Delete nonces older than ttl seconds. Returns count deleted."""
        cutoff = time.time() - ttl
        with self._lock:
            cur = self._execute("DELETE FROM nonces WHERE seen_at < ?", (cutoff,))
            self._commit()
            return cur.rowcount

    # ── Accounts ────────────────────────────────────────────────────────
    #
    # The first thing in this store that is about a person rather than a task.
    # The key is the Stellar address, same as everywhere else in the system:
    # it is what the signed challenge proves control of, and what the JWT
    # carries as `sub`. Name and email hang off it and are decoration — they
    # are self-declared and nothing is verified about them.

    def get_account(self, wallet_address: str) -> dict | None:
        """The profile for a wallet, or None if it has never registered."""
        row = self._fetchone(
            "SELECT * FROM accounts WHERE wallet_address = ?",
            (wallet_address,),
        )
        return dict(row) if row else None

    def upsert_account(
        self,
        wallet_address: str,
        display_name: str = "",
        email: str = "",
        role: str = "client",
    ) -> dict:
        """Create or update a profile, and return it as stored.

        `created_at` survives an update: it is when this person first arrived,
        and overwriting it on every profile edit would lose that.
        """
        with self._lock:
            now = time.time()
            existing = self._fetchone(
                "SELECT created_at FROM accounts WHERE wallet_address = ?",
                (wallet_address,),
            )
            created = float(existing["created_at"]) if existing else now

            if self._use_pg:
                self._execute(
                    "INSERT INTO accounts "
                    "(wallet_address, display_name, email, role, created_at, updated_at) "
                    "VALUES (?, ?, ?, ?, ?, ?) "
                    "ON CONFLICT (wallet_address) DO UPDATE SET "
                    "display_name = EXCLUDED.display_name, "
                    "email = EXCLUDED.email, "
                    "role = EXCLUDED.role, "
                    "updated_at = EXCLUDED.updated_at",
                    (wallet_address, display_name, email, role, created, now),
                )
            else:
                self._execute(
                    "INSERT OR REPLACE INTO accounts "
                    "(wallet_address, display_name, email, role, created_at, updated_at) "
                    "VALUES (?, ?, ?, ?, ?, ?)",
                    (wallet_address, display_name, email, role, created, now),
                )
            self._commit()

        return {
            "wallet_address": wallet_address,
            "display_name": display_name,
            "email": email,
            "role": role,
            "created_at": created,
            "updated_at": now,
        }

    def last_mint_at(self, public_key: str) -> float | None:
        """Return the last-mint timestamp for this wallet, or None if never."""
        row = self._fetchone(
            "SELECT last_mint_at FROM mint_history WHERE public_key = ?",
            (public_key,),
        )
        return float(row["last_mint_at"]) if row else None

    def record_mint(self, public_key: str) -> None:
        """Record now() as the last mint time for this wallet (upsert)."""
        with self._lock:
            now = time.time()
            if self._use_pg:
                self._execute(
                    "INSERT INTO mint_history (public_key, last_mint_at) "
                    "VALUES (?, ?) "
                    "ON CONFLICT (public_key) DO UPDATE SET last_mint_at = EXCLUDED.last_mint_at",
                    (public_key, now),
                )
            else:
                self._execute(
                    "INSERT OR REPLACE INTO mint_history (public_key, last_mint_at) VALUES (?, ?)",
                    (public_key, now),
                )
            self._commit()
