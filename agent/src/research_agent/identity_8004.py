"""Agent identity on the 8004 registry.

What this is for: an agent in this marketplace is a handle in our own
`tool-registry` contract, which says nothing to anybody outside it. The 8004
registry is a shared namespace — an agent registered there has an id and a
payment wallet that other systems can read without asking us, and without
trusting our database.

The property that matters is the wallet link. Before this, the address an
agent gets paid at came from our own records. Now it is read from the registry
on chain, so a client can check who the money went to against a source neither
side controls.

**What 8004 is, accurately.** It is not a Stellar primitive. It began as
ERC-8004 on Ethereum, and the Soroban implementation this module talks to is
`trionlabs/stellar-8004` — MIT licensed, deployed on testnet and mainnet, and
a third-party project with no SDF endorsement. Measured on 2026-10-21: the
testnet registry has 154 invocations and was created in April 2026. Saying
"Stellar has an 8004 identity registry" would be wrong, and the SOW needs to
say "we adopt the emerging agent-identity standard on the existing Soroban
implementation" instead.
"""

from __future__ import annotations

import os
from typing import Any

from stellar_sdk import Account, Keypair, TransactionBuilder, scval
from stellar_sdk import xdr as stellar_xdr

from .stellar_config import NETWORK_PASSPHRASE, RPC_URL, get_soroban_server

ENV_REGISTRY = "IDENTITY_8004_CONTRACT"

#: trionlabs/stellar-8004 Identity Registry on testnet. A default rather than a
#: hardcode: a deployment pointing somewhere else sets the env var, and nothing
#: here assumes this particular instance.
DEFAULT_TESTNET_REGISTRY = (
    "CDE3K4COIAGWNNJQQLL26SYI3KBJF5FUDHXG5FA6GYDJCG7T5V7FIWZH"
)


class IdentityNotConfigured(RuntimeError):
    pass


def registry_contract(contract_id: str | None = None) -> str:
    cid = (contract_id or os.environ.get(ENV_REGISTRY, "")).strip()
    if cid:
        return cid
    if "Test SDF" in (NETWORK_PASSPHRASE or ""):
        return DEFAULT_TESTNET_REGISTRY
    raise IdentityNotConfigured(
        f"{ENV_REGISTRY} is not set and there is no default outside testnet. "
        f"The mainnet registry is a different contract and pointing at the "
        f"wrong one would link agents to identities nobody can resolve."
    )


def _invoke(
    kp: Keypair | None,
    fn_name: str,
    params: list,
    contract_id: str | None = None,
    rpc_url: str | None = None,
    read_only: bool = False,
) -> Any:
    """Simulate (read) or submit (write) a call to the registry.

    Reads go through simulation only: they cost nothing and need no signer, so
    anybody can resolve an agent's wallet without an account.
    """
    cid = registry_contract(contract_id)
    server = get_soroban_server(rpc_url or RPC_URL)

    if read_only:
        # A source account is required to build a transaction, but simulation
        # never signs and never submits, so an unfunded random key is fine and
        # avoids making reads depend on a key they do not need.
        source: Any = Account(Keypair.random().public_key, 0)
    else:
        if kp is None:
            raise ValueError(f"{fn_name} writes to the registry and needs a keypair")
        source = server.load_account(kp.public_key)

    builder = TransactionBuilder(
        source_account=source,
        network_passphrase=NETWORK_PASSPHRASE,
        base_fee=1000,
    )
    builder.append_invoke_contract_function_op(
        contract_id=cid, function_name=fn_name, parameters=params
    )
    builder.set_timeout(60)
    tx = builder.build()

    sim = server.simulate_transaction(tx)
    if sim.error:
        raise RuntimeError(f"8004 {fn_name} simulate error: {sim.error}")
    if read_only:
        if not sim.results:
            return None
        return stellar_xdr.SCVal.from_xdr(sim.results[0].xdr)

    tx = server.prepare_transaction(tx, sim)
    tx.sign(kp)
    resp = server.send_transaction(tx)

    from .escrow import _wait_for_tx_confirmation

    confirmation = _wait_for_tx_confirmation(server, resp.hash)
    return {
        "hash": resp.hash,
        "status": confirmation["status"],
        "error": confirmation["error"],
        "result": stellar_xdr.SCVal.from_xdr(sim.results[0].xdr)
        if sim.results
        else None,
    }


# ---------------------------------------------------------------------------
# Writing
# ---------------------------------------------------------------------------

def register_agent(
    kp: Keypair,
    agent_uri: str,
    contract_id: str | None = None,
    rpc_url: str | None = None,
) -> dict:
    """Register `kp`'s account as an agent, with a URI describing it.

    Returns `{"hash", "status", "agent_id"}`. The id is the registry's, not
    ours, and it is what other systems use to refer to this agent.
    """
    out = _invoke(
        kp,
        "register_with_uri",
        [scval.to_address(kp.public_key), scval.to_string(agent_uri)],
        contract_id,
        rpc_url,
    )
    agent_id = None
    if out.get("result") is not None:
        try:
            agent_id = int(scval.from_uint32(out["result"]))
        except Exception:
            agent_id = None
    out["agent_id"] = agent_id
    return out


def set_agent_wallet(
    kp: Keypair,
    agent_id: int,
    wallet: str,
    contract_id: str | None = None,
    rpc_url: str | None = None,
) -> dict:
    """Point the agent's identity at the address that should receive payment.

    Separate from the identity on purpose: an agent can rotate the account it
    is paid at without losing its id, its URI or anything built on top of them.
    """
    return _invoke(
        kp,
        "set_agent_wallet",
        [
            scval.to_address(kp.public_key),
            scval.to_uint32(int(agent_id)),
            scval.to_address(wallet),
        ],
        contract_id,
        rpc_url,
    )


# ---------------------------------------------------------------------------
# Reading
# ---------------------------------------------------------------------------

def get_agent_wallet(
    agent_id: int,
    contract_id: str | None = None,
    rpc_url: str | None = None,
) -> str | None:
    """The payment address the registry holds for this agent, or None.

    This is the function the settle path should consult. Reading the address
    from the registry instead of from our own records is the whole point: a
    client can check where their money went against a source we do not own.
    """
    val = _invoke(
        None,
        "get_agent_wallet",
        [scval.to_uint32(int(agent_id))],
        contract_id,
        rpc_url,
        read_only=True,
    )
    if val is None or val.type == stellar_xdr.SCValType.SCV_VOID:
        return None
    try:
        return scval.from_address(val).address
    except Exception:
        return None


def agent_exists(
    agent_id: int,
    contract_id: str | None = None,
    rpc_url: str | None = None,
) -> bool:
    val = _invoke(
        None,
        "agent_exists",
        [scval.to_uint32(int(agent_id))],
        contract_id,
        rpc_url,
        read_only=True,
    )
    if val is None:
        return False
    try:
        return bool(scval.from_bool(val))
    except Exception:
        return False


def find_owner(
    agent_id: int,
    contract_id: str | None = None,
    rpc_url: str | None = None,
) -> str | None:
    val = _invoke(
        None,
        "find_owner",
        [scval.to_uint32(int(agent_id))],
        contract_id,
        rpc_url,
        read_only=True,
    )
    if val is None or val.type == stellar_xdr.SCValType.SCV_VOID:
        return None
    try:
        return scval.from_address(val).address
    except Exception:
        return None


def agent_uri(
    agent_id: int,
    contract_id: str | None = None,
    rpc_url: str | None = None,
) -> str | None:
    try:
        val = _invoke(
            None,
            "agent_uri",
            [scval.to_uint32(int(agent_id))],
            contract_id,
            rpc_url,
            read_only=True,
        )
    except RuntimeError:
        # The registry returns an error for an unregistered id rather than an
        # empty string. Not knowing a URI is an ordinary answer here.
        return None
    if val is None:
        return None
    try:
        return scval.from_string(val).decode("utf-8")
    except Exception:
        return None


def resolve_payment_address(
    agent_id: int | None,
    fallback: str,
    contract_id: str | None = None,
    rpc_url: str | None = None,
) -> tuple[str, str]:
    """The address to pay, and where it came from.

    Returns `(address, source)` where source is `"8004"` or `"local"`. The
    fallback exists because not every agent is registered yet, and refusing to
    pay a working agent because a third-party registry has no row for it would
    be the wrong trade. But the source is returned rather than hidden, so a
    caller can record which one it used — "we read this from the registry" and
    "we took our own word for it" are different claims.
    """
    if agent_id is None:
        return fallback, "local"
    try:
        linked = get_agent_wallet(agent_id, contract_id, rpc_url)
    except Exception:
        return fallback, "local"
    if not linked:
        return fallback, "local"
    return linked, "8004"
