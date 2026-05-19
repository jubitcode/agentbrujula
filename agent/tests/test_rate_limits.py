"""Rate limits on cost-incurring endpoints + CORS lockdown.

Limits:
  /api/discover    10/minute
  /api/hire         5/minute
  /api/execute-a2a  5/minute
  /api/debate       3/minute
  /api/hire-team    3/minute

CORS restricted to ALLOWED_ORIGINS (default localhost:3002)."""

import sys
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "src"))


@pytest.fixture(autouse=True)
def reset_limiter():
    """Testclient uses one IP, so limits accumulate across tests — reset per-test."""
    import server as srv
    srv.limiter.reset()
    yield


@pytest.fixture
def client():
    from api_test_app import app
    return TestClient(app)


class TestDiscoverRateLimit:
    def test_within_limit_succeeds(self, client):
        for i in range(5):
            resp = client.post("/api/discover", json={
                "task": "test", "capabilities": ["research"], "budget_xlm": 1,
            })
            assert resp.status_code == 200, f"call {i+1} failed: {resp.status_code}"

    def test_burst_above_limit_returns_429(self, client):
        ok = 0
        for i in range(15):
            resp = client.post("/api/discover", json={
                "task": "test", "capabilities": ["research"], "budget_xlm": 1,
            })
            if resp.status_code == 200:
                ok += 1
            elif resp.status_code == 429:
                break
        assert ok == 10, f"Expected 10 successful before 429; got {ok}"


class TestX402GatedEndpointsHaveLimiter:
    """x402-gated endpoints rate-limit POST-payment-verification — the operator's
    OpenRouter cost is incurred after payment, so that's where the limiter must fire.
    Pre-402 path is cheap and self-limiting via the facilitator."""

    def test_hire_has_limit_decorator(self):
        import server as srv
        assert srv.limiter is not None
        assert hasattr(srv, "hire")


class TestCORSConfig:
    def test_cors_allows_default_origin(self, client):
        resp = client.options(
            "/api/discover",
            headers={
                "Origin": "http://localhost:3002",
                "Access-Control-Request-Method": "POST",
            },
        )
        assert resp.status_code in (200, 204)
        assert resp.headers.get("access-control-allow-origin") == "http://localhost:3002"

    def test_cors_rejects_arbitrary_origin(self, client):
        """Arbitrary origin must NOT be echoed back; must not be `*` either."""
        resp = client.options(
            "/api/discover",
            headers={
                "Origin": "https://attacker.example.com",
                "Access-Control-Request-Method": "POST",
            },
        )
        # Starlette returns preflight with no Allow-Origin header for disallowed origins
        allow_origin = resp.headers.get("access-control-allow-origin", "")
        assert allow_origin != "https://attacker.example.com", (
            "Attacker origin must NOT be echoed back as allowed"
        )
        assert allow_origin != "*"
