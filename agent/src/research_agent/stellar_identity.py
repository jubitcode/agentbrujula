from __future__ import annotations

import base64
import os
import time

import httpx
from stellar_sdk import (
    Asset,
    Keypair,
    Server,
    TransactionBuilder,
)

from .stellar_config import NETWORK_PASSPHRASE, HORIZON_URL, FRIENDBOT_URL

HORIZON_TESTNET = HORIZON_URL
REP_ASSET_CONTRACT = os.environ.get(
    "REP_ASSET_CONTRACT",
    "CDJCWXFHMO3QSZJNP6ZPQKOB4Z6EC6R5SBOOBJ5ULIWPW4FYD4N2KTVY",
)
REP_ASSET_ISSUER = os.environ.get(
    "REP_ASSET_ISSUER",
    "GDABMVSW6QMLZAPOUUW4ZSXN4WLPHAHXFDMTG3FDKMBVXHH6FDHJZ7ON",
)


def generate_agent_keypair() -> Keypair:
    """Generate a new random Stellar keypair."""
    return Keypair.random()


def fund_testnet(public_key: str) -> bool:
    """Fund an account on testnet via Friendbot.

    Returns False on mainnet (no friendbot exists) so callers can surface a
    clear "fund externally" message instead of crashing on a None URL.
    """
    if not FRIENDBOT_URL:
        return False
    resp = httpx.get(
        FRIENDBOT_URL,
        params={"addr": public_key},
        timeout=30,
    )
    return resp.status_code == 200


def set_capability_manifest(
    keypair: Keypair,
    capabilities: list[str],
    model_tier: str,
    rate_stroops: int,
) -> str:
    """Publish agent capabilities as manage_data entries on the account."""
    server = Server(HORIZON_URL)
    account = server.load_account(keypair.public_key)

    builder = TransactionBuilder(
        source_account=account,
        network_passphrase=NETWORK_PASSPHRASE,
        base_fee=100,
    )

    for cap in capabilities:
        builder.append_manage_data_op(
            data_name=f"cap:{cap}",
            data_value="1",
        )

    builder.append_manage_data_op(data_name="model_tier", data_value=model_tier)
    builder.append_manage_data_op(
        data_name="rate_stroops", data_value=str(rate_stroops)
    )

    builder.set_timeout(30)
    tx = builder.build()
    tx.sign(keypair)

    resp = server.submit_transaction(tx)
    return resp["hash"]


def establish_rep_trustline(
    keypair: Keypair, rep_asset_issuer: str | None = None
) -> str:
    """Create a trustline to the REP token asset."""
    issuer = rep_asset_issuer or REP_ASSET_ISSUER
    server = Server(HORIZON_URL)
    account = server.load_account(keypair.public_key)

    rep_asset = Asset("REP", issuer)

    builder = TransactionBuilder(
        source_account=account,
        network_passphrase=NETWORK_PASSPHRASE,
        base_fee=100,
    )
    builder.append_change_trust_op(asset=rep_asset)
    builder.set_timeout(30)
    tx = builder.build()
    tx.sign(keypair)

    resp = server.submit_transaction(tx)
    return resp["hash"]


def read_capability_manifest(public_key: str) -> dict:
    """Read manage_data entries from Horizon and parse the capability manifest."""
    server = Server(HORIZON_URL)
    account = server.accounts().account_id(public_key).call()
    data = account.get("data", {})

    capabilities = []
    model_tier = None
    rate_stroops = None

    for key, b64_value in data.items():
        value = base64.b64decode(b64_value).decode()
        if key.startswith("cap:"):
            capabilities.append(key[4:])  # strip "cap:" prefix
        elif key == "model_tier":
            model_tier = value
        elif key == "rate_stroops":
            rate_stroops = int(value)

    return {
        "capabilities": capabilities,
        "model_tier": model_tier,
        "rate_stroops": rate_stroops,
    }


def onboard_agent(
    capabilities: list[str],
    model_tier: str,
    rate_stroops: int,
) -> dict:
    """Full onboarding: generate keypair → fund → manage_data → trustline."""
    kp = generate_agent_keypair()

    if not FRIENDBOT_URL:
        raise RuntimeError(
            f"Cannot auto-fund {kp.public_key}: friendbot unavailable on this network. "
            "Fund the account externally before completing onboarding."
        )
    if not fund_testnet(kp.public_key):
        raise RuntimeError(f"Failed to fund {kp.public_key}")

    time.sleep(2)

    manifest_tx = set_capability_manifest(kp, capabilities, model_tier, rate_stroops)
    trustline_tx = establish_rep_trustline(kp)

    return {
        "keypair": kp,
        "manifest_tx": manifest_tx,
        "trustline_tx": trustline_tx,
    }
