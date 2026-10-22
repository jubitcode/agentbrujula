#!/usr/bin/env python3
"""The two paths that make the claim, end to end on testnet.

    python3 scripts/demo-evidence-cycle.py           # both paths
    python3 scripts/demo-evidence-cycle.py --pass    # only the settling one
    python3 scripts/demo-evidence-cycle.py --fail    # only the refunding one

Anybody can build a marketplace where a button releases escrow. What has to be
demonstrated here is narrower: that the money follows a verdict recorded on the
ledger, and that a failing verdict actually returns the client's funds.

So this runs both:

    PASS   lock USDC -> attest a passing verdict -> settle -> the agent is paid
    FAIL   lock USDC -> attest a failing verdict -> refund -> the client is whole

Every step is a real transaction on Stellar testnet, and every hash is printed
so it can be opened in an explorer. Balances are read before and after from
Horizon, not from our own database, because the point is that the ledger is
what says what happened.

It uses fresh task ids every run and the `testclient` / `testagent` identities.
It never touches task `40f6f3daf4c546fda37c19afbe8bcc5f`, whose escrow holds
0.675 USDC and is under investigation.
"""

from __future__ import annotations

import argparse
import os
import subprocess
import sys
import time
import uuid
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(REPO / "agent" / "src"))

#: Never operated on by this script, at any point.
PROTECTED_TASK = "40f6f3daf4c546fda37c19afbe8bcc5f"

BRIEF = (
    "Analyse the Stellar agent tooling landscape as of October 2026 and name "
    "the structural gaps that keep autonomous agents from transacting with "
    "each other without a trusted intermediary. Cover settlement, identity and "
    "evidence. Be specific about what exists today and what does not, and say "
    "which gap you would close first and why. Do not speculate about roadmaps "
    "or unannounced work; restrict yourself to what is deployed and verifiable "
    "on chain today, and name the contracts or standards you are relying on so "
    "a reader can check each claim independently without taking your word for "
    "any part of it, including the parts you are most confident about."
)

GOOD_OUTPUT = (
    "Three gaps hold back agent-to-agent transactions on Stellar today.\n\n"
    "Settlement exists but is unconditional. Escrow contracts release on an "
    "authorised call, which means somebody still decides. Trustless Work has "
    "audited escrow in mainnet; what neither it nor anything else deployed "
    "provides is a release condition tied to the work itself.\n\n"
    "Identity is unsolved. ERC-8004 came from Ethereum and the Soroban port "
    "(trionlabs/stellar-8004, MIT) is a third-party implementation without SDF "
    "backing. SEP-53 proves control of a key, not of an account, which matters "
    "under multisig.\n\n"
    "Evidence has no home. Nothing on chain records what an agent delivered, "
    "so a client who disputes a payout has nothing to point at.\n\n"
    "Close the evidence gap first: it is the precondition for the other two. "
    "A settlement gate needs something to gate on, and reputation needs a "
    "record to accumulate over."
)

#: Deliberately padded: 600 words of nothing against a 100-word brief trips
#: `within_compression_limit`, which is the check this path is meant to fail.
BAD_OUTPUT = (
    "In conclusion, the landscape is complex and multifaceted. " * 60
)


def run(cmd: list[str], timeout: int = 120) -> str:
    r = subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)
    if r.returncode != 0:
        raise RuntimeError(f"{' '.join(cmd[:3])}: {r.stderr.strip()[:300]}")
    return r.stdout.strip()


def secret(alias: str) -> str:
    return run(["stellar", "keys", "show", alias], timeout=15)


def address(alias: str) -> str:
    return run(["stellar", "keys", "address", alias], timeout=15)


def usdc_balance(public_key: str, issuer: str) -> float:
    import httpx

    r = httpx.get(
        f"https://horizon-testnet.stellar.org/accounts/{public_key}",
        timeout=20.0,
    )
    r.raise_for_status()
    for b in r.json().get("balances", []):
        if b.get("asset_code") == "USDC" and b.get("asset_issuer") == issuer:
            return float(b["balance"])
    return 0.0


def expl(tx: str) -> str:
    return f"https://stellar.expert/explorer/testnet/tx/{tx}"


def step(n: str, text: str) -> None:
    print(f"\n\033[1m{n}\033[0m {text}")


def ok(text: str) -> None:
    print(f"  \033[32mok\033[0m   {text}")


def bad(text: str) -> None:
    print(f"  \033[31mFAIL\033[0m {text}")


def run_cycle(should_pass: bool, amount_usdc: str = "0.05") -> dict:
    """One full cycle. Returns a record of what happened, for the archive."""
    from stellar_sdk import Keypair
    from research_agent import evidence as ev
    from research_agent.escrow import (
        build_escrow_tx,
        settle_escrow,
        refund_escrow,
        get_escrow_status,
    )
    from research_agent.stellar_config import NETWORK_PASSPHRASE, HORIZON_URL
    from stellar_sdk import Server, TransactionEnvelope

    label = "PASS -> settle" if should_pass else "FAIL -> refund"
    print(f"\n{'=' * 72}\n\033[1m{label}\033[0m\n{'=' * 72}")

    client_kp = Keypair.from_secret(secret("testclient"))
    agent_kp = Keypair.from_secret(secret("testagent"))
    orch_kp = Keypair.from_secret(secret("brujula-orch"))
    issuer = orch_kp.public_key  # the orchestrator issues the testnet USDC

    task_id = ("pass-" if should_pass else "fail-") + uuid.uuid4().hex[:10]
    assert task_id != PROTECTED_TASK
    amount_stroops = int(float(amount_usdc) * 10_000_000)
    record: dict = {"task_id": task_id, "path": label, "amount_usdc": amount_usdc}

    step("0.", "balances before, read from Horizon")
    before = {
        "client": usdc_balance(client_kp.public_key, issuer),
        "agent": usdc_balance(agent_kp.public_key, issuer),
    }
    print(f"       client {before['client']:.7f} USDC")
    print(f"       agent  {before['agent']:.7f} USDC")

    step("1.", f"lock {amount_usdc} USDC in escrow, signed by the client")
    # Deadline far enough out that the sweeper cannot interfere mid-run.
    deadline = int(time.time()) + 3600
    built = build_escrow_tx(
        client_public=client_kp.public_key,
        agent_public=agent_kp.public_key,
        amount_stroops=amount_stroops,
        deadline_unix=deadline,
        task_id=task_id,
    )
    te = TransactionEnvelope.from_xdr(built["unsigned_xdr"], NETWORK_PASSPHRASE)
    te.sign(client_kp)
    resp = Server(HORIZON_URL).submit_transaction(te)
    escrow_tx = resp["hash"]
    ok(f"escrow_tx {escrow_tx}")
    print(f"       {expl(escrow_tx)}")
    record["escrow_tx"] = escrow_tx

    chain = get_escrow_status(task_id) or {}
    if chain.get("escrow_status") == "Created":
        ok(f"the contract holds {chain.get('amount')} USDC for this task")
    else:
        bad(f"the contract does not report a live entry: {chain}")
    record["escrow_amount"] = chain.get("amount")

    step("2.", "build the evidence package and decide the verdict")
    output = GOOD_OUTPUT if should_pass else BAD_OUTPUT
    package = ev.build_evidence_package(
        task_id=task_id,
        agent_handle="competitive-intel",
        brief=BRIEF,
        output=output,
        called_tools=["web_search", "summarise"],
    )
    verdict, checks = ev.verify_delivery(package)
    ev_hash = ev.evidence_hash(package)
    print(f"       evidence hash {ev_hash.hex()}")
    for c in checks:
        mark = "ok  " if c["passed"] else "FAIL"
        print(f"       {mark} {c['name']}: {c['detail']}")
    if verdict == should_pass:
        ok(f"verdict: {'passed' if verdict else 'failed'}, as this path intends")
    else:
        bad(f"verdict is {verdict}, expected {should_pass}")
    record["evidence_hash"] = ev_hash.hex()
    record["verdict"] = "passed" if verdict else "failed"
    record["checks"] = checks
    record["package"] = package

    step("3.", "record the verdict on Stellar")
    att = ev.attest(orch_kp, task_id, agent_kp.public_key, ev_hash, verdict)
    if att["status"] == "SUCCESS":
        ok(f"attest_tx {att['hash']}")
        print(f"       {expl(att['hash'])}")
    else:
        bad(f"attestation: {att}")
    record["attest_tx"] = att.get("hash")

    step("4.", "ask the chain whether settling is justified")
    allowed, reason = ev.settlement_allowed(task_id, package)
    print(f"       allowed = {allowed}")
    print(f"       {reason}")
    if allowed == should_pass:
        ok("the gate agrees with the verdict on chain")
    else:
        bad(f"the gate says {allowed}, the verdict says {should_pass}")
    record["gate_allowed"] = allowed
    record["gate_reason"] = reason

    if allowed:
        step("5.", "settle: the agent is paid")
        r = settle_escrow(orch_kp, task_id, amount_stroops)
        if r["status"] == "SUCCESS":
            ok(f"settle_tx {r['hash']}")
            print(f"       {expl(r['hash'])}")
        else:
            bad(f"settle: {r}")
        record["settle_tx"] = r.get("hash")
    else:
        step("5.", "refund: the client gets their money back")
        # `settle(task_id, 0)` rather than `refund_to_client`, and the
        # difference matters. `refund_to_client` is the deadline path — it
        # panics with "deadline not reached" and exists for the case where
        # nobody delivered at all; that is what the sweeper uses.
        #
        # Here somebody did deliver and the work was checked and failed. There
        # is nothing to wait for: the verdict is already on the ledger. So the
        # orchestrator settles zero to the agent, which the contract pays out
        # as the full amount back to the client, atomically, in one
        # transaction. Making the client wait an hour for money that was
        # already judged theirs would be a worse product, not a safer one.
        r = settle_escrow(orch_kp, task_id, 0)
        if r["status"] == "SUCCESS":
            ok(f"refund_tx {r['hash']}  (settle with 0 to the agent)")
            print(f"       {expl(r['hash'])}")
        else:
            bad(f"refund: {r}")
        record["refund_tx"] = r.get("hash")
        record["refund_method"] = "settle(agent_amount=0)"

    step("6.", "balances after, read from Horizon")
    time.sleep(2)
    after = {
        "client": usdc_balance(client_kp.public_key, issuer),
        "agent": usdc_balance(agent_kp.public_key, issuer),
    }
    d_client = after["client"] - before["client"]
    d_agent = after["agent"] - before["agent"]
    print(f"       client {after['client']:.7f} USDC  ({d_client:+.7f})")
    print(f"       agent  {after['agent']:.7f} USDC  ({d_agent:+.7f})")
    record["delta_client"] = round(d_client, 7)
    record["delta_agent"] = round(d_agent, 7)

    amt = float(amount_usdc)
    if should_pass:
        if abs(d_agent - amt) < 1e-6 and abs(d_client + amt) < 1e-6:
            ok(f"the agent gained {amt} USDC and the client paid it. "
               f"The work passed, so the money moved.")
        else:
            bad(f"expected agent +{amt} / client -{amt}")
    else:
        if abs(d_client) < 1e-6 and abs(d_agent) < 1e-6:
            ok(f"nobody gained or lost anything. The work failed, so the "
               f"{amt} USDC went back to the client.")
        else:
            bad(f"expected both deltas 0, got client {d_client} agent {d_agent}")

    step("7.", "and the verifier answers the question a client would ask")
    assert ev.matches_evidence(task_id, ev_hash), "the chain lost the hash"
    ok("the deliverable they hold is the one that was checked")
    import hashlib
    tampered = hashlib.sha256((output + " (edited later)").encode()).digest()
    assert not ev.matches_evidence(task_id, tampered)
    ok("an edited deliverable is not")

    return record


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--pass", dest="only_pass", action="store_true")
    ap.add_argument("--fail", dest="only_fail", action="store_true")
    ap.add_argument("--amount", default="0.05")
    ap.add_argument("--archive", type=Path,
                    help="write the run to this JSON file")
    args = ap.parse_args()

    if not os.environ.get("EVIDENCE_CONTRACT"):
        print("ABORT: EVIDENCE_CONTRACT is not set. The gate has nothing to "
              "read, so nothing would settle.", file=sys.stderr)
        return 2

    records = []
    try:
        if not args.only_fail:
            records.append(run_cycle(True, args.amount))
        if not args.only_pass:
            records.append(run_cycle(False, args.amount))
    except Exception as e:
        print(f"\n\033[31mABORT\033[0m {type(e).__name__}: {e}", file=sys.stderr)
        return 1

    if args.archive:
        import json

        args.archive.parent.mkdir(parents=True, exist_ok=True)
        args.archive.write_text(
            json.dumps(
                {
                    "network": "testnet",
                    "evidence_contract": os.environ["EVIDENCE_CONTRACT"],
                    "escrow_contract": os.environ.get("X402_ESCROW_CONTRACT"),
                    "note": "Testnet resets on 2026-12-16 and takes contract "
                            "state with it. Hashes here stay checkable against "
                            "the packages below without the chain.",
                    "cycles": records,
                },
                indent=2,
                ensure_ascii=False,
            ),
            encoding="utf8",
        )
        print(f"\narchived to {args.archive}")

    print(f"\n\033[1m{len(records)} cycle(s) completed on testnet\033[0m")
    return 0


if __name__ == "__main__":
    sys.exit(main())
