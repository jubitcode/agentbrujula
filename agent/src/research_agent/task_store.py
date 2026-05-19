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
"""

_REQUIRED_TASK_COLUMNS_SQLITE: dict[str, str] = {
    "refund_attempted_at": "REAL DEFAULT 0",
    "refund_attempts": "INTEGER DEFAULT 0",
    "refund_tx_hash": "TEXT DEFAULT ''",
    "refund_last_error": "TEXT DEFAULT ''",
    "refund_reason": "TEXT DEFAULT ''",
}
_REQUIRED_TASK_COLUMNS_PG: dict[str, str] = {
    "refund_attempted_at": "DOUBLE PRECISION DEFAULT 0",
    "refund_attempts": "INTEGER DEFAULT 0",
    "refund_tx_hash": "TEXT DEFAULT ''",
    "refund_last_error": "TEXT DEFAULT ''",
    "refund_reason": "TEXT DEFAULT ''",
}


class TaskStore:
    """Thread-safe task/nonce store with SQLite or PostgreSQL backend."""

    def __init__(self, db_path: str | None = None, database_url: str | None = None):
        self._lock = threading.Lock()
        self._use_pg = False

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
        return self._fetchall(
            """SELECT * FROM tasks
               WHERE deadline_unix > 0 AND deadline_unix < ?
               AND settled = 0 AND refunded = 0
               AND status NOT IN ('complete', 'refunded', 'settled', 'refund_failed')
               AND refund_attempted_at <= ?""",
            (now, cutoff),
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

    def cleanup(self, max_age_seconds: int = 86400) -> int:
        """Delete tasks older than max_age_seconds. Returns count deleted."""
        cutoff = time.time() - max_age_seconds
        with self._lock:
            cur = self._execute("DELETE FROM tasks WHERE created_at < ?", (cutoff,))
            self._commit()
            return cur.rowcount

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
