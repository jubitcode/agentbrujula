import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "src"))

from fastapi.testclient import TestClient
from api_test_app import app


@pytest.fixture
def client():
    return TestClient(app)


class TestExecuteEndpointAuth:
    def test_execute_without_auth_returns_401(self, client):
        resp = client.get("/api/execute/some-task-id")
        assert resp.status_code == 401, (
            f"/api/execute must require auth, got {resp.status_code}"
        )

    def test_execute_team_without_auth_returns_401(self, client):
        resp = client.get("/api/execute-team/some-task-id")
        assert resp.status_code == 401, (
            f"/api/execute-team must require auth, got {resp.status_code}"
        )


class TestResultEndpointAuth:
    def test_result_without_auth_returns_401(self, client):
        resp = client.get("/api/result/some-task-id")
        assert resp.status_code == 401, (
            f"/api/result must require auth, got {resp.status_code}"
        )

    def test_result_team_without_auth_returns_401(self, client):
        resp = client.get("/api/result-team/some-task-id")
        assert resp.status_code == 401, (
            f"/api/result-team must require auth, got {resp.status_code}"
        )


class TestTaskIdFormat:
    """Task IDs must be unpredictable (UUID) — guard against regression to timestamp-based IDs."""

    def test_no_timestamp_based_task_ids_in_hire(self):
        import server
        import re
        import inspect
        source = inspect.getsource(server.hire)

        assert 'f"hire-{' not in source, (
            "hire() still uses predictable task ID format hire-{handle}-{timestamp}"
        )

    def test_no_timestamp_based_task_ids_in_hire_team(self):
        import server
        import inspect
        source = inspect.getsource(server.hire_team)

        assert 'f"team-{' not in source, (
            "hire_team() still uses predictable task ID format team-{timestamp}"
        )
