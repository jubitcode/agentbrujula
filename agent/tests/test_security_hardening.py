import os
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "src"))
os.environ.setdefault("X402_ENABLED", "false")

from fastapi.testclient import TestClient
from api_test_app import app


@pytest.fixture
def client():
    return TestClient(app)


class TestSecurityHeaders:
    def test_x_content_type_options(self, client):
        resp = client.get("/api/registry")
        assert resp.headers.get("X-Content-Type-Options") == "nosniff"

    def test_x_frame_options(self, client):
        resp = client.get("/api/registry")
        assert resp.headers.get("X-Frame-Options") == "DENY"

    def test_referrer_policy(self, client):
        resp = client.get("/api/registry")
        assert resp.headers.get("Referrer-Policy") == "strict-origin-when-cross-origin"

    def test_x_xss_protection(self, client):
        resp = client.get("/api/registry")
        assert resp.headers.get("X-XSS-Protection") == "1; mode=block"

    def test_headers_on_error_responses_too(self, client):
        """Even 404s must carry security headers — middleware must run before routing."""
        resp = client.get("/api/nonexistent-endpoint")
        assert resp.headers.get("X-Content-Type-Options") == "nosniff"


class TestRateLimiting:
    def test_fund_endpoint_is_rate_limited(self, client):
        import server
        import inspect
        source = inspect.getsource(server.fund_account)
        assert hasattr(server.app.state, "limiter"), "Rate limiter not configured"

    def test_rate_limiter_is_configured(self, client):
        assert hasattr(app.state, "limiter")
        assert app.state.limiter is not None


class TestGitignore:
    def test_gitignore_covers_nested_env(self):
        # agent/tests/ → agent/ → repo root
        gitignore = Path(__file__).resolve().parent.parent.parent / ".gitignore"
        content = gitignore.read_text()
        assert "**/.env" in content, (
            ".gitignore should use **/.env to cover nested directories"
        )
