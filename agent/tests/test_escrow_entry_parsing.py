"""Decoding of the EscrowEntry struct returned by usdc-escrow.get_escrow.

These build the SCVal the contract would return and read it back, so they run
without a network and without a live escrow on testnet. The shape under test
mirrors `EscrowEntry` in contracts/usdc-escrow/src/lib.rs — if that struct
changes, these fail, which is the point.
"""

import pytest
from stellar_sdk import scval

from research_agent.escrow import (
    ESCROW_STATUSES,
    STROOPS_PER_UNIT,
    _parse_escrow_entry,
)

CLIENT = "GAT6TN4C2ECKUMKZTTRQS65RATLH7W7FG3UYPBQEWTEWL5PET2WR3F6X"
AGENT = "GCLSOM36L7OYFZ6LNLDCNDOBXXKKGN7NFD2MST7PMJLCSAZDC7EXDRBO"


def build_entry_xdr(
    *,
    client: str = CLIENT,
    agent: str = AGENT,
    amount: int = 4_500_000,
    deadline: int = 1_792_600_000,
    status: str = "Created",
) -> str:
    """Serialise an EscrowEntry the way the contract does."""
    return scval.to_struct(
        {
            "client": scval.to_address(client),
            "agent": scval.to_address(agent),
            "amount": scval.to_int128(amount),
            "deadline": scval.to_uint64(deadline),
            "status": scval.to_enum(status, None),
        }
    ).to_xdr()


class TestParseEscrowEntry:
    def test_decodes_every_field(self):
        result = _parse_escrow_entry("task-1", build_entry_xdr())

        assert result["task_id"] == "task-1"
        assert result["status"] == "found"
        assert result["client"] == CLIENT
        assert result["agent"] == AGENT
        assert result["amount_stroops"] == 4_500_000
        assert result["deadline"] == 1_792_600_000
        assert result["escrow_status"] == "Created"
        assert "parse_error" not in result

    def test_amount_is_rendered_in_whole_units(self):
        # 4_500_000 stroops is 0.45 USDC. Reporting stroops to a user interface
        # is how 0.45 turns into "four and a half million" on a dashboard.
        result = _parse_escrow_entry("task-1", build_entry_xdr(amount=4_500_000))
        assert result["amount"] == "0.4500000"
        assert float(result["amount"]) * STROOPS_PER_UNIT == 4_500_000

    @pytest.mark.parametrize("status", ESCROW_STATUSES)
    def test_decodes_each_terminal_status(self, status):
        result = _parse_escrow_entry("t", build_entry_xdr(status=status))
        assert result["escrow_status"] == status

    def test_keeps_the_raw_xdr(self):
        # The dashboard shows decoded values; a reviewer checking our work needs
        # the bytes the chain actually returned.
        raw = build_entry_xdr()
        assert _parse_escrow_entry("t", raw)["raw_xdr"] == raw

    def test_large_amount_survives_the_i128(self):
        # i128 is wider than a JSON number. Anything that silently narrows it
        # would corrupt a balance, so check a value past 2^53.
        big = 2**60
        result = _parse_escrow_entry("t", build_entry_xdr(amount=big))
        assert result["amount_stroops"] == big

    def test_malformed_xdr_is_reported_not_raised(self):
        # A caller asking for escrow state should not get an exception because
        # the contract's shape moved underneath it.
        result = _parse_escrow_entry("t", scval.to_string("not a struct").to_xdr())

        assert result["status"] == "found"
        assert "parse_error" in result
        assert "client" not in result
