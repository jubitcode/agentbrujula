"""x402 payment verification: amounts come from the chain, not from the caller.

The scenarios here are the ones that decide whether somebody can be paid less
than they quoted, or paid on the wrong network. The operation shapes mirror what
Stellar Horizon really returns for a Soroban escrow creation — checked against a
live testnet transaction, where the op type is `invoke_host_function` and the
amount lives in `asset_balance_changes`.
"""

import pytest

from research_agent.x402_middleware import _transfer_to_recipient

ESCROW = "CAMPNA3TNE23UYM5WSTXHQWA5HGU7HL6M2T64R5VNQT5PJS6UBIXOCCO"
PAYER = "GCKRS77C3OEUFPE4UEDYPPVS2I7357QNCBUMYTJDLR5PNQNBU2S26RNL"


def soroban_op(*, to=ESCROW, amount="0.6750000", asset="USDC", kind="transfer"):
    """An `invoke_host_function` operation as Horizon reports it."""
    return {
        "type": "invoke_host_function",
        "type_i": 24,
        "asset_balance_changes": [
            {
                "asset_type": "credit_alphanum4",
                "asset_code": asset,
                "type": kind,
                "from": PAYER,
                "to": to,
                "amount": amount,
            }
        ],
    }


def payment_op(*, to=ESCROW, amount="0.6750000", asset="USDC"):
    """A classic payment operation."""
    return {
        "type": "payment",
        "from": PAYER,
        "to": to,
        "amount": amount,
        "asset_code": asset,
    }


class TestSorobanEscrowPayments:
    """The real escrow flow is a contract invocation, not a classic payment."""

    def test_accepts_a_soroban_transfer_into_the_escrow(self):
        found = _transfer_to_recipient(soroban_op(), ESCROW, 0.45, "USDC")

        assert found is not None
        assert found["amount"] == pytest.approx(0.675)
        assert found["payer"] == PAYER

    def test_rejects_a_transfer_to_someone_else(self):
        # Paying a different contract the right amount is not paying us.
        other = "CBOTHERCONTRACTADDRESSXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
        assert _transfer_to_recipient(soroban_op(to=other), ESCROW, 0.45, "USDC") is None

    def test_rejects_an_amount_below_what_was_quoted(self):
        # This is the case that decides whether a client can underpay the
        # ceiling it was quoted in the 402.
        assert _transfer_to_recipient(soroban_op(amount="0.1000000"), ESCROW, 0.45, "USDC") is None

    def test_accepts_exactly_the_quoted_amount(self):
        assert _transfer_to_recipient(soroban_op(amount="0.4500000"), ESCROW, 0.45, "USDC") is not None

    def test_rejects_a_different_asset(self):
        # Paying 0.675 of some other token is not paying 0.675 USDC.
        assert _transfer_to_recipient(soroban_op(asset="SCAM"), ESCROW, 0.45, "USDC") is None

    def test_ignores_balance_changes_that_are_not_transfers(self):
        # Mints and burns appear in the same list and must not count as payment.
        assert _transfer_to_recipient(soroban_op(kind="mint"), ESCROW, 0.45, "USDC") is None

    def test_a_malformed_amount_is_not_treated_as_zero(self):
        # "" parsed as 0.0 would pass any check with a 0 minimum, so a value we
        # cannot read has to be refused outright.
        assert _transfer_to_recipient(soroban_op(amount=""), ESCROW, 0.45, "USDC") is None
        assert _transfer_to_recipient(soroban_op(amount=None), ESCROW, 0.45, "USDC") is None

    def test_picks_the_matching_transfer_out_of_several(self):
        op = soroban_op()
        op["asset_balance_changes"] = [
            {"type": "transfer", "asset_code": "USDC", "from": PAYER, "to": "CBSOMEONEELSE", "amount": "99"},
            {"type": "transfer", "asset_code": "USDC", "from": PAYER, "to": ESCROW, "amount": "0.5000000"},
        ]
        found = _transfer_to_recipient(op, ESCROW, 0.45, "USDC")
        assert found is not None and found["amount"] == pytest.approx(0.5)


class TestClassicPayments:
    """Flat x402 payments that never touch a contract still have to work."""

    def test_accepts_a_classic_payment(self):
        found = _transfer_to_recipient(payment_op(), ESCROW, 0.45, "USDC")
        assert found is not None
        assert found["payer"] == PAYER

    def test_rejects_an_underpaying_classic_payment(self):
        assert _transfer_to_recipient(payment_op(amount="0.01"), ESCROW, 0.45, "USDC") is None

    def test_ignores_unrelated_operation_types(self):
        assert _transfer_to_recipient({"type": "create_account"}, ESCROW, 0.0, "USDC") is None


class TestNetworkGuards:
    """Configuration that could let a testnet payment settle a mainnet debt."""

    def test_mainnet_requires_an_explicit_opt_in(self, monkeypatch):
        import importlib

        monkeypatch.setenv("STELLAR_NETWORK", "public")
        monkeypatch.setenv("X402_ENABLED", "true")
        monkeypatch.setenv("X402_ALLOW_MAINNET", "false")
        monkeypatch.setenv("X402_USDC_ADDRESS", "C" + "A" * 55)
        monkeypatch.setenv("X402_ESCROW_CONTRACT", "C" + "B" * 55)
        monkeypatch.delenv("X402_NETWORK", raising=False)

        import research_agent.stellar_config as sc
        import research_agent.x402_config as xc

        importlib.reload(sc)
        with pytest.raises(RuntimeError, match="X402_ALLOW_MAINNET"):
            importlib.reload(xc)

    def test_contradicting_network_settings_refuse_to_start(self, monkeypatch):
        # Reading testnet Horizon while advertising mainnet in the 402 is how a
        # testnet payment gets honoured as a real one.
        import importlib

        monkeypatch.setenv("STELLAR_NETWORK", "testnet")
        monkeypatch.setenv("X402_NETWORK", "stellar:mainnet")
        monkeypatch.setenv("X402_ENABLED", "true")

        import research_agent.stellar_config as sc
        import research_agent.x402_config as xc

        importlib.reload(sc)
        with pytest.raises(RuntimeError, match="contradicts"):
            importlib.reload(xc)

    def test_testnet_default_starts_cleanly(self, monkeypatch):
        import importlib

        monkeypatch.setenv("STELLAR_NETWORK", "testnet")
        monkeypatch.delenv("X402_NETWORK", raising=False)
        monkeypatch.delenv("X402_ALLOW_MAINNET", raising=False)

        import research_agent.stellar_config as sc
        import research_agent.x402_config as xc

        importlib.reload(sc)
        importlib.reload(xc)
        assert xc.X402_NETWORK == "stellar:testnet"
        assert xc.X402_IS_MAINNET is False
