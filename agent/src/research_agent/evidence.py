"""The attestation layer: what justifies a settlement.

An escrow that releases on a button press proves nothing — any payment
processor does that. What this marketplace claims is narrower and harder: the
money moves only when the work has been checked, and the check is a fact on
the ledger that the client can read without trusting us.

So the sequence is: build an evidence package from what the agent actually
produced, hash it, write the verdict on-chain, and settle only if the chain
says the verdict passed. The hash is what makes the attestation worth
anything: it binds the verdict to one specific deliverable, so neither side can
swap the work afterwards and still point at the same attestation.

The contract (`contracts/evidence-attestation`) is append-only: one attestation
per task, no overwrite. The record is what a payout is justified by, and a
record that can be rewritten after the money has moved justifies nothing.
"""

from __future__ import annotations

import hashlib
import json
import os
from typing import Any

from stellar_sdk import Keypair, TransactionBuilder, scval
from stellar_sdk import xdr as stellar_xdr

from .stellar_config import NETWORK_PASSPHRASE, RPC_URL, get_soroban_server

#: Set by deployment. Reading it at call time rather than import time so a
#: process that loads .env after importing this module still sees it.
ENV_CONTRACT = "EVIDENCE_CONTRACT"


class EvidenceNotConfigured(RuntimeError):
    """No evidence contract is configured.

    Raised rather than returning a falsy value, because the whole point of the
    gate is that an absent attestation layer must not read as "approved". A
    caller that wants to run without the gate has to say so explicitly.
    """


def evidence_contract(contract_id: str | None = None) -> str:
    cid = (contract_id or os.environ.get(ENV_CONTRACT, "")).strip()
    if not cid:
        raise EvidenceNotConfigured(
            f"{ENV_CONTRACT} is not set. Settlement is gated on an on-chain "
            f"attestation, so there is nothing to check against. Deploy "
            f"contracts/evidence-attestation and set {ENV_CONTRACT}."
        )
    return cid


# ---------------------------------------------------------------------------
# The evidence package
# ---------------------------------------------------------------------------

def build_evidence_package(
    task_id: str,
    agent_handle: str,
    brief: str,
    output: str,
    called_tools: list | None = None,
    extra: dict | None = None,
) -> dict:
    """Assemble what is being attested to.

    Deliberately small and entirely derived from the task: the brief that was
    paid for, the output that was delivered, which tools ran, and the sizes.
    Nothing here is a judgement — the verdict is separate, so that anybody
    recomputing the hash gets the same answer whatever they think of the work.

    No timestamps and no run-specific identifiers: the hash has to be
    reproducible by a third party holding only the brief and the deliverable.
    That is what makes the verifier page possible.
    """
    tools = []
    for t in called_tools or []:
        # Tool records arrive in a few shapes depending on the path that
        # produced them. Only the name is part of the evidence.
        if isinstance(t, dict):
            name = t.get("name") or t.get("tool") or t.get("handle") or ""
        else:
            name = str(t)
        if name:
            tools.append(name)

    package = {
        "version": 1,
        "task_id": task_id,
        "agent_handle": agent_handle or "",
        "brief": brief or "",
        "output": output or "",
        "called_tools": sorted(tools),
        "input_words": len((brief or "").split()),
        "output_words": len((output or "").split()),
    }
    if extra:
        # Namespaced, so added fields can never shadow the ones above and
        # silently change what an older verifier recomputes.
        package["extra"] = {str(k): extra[k] for k in sorted(extra)}
    return package


def evidence_hash(package: dict) -> bytes:
    """SHA-256 of the canonical JSON form of the package.

    `sort_keys` plus `separators` plus `ensure_ascii=False` and UTF-8: the same
    package has to hash the same in this process, in the verifier page, and on
    somebody else's machine next year. Any looseness here turns the hash from
    evidence into noise.
    """
    canonical = json.dumps(
        package,
        sort_keys=True,
        separators=(",", ":"),
        ensure_ascii=False,
    ).encode("utf-8")
    return hashlib.sha256(canonical).digest()


def evidence_hash_hex(package: dict) -> str:
    return evidence_hash(package).hex()


# ---------------------------------------------------------------------------
# Writing
# ---------------------------------------------------------------------------

def attest(
    orchestrator_kp: Keypair,
    task_id: str,
    agent_address: str,
    evidence_hash_bytes: bytes,
    passed: bool,
    rpc_url: str | None = None,
    contract_id: str | None = None,
    wait_for_confirmation: bool = True,
) -> dict:
    """Record the verdict on-chain.

    Called for a failing verdict too. A refund needs its justification on the
    ledger as much as a payout does — more, arguably, since that is the side a
    client would dispute.

    Returns `{"hash", "status", "error"}`, the same shape as the escrow
    helpers, so callers handle both the same way.
    """
    if len(evidence_hash_bytes) != 32:
        raise ValueError(
            f"evidence hash must be 32 bytes, got {len(evidence_hash_bytes)}"
        )

    cid = evidence_contract(contract_id)
    rpc_url = rpc_url or RPC_URL
    server = get_soroban_server(rpc_url)
    source = server.load_account(orchestrator_kp.public_key)

    builder = TransactionBuilder(
        source_account=source,
        network_passphrase=NETWORK_PASSPHRASE,
        base_fee=1000,
    )
    builder.append_invoke_contract_function_op(
        contract_id=cid,
        function_name="attest",
        parameters=[
            scval.to_string(task_id),
            scval.to_address(agent_address),
            scval.to_bytes(evidence_hash_bytes),
            scval.to_bool(bool(passed)),
        ],
    )
    builder.set_timeout(60)
    tx = builder.build()

    sim = server.simulate_transaction(tx)
    if sim.error:
        # `AlreadyAttested` lands here. Surfaced as a distinct status rather
        # than an exception: a second attestation attempt is an expected event
        # (a retried settle), and the right response is to read the existing
        # one, not to fail the request.
        if "Error(Contract, #3)" in str(sim.error):
            return {
                "hash": None,
                "status": "ALREADY_ATTESTED",
                "error": "this task already has an attestation; "
                         "attestations are append-only",
            }
        raise RuntimeError(f"attest simulate error: {sim.error}")

    tx = server.prepare_transaction(tx, sim)
    tx.sign(orchestrator_kp)
    resp = server.send_transaction(tx)
    tx_hash = resp.hash

    if not wait_for_confirmation:
        return {"hash": tx_hash, "status": "SUBMITTED", "error": None}

    from .escrow import _wait_for_tx_confirmation

    confirmation = _wait_for_tx_confirmation(server, tx_hash)
    return {
        "hash": tx_hash,
        "status": confirmation["status"],
        "error": confirmation["error"],
    }


# ---------------------------------------------------------------------------
# Reading
# ---------------------------------------------------------------------------

def _simulate_read(
    fn_name: str,
    params: list,
    rpc_url: str | None,
    contract_id: str | None,
) -> Any:
    """Simulate a read-only call and return the decoded SCVal, or None.

    Reads go through simulation, so they cost nothing and need no signer. A
    throwaway keypair is used as the source account: the contract's read
    functions take no auth, and using the orchestrator here would mean the
    read path depended on a key it does not need.
    """
    cid = evidence_contract(contract_id)
    rpc_url = rpc_url or RPC_URL
    server = get_soroban_server(rpc_url)

    # An account that exists on the network is required to simulate. The
    # attester is guaranteed to exist, and simulation never signs.
    attester = _attester_address(server, cid)
    source = server.load_account(attester)

    builder = TransactionBuilder(
        source_account=source,
        network_passphrase=NETWORK_PASSPHRASE,
        base_fee=1000,
    )
    builder.append_invoke_contract_function_op(
        contract_id=cid, function_name=fn_name, parameters=params
    )
    builder.set_timeout(60)
    sim = server.simulate_transaction(builder.build())
    if sim.error or not sim.results:
        return None
    return stellar_xdr.SCVal.from_xdr(sim.results[0].xdr)


_ATTESTER_CACHE: dict[str, str] = {}


def _attester_address(server, contract_id: str) -> str:
    """The attester, read from the contract and remembered per contract.

    Needed as a source account for read simulations, and worth caching: it is
    set once by `init` and cannot change.
    """
    if contract_id in _ATTESTER_CACHE:
        return _ATTESTER_CACHE[contract_id]

    # Bootstrapped with a random keypair, which does not need to exist on the
    # network for this one call because `attester` reads instance storage and
    # the simulation does not check the source account's existence for it.
    probe = Keypair.random()
    try:
        source = server.load_account(probe.public_key)
    except Exception:
        # Not funded, as expected. Build the account stub by hand.
        from stellar_sdk import Account

        source = Account(probe.public_key, 0)

    builder = TransactionBuilder(
        source_account=source,
        network_passphrase=NETWORK_PASSPHRASE,
        base_fee=1000,
    )
    builder.append_invoke_contract_function_op(
        contract_id=contract_id, function_name="attester", parameters=[]
    )
    builder.set_timeout(60)
    sim = server.simulate_transaction(builder.build())
    if sim.error or not sim.results:
        raise RuntimeError(f"cannot read the contract's attester: {sim.error}")

    val = stellar_xdr.SCVal.from_xdr(sim.results[0].xdr)
    addr = scval.from_address(val).address if val.address else None
    if not addr:
        raise RuntimeError("the evidence contract has no attester: not initialised")
    _ATTESTER_CACHE[contract_id] = addr
    return addr


def get_attestation(
    task_id: str,
    rpc_url: str | None = None,
    contract_id: str | None = None,
) -> dict | None:
    """The attestation for a task, read from the chain, or None.

    None means "no attestation on this task", which is an ordinary answer and
    not an error — but see `settlement_allowed`: it is never an approval.
    """
    val = _simulate_read(
        "get_attestation", [scval.to_string(task_id)], rpc_url, contract_id
    )
    if val is None:
        return None
    # Option<Attestation>: void for None, a map for Some.
    if val.type == stellar_xdr.SCValType.SCV_VOID:
        return None
    try:
        fields = scval.from_struct(val)
    except Exception:
        return None

    def _get(name):
        return fields.get(name)

    agent = _get("agent")
    hash_val = _get("evidence_hash")
    passed = _get("passed")
    at = _get("attested_at")

    return {
        "task_id": task_id,
        "agent": scval.from_address(agent).address if agent is not None else None,
        "evidence_hash": scval.from_bytes(hash_val).hex() if hash_val is not None else None,
        "passed": bool(scval.from_bool(passed)) if passed is not None else None,
        "attested_at": int(scval.from_uint64(at)) if at is not None else None,
    }


def has_passing_attestation(
    task_id: str,
    rpc_url: str | None = None,
    contract_id: str | None = None,
) -> bool:
    val = _simulate_read(
        "has_passing_attestation", [scval.to_string(task_id)], rpc_url, contract_id
    )
    if val is None:
        return False
    try:
        return bool(scval.from_bool(val))
    except Exception:
        return False


def matches_evidence(
    task_id: str,
    evidence_hash_bytes: bytes,
    rpc_url: str | None = None,
    contract_id: str | None = None,
) -> bool:
    """Whether the chain's attestation is for exactly this evidence.

    This is the question the verifier page asks: somebody holds a deliverable
    and wants to know whether it is the one that was paid for. A passing
    attestation for a different hash is not an answer in their favour.
    """
    if len(evidence_hash_bytes) != 32:
        return False
    val = _simulate_read(
        "matches_evidence",
        [scval.to_string(task_id), scval.to_bytes(evidence_hash_bytes)],
        rpc_url,
        contract_id,
    )
    if val is None:
        return False
    try:
        return bool(scval.from_bool(val))
    except Exception:
        return False


# ---------------------------------------------------------------------------
# The gate
# ---------------------------------------------------------------------------

def settlement_allowed(
    task_id: str,
    package: dict,
    rpc_url: str | None = None,
    contract_id: str | None = None,
) -> tuple[bool, str]:
    """Whether the chain justifies paying for this deliverable.

    Returns `(allowed, reason)`. The reason is recorded either way, because
    "why did this settle" and "why did this not" are both things a client is
    entitled to ask later.

    Fails closed on every uncertainty. An attestation layer that cannot be
    reached, a task with no attestation, a verdict that failed, or an
    attestation whose hash belongs to a different deliverable — none of those
    is permission to move money. The absence of a readable answer is not a yes.
    """
    want = evidence_hash(package)

    try:
        record = get_attestation(task_id, rpc_url=rpc_url, contract_id=contract_id)
    except EvidenceNotConfigured as e:
        return False, f"no attestation layer configured: {e}"
    except Exception as e:
        return False, f"could not read the attestation ({type(e).__name__}: {e})"

    if record is None:
        return False, "no attestation on chain for this task"
    if not record.get("passed"):
        return False, "the attestation records a failing verdict"

    on_chain = (record.get("evidence_hash") or "").lower()
    if on_chain != want.hex():
        return False, (
            f"the attestation is for different evidence "
            f"(chain {on_chain[:16]}…, deliverable {want.hex()[:16]}…)"
        )

    return True, (
        f"attestation passed, evidence hash matches ({want.hex()[:16]}…), "
        f"attested at {record.get('attested_at')}"
    )


# ---------------------------------------------------------------------------
# The verdict
# ---------------------------------------------------------------------------

#: Prefixes an agent's output takes when it failed rather than delivered.
#: Checked case-insensitively on the first line only, so a report that
#: discusses errors is not mistaken for one.
_FAILURE_PREFIXES = (
    "error", "exception", "traceback", "failed", "unable to",
    "i cannot", "i can't", "i'm sorry", "sorry,",
)

#: A deliverable shorter than this is not a deliverable. Low on purpose: the
#: job of this check is to catch an empty or one-line response, not to judge
#: whether a short answer is a good answer.
MIN_OUTPUT_WORDS = 20

#: Ratio of output to input words above which the output is treated as padded.
#: Secondary by design — see `verify_delivery`.
MAX_COMPRESSION_RATIO_PCT = 200

#: The ratio is only meaningful against a brief with some substance in it. A
#: ten-word brief answered in fifty words is 500% and completely normal — that
#: is the service working, not padding. Measured: a good three-sentence answer
#: to a one-line brief failed the 200% limit outright.
#:
#: `api/server.py` had the same threshold for the same reason, but there the
#: ratio WAS the decision, so a short brief meant settling on no check at all.
#: Here the other three checks always run, which is what makes skipping this
#: one safe.
MIN_BRIEF_WORDS_FOR_RATIO = 100


def verify_delivery(
    package: dict,
    max_ratio_pct: int = MAX_COMPRESSION_RATIO_PCT,
) -> tuple[bool, list[dict]]:
    """Decide the verdict for an evidence package.

    Returns `(passed, checks)`, where each check is
    `{"name", "passed", "detail"}` and `passed` is true only if every check is.
    The list is returned whatever the outcome, because the reason a delivery
    was rejected is the part a client will argue with.

    **What this does and does not check.** These are structural checks on the
    deliverable: that something was produced, that it is not an error message,
    that it is not padding. They are objective and reproducible by anybody
    holding the package — which is what lets the verdict live on a public
    ledger.

    They are *not* a judgement of whether the work is correct or useful. That
    needs a model reading the brief against the output, and this marketplace
    does not have an LLM provider configured yet. Calling structural checks
    "verification" and stopping there would be the dishonest version of this
    feature, so the distinction is in the check names and in what the verifier
    page shows.

    The compression ratio is deliberately one check among several rather than
    the decision. It used to be the whole decision, which meant a task could
    settle purely because its output was short enough — and, because
    `api/server.py` skipped the check below 100 input words, most tasks settled
    on no check at all.
    """
    output = (package.get("output") or "").strip()
    first_line = output.split("\n", 1)[0].strip().lower()
    in_words = int(package.get("input_words") or 0)
    out_words = int(package.get("output_words") or 0)

    checks: list[dict] = []

    checks.append({
        "name": "delivered_something",
        "passed": bool(output),
        "detail": f"{out_words} words" if output else "the output is empty",
    })

    hit = next((p for p in _FAILURE_PREFIXES if first_line.startswith(p)), None)
    checks.append({
        "name": "not_an_error_message",
        "passed": hit is None,
        "detail": "ok" if hit is None else f"the output opens with {hit!r}",
    })

    checks.append({
        "name": "long_enough_to_be_a_deliverable",
        "passed": out_words >= MIN_OUTPUT_WORDS,
        "detail": f"{out_words} words, minimum {MIN_OUTPUT_WORDS}",
    })

    # Secondary signal, and only applied where it means something.
    if in_words >= MIN_BRIEF_WORDS_FOR_RATIO:
        ratio = out_words * 100 // in_words
        checks.append({
            "name": "within_compression_limit",
            "passed": ratio <= max_ratio_pct,
            "detail": f"{ratio}% of the brief, limit {max_ratio_pct}%",
        })
    else:
        checks.append({
            "name": "within_compression_limit",
            "passed": True,
            "detail": (
                f"brief is {in_words} words, under the {MIN_BRIEF_WORDS_FOR_RATIO} "
                f"needed for a ratio to mean anything — not measured"
            ),
        })

    return all(c["passed"] for c in checks), checks
