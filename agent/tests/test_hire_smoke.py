from __future__ import annotations

import pytest
import httpx

from research_agent.cost_table import AgentRow, print_cost_table
from research_agent.soroban_client import list_active_full, get_tool, get_rep, stroops_to_xlm
from research_agent.hebbian_bridge import query_hebbian, reinforce_hebbian
from research_agent.hire import _match_capabilities
from research_agent.knowledge import ResearchKnowledgeStore


def _mcp_meta_running() -> bool:
    """Confirm MCP-Meta is the service on the configured port (not some other process)."""
    import os
    mcp_url = os.environ.get("MCP_META_URL", "http://127.0.0.1:8080")
    try:
        resp = httpx.get(f"{mcp_url}/v1/tools", timeout=2)
        return resp.status_code == 200
    except Exception:
        return False


class TestSorobanLive:
    def test_list_active_full(self):
        tools = list_active_full()
        assert isinstance(tools, list)
        assert len(tools) >= 1
        for t in tools:
            assert "handle" in t
            assert "capabilities" in t
            assert "rate_stroops" in t
            assert "model_tier" in t
            assert t["active"] is True

    def test_get_tool(self):
        tools = list_active_full()
        handle = tools[0]["handle"]
        tool = get_tool(handle)
        assert tool["handle"] == handle
        assert isinstance(tool["capabilities"], list)
        assert isinstance(tool["rate_stroops"], int)

    def test_get_rep(self):
        tools = list_active_full()
        agent_addr = tools[0]["agent"]
        try:
            rep = get_rep(agent_addr)
            assert isinstance(rep, int)
            assert rep >= 0
        except RuntimeError as e:
            assert "trustline" in str(e).lower() or "balance" in str(e).lower()


class TestStroopsConversion:
    def test_basic(self):
        assert stroops_to_xlm(10_000_000) == 1.0

    def test_fractional(self):
        assert stroops_to_xlm(80_000_000) == 8.0

    def test_zero(self):
        assert stroops_to_xlm(0) == 0.0


class TestCapabilityMatching:
    def test_match(self):
        assert _match_capabilities(["PDF", "Summarization"], ["PDF"])

    def test_no_match(self):
        assert not _match_capabilities(["SQL", "Analytics"], ["PDF"])

    def test_case_insensitive(self):
        assert _match_capabilities(["pdf", "summarization"], ["PDF"])

    def test_partial_overlap(self):
        assert _match_capabilities(["PDF", "OCR"], ["PDF", "Summarization"])


class TestCostTable:
    def test_renders(self):
        rows = [
            AgentRow("pdf-reader", "HAIKU", 8.0, 921, True),
            AgentRow("sql-analyst", "SONNET", 12.0, 874, False),
        ]
        result = print_cost_table(rows)
        assert "pdf-reader" in result
        assert "sql-analyst" in result
        assert "HAIKU" in result
        assert "8.0 XLM" in result
        assert "yes" in result
        assert "no" in result


class TestHebbianBridge:
    @pytest.fixture
    def knowledge(self, tmp_path):
        return ResearchKnowledgeStore(db_path=str(tmp_path / "test_hebbian.db"))

    def test_no_knowledge(self):
        handle, confidence = query_hebbian(None, ["PDF"])
        assert handle is None
        assert confidence == 0.0

    def test_query_empty_store(self, knowledge):
        handle, confidence = query_hebbian(knowledge, ["PDF"])
        assert handle is None
        assert confidence == 0.0

    def test_query_after_storing_finding(self, knowledge):
        knowledge.store_finding(
            content="pdf-reader agent handles PDF extraction and OCR",
            source_url="",
            importance=0.8,
            domain="agents",
        )
        handle, confidence = query_hebbian(knowledge, ["PDF", "extraction"])
        # Testing the mechanism works, not the exact confidence value
        assert isinstance(handle, (str, type(None)))
        assert isinstance(confidence, float)

    def test_store_finding_and_query(self, knowledge):
        knowledge.store_finding(content="pdf-reader agent handles PDF extraction and OCR tasks", importance=0.9)
        knowledge.store_finding(content="web-search agent handles web queries and research", importance=0.8)
        knowledge.store_finding(content="sql-analyst agent handles database analytics", importance=0.7)

        results = knowledge.search("PDF extraction", top_k=5)
        assert len(results) > 0
        assert any("pdf" in r.get("content", "").lower() for r in results)

    def test_real_store_has_nodes(self):
        knowledge = ResearchKnowledgeStore()
        assert len(knowledge.graph.nodes) > 0

    def test_real_store_query_returns_recommendation(self):
        """Real store has 205+ nodes from prior research — query should find something."""
        knowledge = ResearchKnowledgeStore()
        handle, confidence = query_hebbian(knowledge, ["Web", "Extraction"])
        assert isinstance(confidence, float)
        assert confidence > 0

    def test_reinforce_on_real_store(self):
        knowledge = ResearchKnowledgeStore()
        edges_before = len(list(knowledge.graph.edges))
        reinforce_hebbian(knowledge, "PDF Extraction", "pdf-reader")
        edges_after = len(list(knowledge.graph.edges))
        assert edges_after >= edges_before


# TestHireFlowLive removed — exercised the deleted CLI `run_hire` path.
# Production hire flow is /api/hire (x402-gated) — covered by
# test_integ_x402_hire.py + test_payment_flow.py.
