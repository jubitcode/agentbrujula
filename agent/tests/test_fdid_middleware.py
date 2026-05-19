import os
from unittest.mock import patch

import pytest
from fastapi.testclient import TestClient


@pytest.fixture(autouse=True)
def reset_limiter():
    import server as srv
    srv.limiter.reset()
    yield


def _make_client_with_fdid(fdid_value: str | None):
    """Reload server module with AZURE_FDID env set (or unset) so the middleware re-reads it."""
    import sys
    import importlib
    from pathlib import Path

    sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "src"))
    if fdid_value is None:
        os.environ.pop("AZURE_FDID", None)
    else:
        os.environ["AZURE_FDID"] = fdid_value

    if "server" in sys.modules:
        importlib.reload(sys.modules["server"])
    from api_test_app import app
    return TestClient(app)


class TestFDIDMiddlewareDisabled:
    def test_unset_env_allows_all_requests(self):
        client = _make_client_with_fdid(None)
        resp = client.get("/api/discover")
        assert resp.status_code != 403, "FDID middleware must not run when AZURE_FDID is unset"


class TestFDIDMiddlewareEnabled:
    EXPECTED = "11111111-2222-3333-4444-555555555555"

    def test_missing_header_returns_403(self):
        client = _make_client_with_fdid(self.EXPECTED)
        resp = client.get("/api/discover")
        assert resp.status_code == 403
        assert "front door" in resp.text.lower() or "fdid" in resp.text.lower()

    def test_wrong_header_returns_403(self):
        client = _make_client_with_fdid(self.EXPECTED)
        resp = client.get("/api/discover", headers={"X-Azure-FDID": "wrong-guid"})
        assert resp.status_code == 403

    def test_matching_header_passes(self):
        client = _make_client_with_fdid(self.EXPECTED)
        resp = client.get("/api/discover", headers={"X-Azure-FDID": self.EXPECTED})
        # Downstream may 200/500 depending on store state; only assert FDID itself didn't block
        assert resp.status_code != 403


class TestFDIDBypassesNonAPIPaths:
    EXPECTED = "11111111-2222-3333-4444-555555555555"

    def test_docs_bypassed(self):
        client = _make_client_with_fdid(self.EXPECTED)
        resp = client.get("/docs")
        # Health probes / docs are not under /api/; FDID must let them through
        assert resp.status_code != 403

    def test_openapi_bypassed(self):
        client = _make_client_with_fdid(self.EXPECTED)
        resp = client.get("/openapi.json")
        assert resp.status_code != 403
