#!/usr/bin/env python3
"""Snapshot the on-chain evidence this project relies on, into the repository.

Why this exists
---------------
Two separate things destroy evidence that lives only as explorer links:

1. **Testnet resets.** Stellar wipes testnet 2–4 times a year — all ledger
   entries, smart contract data included. The next published date is
   **16 December 2026**. Every stellar.expert link in a report dies that day.
2. **Contract migration.** When a contract is redeployed to change its
   behaviour, the new deployment gets a new ID and starts at zero invocations.
   The history of the old one stays on the ledger but stops being *this
   project's* history in any obvious way.

Both have already been decided for or scheduled. So the record of what ran,
how often, and in which transactions is captured here — in the repository,
versioned, readable without a network.

What it captures
----------------
* Per contract: creation time, creator, wasm hash, invocation and event counts.
* Per known transaction: ledger, timestamp, success, fee, and the signed
  envelope XDR — the raw bytes, from which anyone can reconstruct exactly what
  was submitted.
* A manifest saying when the snapshot was taken and why.

What it does NOT do
-------------------
It does not prove the transactions were honest; it preserves them so somebody
else can check. That is the whole point: the evidence survives the reset so the
claim stays falsifiable.

Usage
-----
    python3 scripts/archive-onchain-evidence.py
    python3 scripts/archive-onchain-evidence.py --reason "migrating usdc-escrow"
"""

from __future__ import annotations

import argparse
import datetime as dt
import json
import os
import sys
import urllib.error
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
TIMEOUT = 25

NETWORKS = {
    "testnet": {
        "horizon": "https://horizon-testnet.stellar.org",
        "expert": "https://api.stellar.expert/explorer/testnet",
        "explorer": "https://stellar.expert/explorer/testnet",
    },
    "public": {
        "horizon": "https://horizon.stellar.org",
        "expert": "https://api.stellar.expert/explorer/public",
        "explorer": "https://stellar.expert/explorer/public",
    },
}


# The project depends on truststore so that Python uses the operating system's
# certificate store. Without it, a stock interpreter has no CA bundle and every
# HTTPS call here fails with CERTIFICATE_VERIFY_FAILED — which looks exactly
# like "the network is down" unless the reason is reported.
try:
    import truststore

    truststore.inject_into_ssl()
    _TLS = "system trust store (truststore)"
except ImportError:  # pragma: no cover - depends on how the script is invoked
    _TLS = "interpreter default (truststore not importable)"


def fetch(url: str) -> tuple[dict | None, str | None]:
    """GET a JSON document.

    Returns `(data, None)` on success and `(None, reason)` on failure. The
    reason is carried rather than swallowed: a snapshot that records
    "UNREACHABLE" without saying whether that was a 404, a timeout or a local
    TLS misconfiguration is a snapshot nobody can act on.
    """
    try:
        req = urllib.request.Request(url, headers={"User-Agent": "brujula-evidence-archive"})
        with urllib.request.urlopen(req, timeout=TIMEOUT) as r:
            return json.loads(r.read().decode("utf8")), None
    except urllib.error.HTTPError as exc:
        return None, f"HTTP {exc.code}"
    except urllib.error.URLError as exc:
        return None, f"URLError: {exc.reason}"
    except (TimeoutError, OSError) as exc:
        return None, f"{type(exc).__name__}: {exc}"
    except ValueError as exc:
        return None, f"malformed JSON: {exc}"


def load_contract_ids() -> dict[str, str]:
    """Read the contract IDs from the environment, falling back to .env.

    Reads the file rather than requiring it to be sourced, so the archive can
    be taken without arranging a shell first.
    """
    names = {
        "usdc-escrow": "X402_ESCROW_CONTRACT",
        "tool-registry": "TOOL_REGISTRY_CONTRACT_ID",
        "compression-compliance": "COMPLIANCE_CONTRACT_ID",
        "evidence-attestation": "EVIDENCE_CONTRACT_ID",
    }
    values = dict(os.environ)
    env_file = ROOT / ".env"
    if env_file.exists():
        for line in env_file.read_text(encoding="utf8").splitlines():
            line = line.strip()
            if not line or line.startswith("#") or "=" not in line:
                continue
            k, _, v = line.partition("=")
            values.setdefault(k.strip(), v.strip().strip('"').strip("'"))

    found = {}
    for label, var in names.items():
        cid = values.get(var, "").strip()
        if cid.startswith("C") and len(cid) == 56:
            found[label] = cid
    return found


def snapshot_contract(expert: str, explorer: str, label: str, cid: str) -> dict:
    data, reason = fetch(f"{expert}/contract/{cid}")
    data = data or {}
    created = data.get("created")
    return {
        "label": label,
        "contract_id": cid,
        "explorer_url": f"{explorer}/contract/{cid}",
        "reachable": bool(data),
        "unreachable_reason": reason,
        "created_unix": created,
        "created_utc": dt.datetime.fromtimestamp(created, dt.UTC).isoformat() if created else None,
        "creator": data.get("creator"),
        "wasm_hash": data.get("wasm"),
        "invocations": data.get("invocations"),
        "events": data.get("events"),
        "errors": data.get("errors"),
        "storage_entries": data.get("storage_entries"),
    }


def snapshot_transaction(horizon: str, explorer: str, tx_hash: str) -> dict:
    data, reason = fetch(f"{horizon}/transactions/{tx_hash}")
    data = data or {}
    return {
        "hash": tx_hash,
        "explorer_url": f"{explorer}/tx/{tx_hash}",
        "reachable": bool(data.get("hash")),
        "unreachable_reason": reason,
        "ledger": data.get("ledger"),
        "created_at": data.get("created_at"),
        "successful": data.get("successful"),
        "source_account": data.get("source_account"),
        "fee_charged": data.get("fee_charged"),
        "operation_count": data.get("operation_count"),
        # The signed bytes. Everything above can be recomputed from this.
        "envelope_xdr": data.get("envelope_xdr"),
        "result_xdr": data.get("result_xdr"),
    }


def known_transaction_hashes() -> list[str]:
    """Transaction hashes this project recorded, from the task store.

    Only well-formed hashes: a Stellar transaction hash is 64 hex characters,
    and rows holding anything else are test leftovers.
    """
    hashes: list[str] = []
    try:
        sys.path.insert(0, str(ROOT / "agent" / "src"))
        from research_agent.task_store import TaskStore  # noqa: PLC0415

        store = TaskStore()
        for row in store.list_with_escrow(limit=500):
            for field in ("escrow_tx", "refund_tx_hash"):
                h = (row.get(field) or "").strip()
                if len(h) == 64 and all(c in "0123456789abcdefABCDEF" for c in h):
                    hashes.append(h)
    except Exception as exc:  # noqa: BLE001
        print(f"  ! task store unavailable ({type(exc).__name__}); "
              f"archiving contract metadata only", file=sys.stderr)
    return sorted(set(hashes))


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--network", default=os.environ.get("STELLAR_NETWORK", "testnet"),
                    choices=sorted(NETWORKS))
    ap.add_argument("--reason", default="periodic snapshot",
                    help="why this snapshot was taken; goes in the manifest")
    ap.add_argument("--out", default=None, help="output directory")
    args = ap.parse_args()

    net = NETWORKS[args.network]
    taken_at = dt.datetime.now(dt.UTC)
    stamp = taken_at.strftime("%Y-%m-%d")

    out = Path(args.out) if args.out else ROOT / "evidence" / args.network / stamp
    out.mkdir(parents=True, exist_ok=True)

    print(f"Archiving {args.network} evidence to {out.relative_to(ROOT)}")

    contracts = load_contract_ids()
    if not contracts:
        print("  ! no contract IDs found in environment or .env", file=sys.stderr)

    contract_snaps = []
    for label, cid in sorted(contracts.items()):
        snap = snapshot_contract(net["expert"], net["explorer"], label, cid)
        state = (f"{snap['invocations']} invocations" if snap["reachable"]
                 else f"UNREACHABLE — {snap['unreachable_reason']}")
        print(f"  {label:26} {state}")
        contract_snaps.append(snap)

    hashes = known_transaction_hashes()
    print(f"  {len(hashes)} known transaction hash(es)")
    tx_snaps = [snapshot_transaction(net["horizon"], net["explorer"], h) for h in hashes]
    live = sum(1 for t in tx_snaps if t["reachable"])
    if hashes:
        print(f"  {live}/{len(tx_snaps)} still readable on the network")

    (out / "contracts.json").write_text(
        json.dumps(contract_snaps, indent=2, ensure_ascii=False) + "\n", encoding="utf8")
    (out / "transactions.json").write_text(
        json.dumps(tx_snaps, indent=2, ensure_ascii=False) + "\n", encoding="utf8")

    total_invocations = sum(
        c["invocations"] for c in contract_snaps if isinstance(c["invocations"], int))

    manifest = {
        "taken_at_utc": taken_at.isoformat(),
        "network": args.network,
        "reason": args.reason,
        "tls": _TLS,
        "contracts_archived": len(contract_snaps),
        "total_invocations": total_invocations,
        "transactions_archived": len(tx_snaps),
        "transactions_still_readable": live,
        "note": (
            "Snapshot of on-chain state at the time shown. Kept because testnet "
            "resets delete contract data (next published date: 2026-12-16) and "
            "because redeploying a contract moves its history to a new ID. The "
            "envelope_xdr of each transaction is the signed bytes as submitted; "
            "everything else here can be recomputed from it."
        ),
        "how_to_verify": [
            "Each contract and transaction carries its explorer_url. While the "
            "network still holds the data, open it and compare.",
            "After a reset the links 404 by design. The envelope_xdr remains "
            "decodable offline with: stellar xdr decode --type TransactionEnvelope",
        ],
    }
    (out / "MANIFEST.json").write_text(
        json.dumps(manifest, indent=2, ensure_ascii=False) + "\n", encoding="utf8")

    print(f"\n  {total_invocations} total invocations across {len(contract_snaps)} contracts")
    print(f"  written: contracts.json, transactions.json, MANIFEST.json")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
