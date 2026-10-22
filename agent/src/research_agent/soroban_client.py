from __future__ import annotations

import threading
import time as _time

from stellar_sdk import Account, Keypair, SorobanServer, TransactionBuilder, scval
from stellar_sdk import xdr as stellar_xdr

from .stellar_config import (
    COMPLIANCE_CONTRACT_ID as _COMPLIANCE_CONTRACT_ID,
    NETWORK_PASSPHRASE,
    RPC_URL,
    TOOL_REGISTRY_CONTRACT_ID as _TOOL_REGISTRY_CONTRACT_ID,
    get_soroban_server,
    require_contract_id,
)

STROOPS_PER_XLM = 10_000_000

_registry_cache: dict = {"data": None, "expires": 0.0}
_registry_cache_lock = threading.Lock()
_CACHE_TTL = 60  # seconds


def _get_config() -> tuple[str, str]:
    return RPC_URL, require_contract_id(
        "TOOL_REGISTRY_CONTRACT_ID", _TOOL_REGISTRY_CONTRACT_ID
    )


def _simulate_invoke(
    rpc_url: str,
    contract_id: str,
    fn_name: str,
    args: list[stellar_xdr.SCVal] | None = None,
) -> stellar_xdr.SCVal:
    """Build and simulate a contract invocation (read-only, no signing)."""
    server = get_soroban_server(rpc_url)

    source_kp = Keypair.random()
    source_account = Account(source_kp.public_key, 0)

    builder = TransactionBuilder(
        source_account=source_account,
        network_passphrase=NETWORK_PASSPHRASE,
        base_fee=100,
    )
    builder.append_invoke_contract_function_op(
        contract_id=contract_id,
        function_name=fn_name,
        parameters=args or [],
    )
    builder.set_timeout(30)
    tx = builder.build()

    sim = server.simulate_transaction(tx)
    if sim.error:
        raise RuntimeError(f"Soroban simulate error: {sim.error}")

    return sim.results[0].xdr  # type: ignore[union-attr]


def _to_str(val) -> str:
    """Convert a value to string, handling stellar Address objects."""
    from stellar_sdk import Address

    if isinstance(val, Address):
        return val.address
    return str(val)


def _parse_tool_entry(val: stellar_xdr.SCVal) -> dict:
    """Convert a ToolEntry ScVal (struct/map) to a plain dict."""
    native = scval.to_native(val)
    if isinstance(native, dict):
        return {
            "agent": _to_str(native.get("agent", "")),
            "handle": _to_str(native.get("handle", "")),
            "url": _to_str(native.get("url", "")),
            "capabilities": [_to_str(c) for c in native.get("capabilities", [])],
            "model_tier": _to_str(native.get("model_tier", "")),
            "rate_stroops": int(native.get("rate_stroops", 0)),
            "max_tokens": int(native.get("max_tokens", 0)),
            "active": bool(native.get("active", False)),
            "regis": int(native.get("regis", 0)),
        }
    raise ValueError(f"Unexpected ScVal type for ToolEntry: {type(native)}")


def invalidate_registry_cache():
    """Invalidate the registry cache (call after register/deactivate)."""
    with _registry_cache_lock:
        _registry_cache["data"] = None
        _registry_cache["expires"] = 0.0


def list_active_full(
    rpc_url: str | None = None,
    contract_id: str | None = None,
) -> list[dict]:
    """List all active tools with full details from the Soroban registry.

    Results are cached for 60 seconds to avoid repeated RPC simulations.
    """
    now = _time.monotonic()
    with _registry_cache_lock:
        if _registry_cache["data"] is not None and now < _registry_cache["expires"]:
            return _registry_cache["data"]

    _rpc, _cid = _get_config()
    rpc_url = rpc_url or _rpc
    contract_id = contract_id or _cid

    result_xdr = _simulate_invoke(rpc_url, contract_id, "list_active_full")
    native = scval.to_native(result_xdr)

    if isinstance(native, list):
        entries = []
        for item in native:
            if isinstance(item, dict):
                entries.append(
                    {
                        "agent": _to_str(item.get("agent", "")),
                        "handle": _to_str(item.get("handle", "")),
                        "url": _to_str(item.get("url", "")),
                        "capabilities": [
                            _to_str(c) for c in item.get("capabilities", [])
                        ],
                        "model_tier": str(item.get("model_tier", "")),
                        "rate_stroops": int(item.get("rate_stroops", 0)),
                        "max_tokens": int(item.get("max_tokens", 0)),
                        "active": bool(item.get("active", False)),
                        "regis": int(item.get("regis", 0)),
                    }
                )
        with _registry_cache_lock:
            _registry_cache["data"] = entries
            _registry_cache["expires"] = _time.monotonic() + _CACHE_TTL
        return entries

    raise ValueError(f"Unexpected result type: {type(native)}")


def get_tool(
    handle: str,
    rpc_url: str | None = None,
    contract_id: str | None = None,
) -> dict:
    """Get a single tool entry by handle."""
    _rpc, _cid = _get_config()
    rpc_url = rpc_url or _rpc
    contract_id = contract_id or _cid

    args = [scval.to_string(handle)]
    result_xdr = _simulate_invoke(rpc_url, contract_id, "get_tool", args)
    return _parse_tool_entry(result_xdr)


def get_rep(
    agent_address: str,
    rpc_url: str | None = None,
    contract_id: str | None = None,
) -> int:
    """Get REP token balance for an agent address."""
    _rpc, _cid = _get_config()
    rpc_url = rpc_url or _rpc
    contract_id = contract_id or _cid

    args = [scval.to_address(agent_address)]
    result_xdr = _simulate_invoke(rpc_url, contract_id, "get_rep", args)
    return int(scval.to_native(result_xdr))


def stroops_to_xlm(stroops: int) -> float:
    return stroops / STROOPS_PER_XLM


def build_deactivate_tx(
    agent_public: str,
    handle: str,
    rpc_url: str | None = None,
    contract_id: str | None = None,
) -> dict:
    """Build an unsigned deactivate_tool transaction for the agent to sign.

    Returns dict with: unsigned_xdr, handle.
    The agent signs this with their wallet (Freighter), then submits via submit-deactivate.
    """
    _rpc, _cid = _get_config()
    rpc_url = rpc_url or _rpc
    contract_id = contract_id or _cid

    server = get_soroban_server(rpc_url)
    source_account = server.load_account(agent_public)

    builder = TransactionBuilder(
        source_account=source_account,
        network_passphrase=NETWORK_PASSPHRASE,
        base_fee=100,
    )
    builder.append_invoke_contract_function_op(
        contract_id=contract_id,
        function_name="deactivate_tool",
        parameters=[
            scval.to_address(agent_public),
            scval.to_string(handle),
        ],
    )
    builder.set_timeout(30)
    tx = builder.build()

    sim = server.simulate_transaction(tx)
    if sim.error:
        raise RuntimeError(f"Simulate error: {sim.error}")

    tx = server.prepare_transaction(tx, sim)

    return {
        "unsigned_xdr": tx.to_xdr(),
        "handle": handle,
    }


def submit_register_tool(
    agent_kp,
    handle: str,
    url: str,
    capabilities: list[str],
    model_tier: str,
    rate_stroops: int,
    max_tokens: int,
    rpc_url: str | None = None,
    contract_id: str | None = None,
) -> str:
    """Call register_tool() on the tool registry contract (signed, on-chain)."""
    _rpc, _cid = _get_config()
    rpc_url = rpc_url or _rpc
    contract_id = contract_id or _cid

    server = get_soroban_server(rpc_url)
    source_account = server.load_account(agent_kp.public_key)

    cap_scvals = [scval.to_string(c) for c in capabilities]

    builder = TransactionBuilder(
        source_account=source_account,
        network_passphrase=NETWORK_PASSPHRASE,
        base_fee=100,
    )
    builder.append_invoke_contract_function_op(
        contract_id=contract_id,
        function_name="register_tool",
        parameters=[
            scval.to_address(agent_kp.public_key),
            scval.to_string(handle),
            scval.to_string(url),
            scval.to_vec(cap_scvals),
            scval.to_string(model_tier),
            scval.to_int128(rate_stroops),
            scval.to_uint32(max_tokens),
        ],
    )
    builder.set_timeout(30)
    tx = builder.build()

    sim = server.simulate_transaction(tx)
    if sim.error:
        raise RuntimeError(f"Simulate error: {sim.error}")

    tx = server.prepare_transaction(tx, sim)
    tx.sign(agent_kp)

    resp = server.send_transaction(tx)
    return resp.hash


def submit_update_tool(
    agent_kp,
    handle: str,
    url: str,
    capabilities: list[str],
    model_tier: str,
    rate_stroops: int,
    max_tokens: int,
    rpc_url: str | None = None,
    contract_id: str | None = None,
) -> str:
    """Call update_tool() on the tool registry contract (signed, on-chain).

    Updates mutable fields on an existing entry; only the original owner
    agent can update its own row (`entry.agent == agent` enforced on-chain).
    Identity fields (`agent`, `handle`, `regis`) and `active` are not touched use `submit_deactivate_tool` to flip `active`.
    """
    _rpc, _cid = _get_config()
    rpc_url = rpc_url or _rpc
    contract_id = contract_id or _cid

    server = get_soroban_server(rpc_url)
    source_account = server.load_account(agent_kp.public_key)

    cap_scvals = [scval.to_string(c) for c in capabilities]

    builder = TransactionBuilder(
        source_account=source_account,
        network_passphrase=NETWORK_PASSPHRASE,
        base_fee=100,
    )
    builder.append_invoke_contract_function_op(
        contract_id=contract_id,
        function_name="update_tool",
        parameters=[
            scval.to_address(agent_kp.public_key),
            scval.to_string(handle),
            scval.to_string(url),
            scval.to_vec(cap_scvals),
            scval.to_string(model_tier),
            scval.to_int128(rate_stroops),
            scval.to_uint32(max_tokens),
        ],
    )
    builder.set_timeout(30)
    tx = builder.build()

    sim = server.simulate_transaction(tx)
    if sim.error:
        raise RuntimeError(f"Simulate error: {sim.error}")

    tx = server.prepare_transaction(tx, sim)
    tx.sign(agent_kp)

    resp = server.send_transaction(tx)
    return resp.hash


def submit_claim_task(
    requester_kp,
    task_id: str,
    handle: str,
    deadline: int,
    rpc_url: str | None = None,
    contract_id: str | None = None,
) -> str:
    """Call claim_task() on the tool registry contract (signed, on-chain)."""
    _rpc, _cid = _get_config()
    rpc_url = rpc_url or _rpc
    contract_id = contract_id or _cid

    server = get_soroban_server(rpc_url)
    source_account = server.load_account(requester_kp.public_key)

    builder = TransactionBuilder(
        source_account=source_account,
        network_passphrase=NETWORK_PASSPHRASE,
        base_fee=100,
    )
    builder.append_invoke_contract_function_op(
        contract_id=contract_id,
        function_name="claim_task",
        parameters=[
            scval.to_address(requester_kp.public_key),
            scval.to_string(task_id),
            scval.to_string(handle),
            scval.to_uint64(deadline),
        ],
    )
    builder.set_timeout(30)
    tx = builder.build()

    sim = server.simulate_transaction(tx)
    if sim.error:
        raise RuntimeError(f"Simulate error: {sim.error}")

    tx = server.prepare_transaction(tx, sim)
    tx.sign(requester_kp)

    resp = server.send_transaction(tx)
    return resp.hash


def verify_compression(
    task_id: str,
    input_tokens: int,
    output_tokens: int,
    max_ratio_pct: int,
    rpc_url: str | None = None,
    contract_id: str | None = None,
) -> bool:
    """Call the compression compliance contract (read-only simulate)."""
    rpc_url = rpc_url or RPC_URL
    contract_id = contract_id or require_contract_id(
        "COMPLIANCE_CONTRACT_ID", _COMPLIANCE_CONTRACT_ID
    )

    args = [
        scval.to_string(task_id),
        scval.to_uint32(input_tokens),
        scval.to_uint32(output_tokens),
        scval.to_uint32(max_ratio_pct),
    ]
    result_xdr = _simulate_invoke(rpc_url, contract_id, "verify_compression", args)
    return bool(scval.to_native(result_xdr))
