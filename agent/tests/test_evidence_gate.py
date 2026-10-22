"""The gate that decides whether money moves.

An escrow that releases on a signature proves nothing — any payment processor
does that. What this marketplace claims is that the release is justified by a
check recorded on Stellar, which the client can read without trusting us.

So the properties worth fixing are about refusal, not about the happy path:

1. The hash binds a verdict to one specific deliverable, reproducibly.
2. The verdict is made of named, objective checks, and the compression ratio
   is one of them rather than all of them.
3. Every way the gate can fail to get an answer reads as "no".

The chain is doubled here. The real contract is exercised against testnet in
`manual_evidence_e2e.py`; these are about the decision logic, which has to be
deterministic and fast enough to run on every change.
"""

import hashlib

import pytest

from research_agent import evidence as ev


BRIEF = "Analyse the Stellar agent tooling landscape and name the gaps."
GOOD = (
    "Three structural gaps stand out. First, no settlement gate: escrow "
    "contracts release on a signature rather than on a check of the work. "
    "Second, no shared identity primitive for agents. Third, no evidence "
    "trail a client can audit without trusting the platform."
)
LONG_BRIEF = " ".join(
    ["Produce a detailed competitive analysis of Stellar agent tooling, "
     "covering settlement, identity and evidence."] * 9
)


def pkg(brief=BRIEF, output=GOOD, **kw):
    return ev.build_evidence_package(
        task_id=kw.pop("task_id", "t1"),
        agent_handle=kw.pop("agent_handle", "competitive-intel"),
        brief=brief,
        output=output,
        **kw,
    )


# ---------------------------------------------------------------------------
# 1. The hash
# ---------------------------------------------------------------------------

class TestEvidenceHashIsEvidence:
    def test_the_same_deliverable_hashes_the_same(self):
        assert ev.evidence_hash(pkg()) == ev.evidence_hash(pkg())

    def test_it_is_reproducible_from_the_package_alone(self):
        """A third party holding the brief and the output must arrive at the
        same hash. That is what makes the verifier page possible, and what
        stops the attestation from being a claim only we can check."""
        mine = ev.evidence_hash_hex(pkg())
        theirs = ev.evidence_hash_hex(
            ev.build_evidence_package("t1", "competitive-intel", BRIEF, GOOD)
        )
        assert mine == theirs

    def test_changing_the_output_changes_the_hash(self):
        edited = pkg(output=GOOD + " (edited after payment)")
        assert ev.evidence_hash(edited) != ev.evidence_hash(pkg())

    def test_changing_the_brief_changes_the_hash(self):
        assert ev.evidence_hash(pkg(brief=BRIEF + "?")) != ev.evidence_hash(pkg())

    def test_tool_order_does_not(self):
        """Callers collect tool records in whatever order they ran. The set is
        the evidence; the ordering is not, and letting it matter would make
        the same work hash differently on a re-run."""
        a = pkg(called_tools=["web_search", "summarise"])
        b = pkg(called_tools=["summarise", "web_search"])
        assert ev.evidence_hash(a) == ev.evidence_hash(b)

    def test_tool_records_of_different_shapes_normalise(self):
        a = pkg(called_tools=[{"name": "web_search"}])
        b = pkg(called_tools=["web_search"])
        c = pkg(called_tools=[{"tool": "web_search"}])
        assert ev.evidence_hash(a) == ev.evidence_hash(b) == ev.evidence_hash(c)

    def test_the_package_carries_no_timestamp_or_run_id(self):
        """Anything that varies between runs would make the hash unverifiable
        by somebody holding only the deliverable."""
        p = pkg()
        flat = str(p).lower()
        for word in ("time", "date", "created", "uuid", "run_id", "nonce"):
            assert word not in flat, f"{word!r} would make the hash unreproducible"

    def test_extra_fields_cannot_shadow_the_real_ones(self):
        p = pkg(extra={"output": "something else", "task_id": "other"})
        assert p["output"] == GOOD
        assert p["task_id"] == "t1"
        assert p["extra"]["output"] == "something else"

    def test_it_is_thirty_two_bytes(self):
        assert len(ev.evidence_hash(pkg())) == 32


# ---------------------------------------------------------------------------
# 2. The verdict
# ---------------------------------------------------------------------------

class TestVerdict:
    def test_a_real_deliverable_passes(self):
        passed, checks = ev.verify_delivery(pkg())
        assert passed, [c for c in checks if not c["passed"]]
        assert len(checks) == 4, "a check disappeared without the list changing"

    def test_an_empty_output_fails(self):
        passed, checks = ev.verify_delivery(pkg(output=""))
        assert not passed
        assert not _check(checks, "delivered_something")["passed"]

    def test_an_error_message_is_not_a_deliverable(self):
        passed, checks = ev.verify_delivery(pkg(
            output="Error: the upstream model timed out after three retries "
                   "and produced nothing at all for this brief."
        ))
        assert not passed
        assert not _check(checks, "not_an_error_message")["passed"]

    @pytest.mark.parametrize("opening", [
        "Error: ...", "Exception in thread", "Traceback (most recent call last)",
        "Failed to reach the provider", "Unable to complete the brief",
        "I cannot help with that", "I'm sorry, I can't do this",
    ])
    def test_the_usual_failure_openings_are_caught(self, opening):
        text = opening + " " + " ".join(["filler"] * 40)
        _, checks = ev.verify_delivery(pkg(output=text))
        assert not _check(checks, "not_an_error_message")["passed"]

    def test_a_report_that_discusses_errors_still_passes(self):
        """Only the first line is checked, and only as a prefix. A competitive
        analysis whose body mentions errors is not an error message."""
        text = ("The landscape has three gaps worth naming in detail here. "
                "Error handling across these tools is inconsistent, and "
                "failures are reported late. " + " ".join(["more"] * 30))
        passed, checks = ev.verify_delivery(pkg(output=text))
        assert _check(checks, "not_an_error_message")["passed"]
        assert passed

    def test_a_one_word_answer_fails(self):
        passed, checks = ev.verify_delivery(pkg(output="Done."))
        assert not passed
        assert not _check(checks, "long_enough_to_be_a_deliverable")["passed"]


class TestTheRatioIsSecondary:
    def test_padding_against_a_substantial_brief_fails(self):
        passed, checks = ev.verify_delivery(
            pkg(brief=LONG_BRIEF, output=" ".join(["padding"] * 600))
        )
        assert not passed
        assert not _check(checks, "within_compression_limit")["passed"]

    def test_a_short_brief_answered_well_is_not_punished(self):
        """A ten-word brief answered in fifty words is 500%, and completely
        normal. Measured: the old 200% limit rejected a good three-sentence
        answer to a one-line brief."""
        passed, checks = ev.verify_delivery(pkg())
        ratio = _check(checks, "within_compression_limit")
        assert passed
        assert ratio["passed"]
        assert "not measured" in ratio["detail"]

    def test_skipping_the_ratio_is_not_skipping_the_verdict(self):
        """This is the difference from the old code. There the ratio WAS the
        decision and `if prompt_tokens < 100` set `compliant = True`, so a
        short brief meant settling on no check at all. Here the other three
        still run."""
        passed, checks = ev.verify_delivery(pkg(brief="short brief", output=""))
        assert not passed
        assert _check(checks, "within_compression_limit")["passed"]
        assert not _check(checks, "delivered_something")["passed"]

    def test_the_ratio_alone_cannot_pass_a_delivery(self):
        passed, _ = ev.verify_delivery(pkg(brief=LONG_BRIEF, output="Error: no."))
        assert not passed


def _check(checks, name):
    for c in checks:
        if c["name"] == name:
            return c
    raise AssertionError(f"no check named {name!r}; got "
                         f"{[c['name'] for c in checks]}")


# ---------------------------------------------------------------------------
# 3. The gate fails closed
# ---------------------------------------------------------------------------

class TestGateFailsClosed:
    """Every way of not getting an answer has to read as "no".

    This is the same principle the refund sweeper needed: the absence of a
    readable answer is not authorisation. There it was about returning money;
    here it is about paying it out.
    """

    def test_no_contract_configured(self, monkeypatch):
        monkeypatch.delenv("EVIDENCE_CONTRACT", raising=False)
        allowed, why = ev.settlement_allowed("t1", pkg())
        assert allowed is False
        assert "no attestation layer configured" in why

    def test_no_attestation_on_chain(self, monkeypatch):
        monkeypatch.setattr(ev, "get_attestation", lambda *a, **k: None)
        allowed, why = ev.settlement_allowed("t1", pkg())
        assert allowed is False
        assert "no attestation on chain" in why

    def test_a_failing_verdict(self, monkeypatch):
        monkeypatch.setattr(ev, "get_attestation", lambda *a, **k: {
            "passed": False,
            "evidence_hash": ev.evidence_hash_hex(pkg()),
        })
        allowed, why = ev.settlement_allowed("t1", pkg())
        assert allowed is False
        assert "failing verdict" in why

    def test_an_attestation_for_different_evidence(self, monkeypatch):
        """The attack this closes: pass a small deliverable, get the
        attestation, then hand over something else and point at the same
        on-chain record."""
        monkeypatch.setattr(ev, "get_attestation", lambda *a, **k: {
            "passed": True,
            "evidence_hash": hashlib.sha256(b"what was actually checked").hexdigest(),
        })
        allowed, why = ev.settlement_allowed("t1", pkg())
        assert allowed is False
        assert "different evidence" in why

    def test_the_chain_being_unreachable(self, monkeypatch):
        def boom(*a, **k):
            raise ConnectionError("soroban-testnet.stellar.org unreachable")

        monkeypatch.setattr(ev, "get_attestation", boom)
        allowed, why = ev.settlement_allowed("t1", pkg())
        assert allowed is False
        assert "could not read the attestation" in why

    def test_a_malformed_record(self, monkeypatch):
        monkeypatch.setattr(ev, "get_attestation",
                            lambda *a, **k: {"passed": True})
        allowed, why = ev.settlement_allowed("t1", pkg())
        assert allowed is False

    def test_it_opens_only_on_a_pass_for_this_exact_evidence(self, monkeypatch):
        p = pkg()
        monkeypatch.setattr(ev, "get_attestation", lambda *a, **k: {
            "passed": True,
            "evidence_hash": ev.evidence_hash_hex(p),
            "attested_at": 1792560122,
        })
        allowed, why = ev.settlement_allowed("t1", p)
        assert allowed is True
        assert "hash matches" in why

    def test_an_edited_deliverable_closes_it_again(self, monkeypatch):
        """Same on-chain attestation, deliverable changed afterwards."""
        original = pkg()
        monkeypatch.setattr(ev, "get_attestation", lambda *a, **k: {
            "passed": True,
            "evidence_hash": ev.evidence_hash_hex(original),
            "attested_at": 1792560122,
        })
        assert ev.settlement_allowed("t1", original)[0] is True

        edited = pkg(output=GOOD + " (edited after payment)")
        allowed, why = ev.settlement_allowed("t1", edited)
        assert allowed is False
        assert "different evidence" in why


class TestConfiguration:
    def test_an_absent_contract_raises_rather_than_returning_falsy(
        self, monkeypatch
    ):
        """A falsy return would get mixed up with "no attestation", and the
        operator needs to know the difference between "this task was not
        checked" and "nothing can check anything"."""
        monkeypatch.delenv("EVIDENCE_CONTRACT", raising=False)
        with pytest.raises(ev.EvidenceNotConfigured):
            ev.evidence_contract()

    def test_it_is_read_at_call_time(self, monkeypatch):
        """So a process that loads .env after importing still sees it."""
        monkeypatch.setenv("EVIDENCE_CONTRACT", "CDW2GATRG5M3AZQ")
        assert ev.evidence_contract() == "CDW2GATRG5M3AZQ"

    def test_an_explicit_argument_wins(self, monkeypatch):
        monkeypatch.setenv("EVIDENCE_CONTRACT", "CFROMENV")
        assert ev.evidence_contract("CEXPLICIT") == "CEXPLICIT"

    def test_attest_refuses_a_hash_that_is_not_32_bytes(self):
        with pytest.raises(ValueError, match="32 bytes"):
            ev.attest(None, "t1", "GABC", b"too short", True)
