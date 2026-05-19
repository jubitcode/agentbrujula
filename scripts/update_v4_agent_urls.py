"""Bulk-update the `url` field on every AgentPod registered in tool-registry.

Reads `scripts/v4_agent_keys.json` (the secrets file produced by
`migrate_v4_agents.py`), then calls `submit_update_tool` on each entry
with the new URL. Other mutable fields (capabilities / tier / rate /
max_tokens) are preserved from the JSON — they are NOT changed.

Idempotent: agents whose current URL in the JSON already matches the
target are skipped (saves a tx fee).

Run from `agent_canonical/`:

    cd agent
    uv run python ../scripts/update_v4_agent_urls.py --url https://mcp-meta.example.com
    uv run python ../scripts/update_v4_agent_urls.py --url https://mcp-meta.example.com --dry-run
    uv run python ../scripts/update_v4_agent_urls.py --url https://mcp-meta.example.com --only adr-moderator

Requires:

* `ORCHESTRATOR_SECRET_KEY` in env (loads the same .env that the api
  container uses) — needed only by `submit_update_tool` for its RPC
  config; the agents sign their own update.
* `scripts/v4_agent_keys.json` present and readable (chmod 600).
* tool-registry — `update_tool` entrypoint is required.

After a successful run, this script writes the new URL back into
`v4_agent_keys.json` so future runs see the change. The `--dry-run`
flag prints the would-be calls without signing or writing.

Verification (operator step, after the script):

    stellar contract invoke \\
      --network testnet \\
      --id CARJRAXMYOM7TNNCR2K4OVZTO6BYUTUIW5AB2ZHDSF5UZXBSWU7YZQBW \\
      -- list_active_full

Every row.s `url` should match the new value. Record the bulk update in
the ops log (one row per script invocation).
"""

from __future__ import annotations

import argparse
import json
import sys
import time
from pathlib import Path

# Make `research_agent` importable when run from anywhere in the repo.
THIS_DIR = Path(__file__).resolve().parent
REPO_ROOT = THIS_DIR.parent
sys.path.insert(0, str(REPO_ROOT / "agent" / "src"))

from stellar_sdk import Keypair  # noqa: E402

from research_agent.soroban_client import submit_update_tool  # noqa: E402


KEYS_FILE = THIS_DIR / "v4_agent_keys.json"


def load_keys() -> dict[str, dict]:
    if not KEYS_FILE.exists():
        sys.exit(
            f"ERROR: {KEYS_FILE} not found. Did `scripts/migrate_v4_agents.py` "
            f"ever run? See the key-backup procedure for recovery."
        )
    return json.loads(KEYS_FILE.read_text())


def save_keys(keys: dict[str, dict]) -> None:
    """Atomic-ish write: write to tmp then rename, preserve 0600 perms."""
    tmp = KEYS_FILE.with_suffix(".json.tmp")
    tmp.write_text(json.dumps(keys, indent=2) + "\n")
    tmp.chmod(0o600)
    tmp.replace(KEYS_FILE)


def update_one(handle: str, entry: dict, new_url: str, dry_run: bool) -> tuple[bool, str]:
    """Return (changed, message). `changed` reflects on-chain change, not skip."""
    current_url = entry.get("url", "")
    if current_url == new_url:
        return False, f"SKIP   already at {new_url}"

    if dry_run:
        return True, f"WOULD  {current_url}  ->  {new_url}"

    kp = Keypair.from_secret(entry["secret_key"])
    try:
        tx_hash = submit_update_tool(
            kp,
            handle=handle,
            url=new_url,
            capabilities=entry["capabilities"],
            model_tier=entry["model_tier"],
            rate_stroops=int(entry["rate_stroops"]),
            max_tokens=int(entry["max_tokens"]),
        )
    except Exception as exc:
        return False, f"FAIL   {exc}"

    entry["url"] = new_url
    entry["url_updated_at"] = int(time.time())
    return True, f"OK     {tx_hash}"


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--url", required=True, help="New public URL for all agents")
    parser.add_argument("--dry-run", action="store_true", help="Print would-be changes, do not sign")
    parser.add_argument("--only", help="Restrict to one agent handle (e.g. --only adr-moderator)")
    args = parser.parse_args()

    new_url = args.url.rstrip("/")
    if not (new_url.startswith("http://") or new_url.startswith("https://")):
        sys.exit("ERROR: --url must start with http:// or https://")

    keys = load_keys()

    if args.only:
        if args.only not in keys:
            sys.exit(f"ERROR: --only {args.only!r} not in {KEYS_FILE.name}. "
                     f"Known handles: {sorted(keys)}")
        targets = {args.only: keys[args.only]}
    else:
        targets = keys

    print(f"keys file:  {KEYS_FILE}")
    print(f"target URL: {new_url}")
    print(f"mode:       {'DRY-RUN' if args.dry_run else 'LIVE'}")
    print(f"agents:     {len(targets)}")
    print()

    updated = 0
    skipped = 0
    failed = 0

    for handle, entry in targets.items():
        print(f"  {handle:24s}  ", end="")
        changed, message = update_one(handle, entry, new_url, args.dry_run)
        print(message)
        if message.startswith("FAIL"):
            failed += 1
        elif message.startswith("SKIP"):
            skipped += 1
        elif changed:
            updated += 1

    if updated and not args.dry_run:
        save_keys(keys)
        print()
        print(f"wrote new URLs to {KEYS_FILE}")

    print()
    print(f"summary: updated={updated} skipped={skipped} failed={failed}")

    if not args.dry_run and updated:
        print()
        print("NEXT STEPS:")
        print("  1. Verify on-chain: stellar contract invoke --network testnet \\")
        print("       --id CARJRAXMYOM7TNNCR2K4OVZTO6BYUTUIW5AB2ZHDSF5UZXBSWU7YZQBW \\")
        print("       -- list_active_full")
        print("  2. Record in the ops log (one row per run).")
        print("  3. Re-back up v4_agent_keys.json (url + url_updated_at changed).")

    return 0 if failed == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
