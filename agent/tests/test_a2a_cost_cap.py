import sys
from pathlib import Path

import pytest
from fastapi import HTTPException

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "src"))


@pytest.fixture(autouse=True)
def reset_caps(monkeypatch):
    """Each test runs with default caps; server.py reads constants at module
    import so we monkeypatch them per-test rather than re-importing."""
    yield


def _estimator():
    import server as srv
    return srv._estimate_a2a_cost


class TestSizeCap:
    def test_normal_task_accepted(self):
        result = _estimator()({"task": "research stellar consensus"})
        assert float(result["estimated_cost"]) > 0
        assert float(result["estimated_cost"]) <= 5.0

    def test_oversized_task_rejected_with_413(self):
        oversized = "word " * 5001
        with pytest.raises(HTTPException) as exc:
            _estimator()({"task": oversized})
        assert exc.value.status_code == 413
        assert "too long" in exc.value.detail.lower()

    def test_at_cap_boundary_accepted(self):
        at_cap = "word " * 5000
        result = _estimator()({"task": at_cap})
        assert float(result["estimated_cost"]) > 0


class TestEstimateCeiling:
    def test_estimate_never_exceeds_ceiling(self):
        large = "word " * 5000  # at cap; raw formula 0.10 + 5000*0.001 = $5.10 would exceed ceiling
        result = _estimator()({"task": large})
        assert float(result["estimated_cost"]) <= 5.0, (
            "Estimator must clamp at MAX_A2A_ESTIMATED_USDC even at the size cap"
        )


class TestEnvOverride:
    def test_override_lowers_size_cap(self, monkeypatch):
        import server as srv
        monkeypatch.setattr(srv, "_MAX_A2A_TASK_WORDS", 100)
        oversized = "word " * 101
        with pytest.raises(HTTPException) as exc:
            srv._estimate_a2a_cost({"task": oversized})
        assert exc.value.status_code == 413

    def test_override_lowers_estimate_ceiling(self, monkeypatch):
        import server as srv
        monkeypatch.setattr(srv, "_MAX_A2A_ESTIMATED_USDC", 1.00)
        big = "word " * 4999  # raw = 0.10 + 4.999 = $5.099, must clamp to 1.00
        result = srv._estimate_a2a_cost({"task": big})
        assert float(result["estimated_cost"]) <= 1.00


class TestEdgeCases:
    def test_empty_task_returns_base_cost(self):
        result = _estimator()({"task": ""})
        assert float(result["estimated_cost"]) == 0.10

    def test_missing_task_returns_base_cost(self):
        result = _estimator()({})
        assert float(result["estimated_cost"]) == 0.10
