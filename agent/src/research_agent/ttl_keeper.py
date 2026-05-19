from __future__ import annotations

from dataclasses import dataclass
from typing import Callable


def should_extend(live_until_ledger: int, current_ledger: int, threshold: int) -> bool:
    """Return True if the entry's remaining TTL is strictly below `threshold`.

    `threshold` is the minimum number of ledgers we want to keep ahead of
    expiry. Tune by environment: testnet ledgers ~5s (so 100k ledgers ≈ 6 days),
    mainnet ledgers similar but the cost of falling behind is real.
    """
    remaining = live_until_ledger - current_ledger
    return remaining < threshold


@dataclass(frozen=True)
class KeeperResult:
    checked: int
    extended: int
    skipped: int
    failed: int


TTLFetcher = Callable[[str], tuple[int, int]]
"""(contract_id) -> (live_until_ledger, current_ledger)."""

TTLExtender = Callable[[str, int], bool]
"""(contract_id, extend_to_ledgers) -> success."""


def run_keeper_cycle(
    contracts: list[str],
    ttl_fetcher: TTLFetcher,
    ttl_extender: TTLExtender,
    threshold_ledgers: int,
    extend_to_ledgers: int,
) -> KeeperResult:
    """Check each contract's TTL and extend when below threshold.

    Failures (fetcher or extender) are counted, not raised one bad
    contract shouldn't stop the keeper from servicing the others.
    """
    extended = 0
    skipped = 0
    failed = 0

    for contract_id in contracts:
        try:
            live_until, current = ttl_fetcher(contract_id)
        except Exception:
            failed += 1
            continue

        if not should_extend(live_until, current, threshold_ledgers):
            skipped += 1
            continue

        try:
            ok = ttl_extender(contract_id, extend_to_ledgers)
        except Exception:
            failed += 1
            continue

        if ok:
            extended += 1
        else:
            failed += 1

    return KeeperResult(
        checked=len(contracts),
        extended=extended,
        skipped=skipped,
        failed=failed,
    )
