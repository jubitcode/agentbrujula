"""TTL keeper: checks each contract's TTL (live_until_ledger - current_ledger),
submits ExtendFootprintTTL if below threshold. Stub fetchers/extenders keep tests off live RPC."""

from __future__ import annotations

import pytest

from research_agent.ttl_keeper import (
    KeeperResult,
    run_keeper_cycle,
    should_extend,
)


def test_should_extend_when_ttl_below_threshold():
    # 50 ledgers remaining, threshold 100 → extend
    assert should_extend(live_until_ledger=150, current_ledger=100, threshold=100) is True


def test_should_skip_when_ttl_above_threshold():
    # 500 ledgers remaining, threshold 100 → skip
    assert should_extend(live_until_ledger=600, current_ledger=100, threshold=100) is False


def test_should_extend_when_already_expired():
    assert should_extend(live_until_ledger=50, current_ledger=100, threshold=100) is True


def test_should_extend_at_exact_boundary():
    # equal → not extending (only strictly below)
    assert should_extend(live_until_ledger=200, current_ledger=100, threshold=100) is False


def test_cycle_extends_only_below_threshold_contracts():
    contracts = ["CESCROW", "CREGISTRY", "CCOMPLIANCE"]
    fetcher_calls: list[str] = []
    extender_calls: list[tuple[str, int]] = []

    ttls = {
        "CESCROW": (1100, 1000),       # 100 remaining
        "CREGISTRY": (5000, 1000),     # 4000 remaining
        "CCOMPLIANCE": (3000, 1000),   # 2000 remaining
    }

    def fetcher(contract_id: str) -> tuple[int, int]:
        fetcher_calls.append(contract_id)
        return ttls[contract_id]

    def extender(contract_id: str, extend_to: int) -> bool:
        extender_calls.append((contract_id, extend_to))
        return True

    result = run_keeper_cycle(
        contracts=contracts,
        ttl_fetcher=fetcher,
        ttl_extender=extender,
        threshold_ledgers=500,
        extend_to_ledgers=10_000,
    )

    assert sorted(fetcher_calls) == sorted(contracts)
    assert extender_calls == [("CESCROW", 10_000)]
    assert result == KeeperResult(checked=3, extended=1, skipped=2, failed=0)


def test_cycle_records_failures_without_aborting_other_contracts():
    contracts = ["A", "B"]
    ttls = {"A": (1100, 1000), "B": (1050, 1000)}  # both need extending

    def fetcher(c):
        return ttls[c]

    extend_calls: list[str] = []

    def flaky_extender(c: str, extend_to: int) -> bool:
        extend_calls.append(c)
        if c == "A":
            raise RuntimeError("RPC error")
        return True

    result = run_keeper_cycle(
        contracts=contracts,
        ttl_fetcher=fetcher,
        ttl_extender=flaky_extender,
        threshold_ledgers=500,
        extend_to_ledgers=10_000,
    )

    assert extend_calls == ["A", "B"]
    assert result == KeeperResult(checked=2, extended=1, skipped=0, failed=1)


def test_cycle_handles_fetcher_failure_gracefully():
    def broken_fetcher(c):
        raise RuntimeError("RPC unavailable")

    extender_calls: list[str] = []

    def extender(c, extend_to):
        extender_calls.append(c)
        return True

    result = run_keeper_cycle(
        contracts=["X", "Y"],
        ttl_fetcher=broken_fetcher,
        ttl_extender=extender,
        threshold_ledgers=500,
        extend_to_ledgers=10_000,
    )

    # Can't read TTL, so extends must not be attempted
    assert extender_calls == []
    assert result.failed == 2
    assert result.extended == 0
