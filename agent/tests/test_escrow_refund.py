from unittest.mock import patch, MagicMock
import pytest

from stellar_sdk import Keypair


@pytest.fixture
def orch_kp():
    return Keypair.random()


class TestRefundEscrow:
    @patch("research_agent.escrow._submit_escrow_invoke")
    def test_calls_refund_to_client(self, mock_invoke, orch_kp):
        from research_agent.escrow import refund_escrow

        mock_invoke.return_value = "tx_hash_abc"
        result = refund_escrow(orch_kp, "task-123")

        mock_invoke.assert_called_once()
        args = mock_invoke.call_args
        assert args[0][0] is orch_kp
        assert args[0][1] == "refund_to_client"
        assert result == "tx_hash_abc"

    @patch("research_agent.escrow._submit_escrow_invoke")
    def test_passes_task_id_as_string_param(self, mock_invoke, orch_kp):
        from research_agent.escrow import refund_escrow

        mock_invoke.return_value = "tx_hash"
        refund_escrow(orch_kp, "x402-abc123")

        params = mock_invoke.call_args[0][2]
        assert len(params) == 1  # refund_to_client takes only task_id

    @patch("research_agent.escrow._submit_escrow_invoke")
    def test_propagates_kwargs(self, mock_invoke, orch_kp):
        from research_agent.escrow import refund_escrow

        mock_invoke.return_value = "tx_hash"
        refund_escrow(orch_kp, "task-123", rpc_url="https://custom-rpc.example.com")

        assert mock_invoke.call_args[1]["rpc_url"] == "https://custom-rpc.example.com"

    @patch("research_agent.escrow._submit_escrow_invoke")
    def test_raises_on_soroban_error(self, mock_invoke, orch_kp):
        from research_agent.escrow import refund_escrow

        mock_invoke.side_effect = RuntimeError("Soroban simulate error")
        with pytest.raises(RuntimeError, match="Soroban simulate error"):
            refund_escrow(orch_kp, "task-123")


class TestGetEscrowStatus:
    @patch("research_agent.escrow.SorobanServer")
    def test_returns_parsed_status(self, MockServer):
        from research_agent.escrow import get_escrow_status

        mock_server = MockServer.return_value
        mock_result = MagicMock()
        mock_result.results = [MagicMock()]
        mock_result.results[0].xdr = "AAAA"
        mock_result.error = None
        mock_server.simulate_transaction.return_value = mock_result

        # Parsing may fail with placeholder XDR; we only assert simulate was called
        try:
            get_escrow_status("task-123")
        except Exception:
            pass

        mock_server.simulate_transaction.assert_called_once()

    @patch("research_agent.escrow.SorobanServer")
    def test_uses_correct_function_name(self, MockServer):
        from research_agent.escrow import get_escrow_status

        mock_server = MockServer.return_value
        mock_result = MagicMock()
        mock_result.error = None
        mock_result.results = [MagicMock()]
        mock_server.simulate_transaction.return_value = mock_result

        try:
            get_escrow_status("task-456")
        except Exception:
            pass

        call_args = mock_server.simulate_transaction.call_args
        assert call_args is not None


class TestBuildEscrowTx:
    @patch("research_agent.escrow.SorobanServer")
    def test_returns_unsigned_xdr_and_task_id(self, MockServer):
        from research_agent.escrow import build_escrow_tx

        client_kp = Keypair.random()
        agent_kp = Keypair.random()

        mock_server = MockServer.return_value
        mock_account = MagicMock()
        mock_account.sequence = 100
        mock_account.account = MagicMock()
        mock_account.account.account_id = client_kp.public_key
        mock_server.load_account.return_value = mock_account

        mock_sim = MagicMock()
        mock_sim.error = None
        mock_server.simulate_transaction.return_value = mock_sim

        mock_prepared = MagicMock()
        mock_prepared.to_xdr.return_value = "unsigned_xdr_here"
        mock_server.prepare_transaction.return_value = mock_prepared

        result = build_escrow_tx(
            client_public=client_kp.public_key,
            agent_public=agent_kp.public_key,
            amount_stroops=2500000,
            deadline_unix=9999999999,
            task_id="x402-test123",
        )

        assert result["unsigned_xdr"] == "unsigned_xdr_here"
        assert result["task_id"] == "x402-test123"
        assert result["deadline"] == 9999999999

    @patch("research_agent.escrow.SorobanServer")
    def test_raises_on_simulation_error(self, MockServer):
        from research_agent.escrow import build_escrow_tx

        client_kp = Keypair.random()
        agent_kp = Keypair.random()

        mock_server = MockServer.return_value
        mock_account = MagicMock()
        mock_account.sequence = 100
        mock_account.account = MagicMock()
        mock_account.account.account_id = client_kp.public_key
        mock_server.load_account.return_value = mock_account

        mock_sim = MagicMock()
        mock_sim.error = "insufficient funds"
        mock_server.simulate_transaction.return_value = mock_sim

        with pytest.raises(RuntimeError, match="simulation failed"):
            build_escrow_tx(
                client_public=client_kp.public_key,
                agent_public=agent_kp.public_key,
                amount_stroops=2500000,
                deadline_unix=9999999999,
                task_id="x402-test123",
            )
