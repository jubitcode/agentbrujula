import sys
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "src"))


@pytest.fixture
def client():
    from api_test_app import app
    return TestClient(app)


class TestDebugMainnetGate:
    def test_debug_mcp_meta_returns_404_on_mainnet(self, client, monkeypatch):
        import server as srv
        monkeypatch.setattr(srv, "STELLAR_NETWORK", "public")
        resp = client.get("/debug/mcp-meta")
        assert resp.status_code == 404

    def test_debug_stream_debate_returns_404_on_mainnet(self, client, monkeypatch):
        import server as srv
        monkeypatch.setattr(srv, "STELLAR_NETWORK", "public")
        resp = client.get("/debug/stream-debate")
        assert resp.status_code == 404


class TestDebugTestnetStillWorks:
    """Sanity: testnet still reaches the handler (200 may contain health_error if mcp-meta isn't up — that's fine)."""

    def test_debug_mcp_meta_reachable_on_testnet(self, client, monkeypatch):
        import server as srv
        monkeypatch.setattr(srv, "STELLAR_NETWORK", "testnet")
        resp = client.get("/debug/mcp-meta")
        assert resp.status_code == 200
