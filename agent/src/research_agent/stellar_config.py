import os
from functools import lru_cache

from stellar_sdk import Network

STELLAR_NETWORK = os.getenv("STELLAR_NETWORK", "testnet").lower()

if STELLAR_NETWORK == "testnet":
    NETWORK_PASSPHRASE = Network.TESTNET_NETWORK_PASSPHRASE
    _DEFAULT_HORIZON = "https://horizon-testnet.stellar.org"
    _DEFAULT_RPC = "https://soroban-testnet.stellar.org"
    FRIENDBOT_URL = "https://friendbot.stellar.org"
    _DEFAULT_TOOL_REGISTRY = "CARJRAXMYOM7TNNCR2K4OVZTO6BYUTUIW5AB2ZHDSF5UZXBSWU7YZQBW"
    _DEFAULT_COMPLIANCE = "CA6CNNWPEP7CIOQA2YOQ425O6BVU6JUVY4375MBCO6D4FRXGN2ZCPHD2"
    _DEFAULT_USDC = "CDUQPQI6SCMDBWK4ZKBMJGMGSJHHO5ARKDW45M7GJSEFNC44RUPD2WJQ"
    _DEFAULT_ESCROW = "CAMPNA3TNE23UYM5WSTXHQWA5HGU7HL6M2T64R5VNQT5PJS6UBIXOCCO"
elif STELLAR_NETWORK == "public":
    NETWORK_PASSPHRASE = Network.PUBLIC_NETWORK_PASSPHRASE
    _DEFAULT_HORIZON = "https://horizon.stellar.org"
    _DEFAULT_RPC = "https://soroban.stellar.org"
    FRIENDBOT_URL = None
    _DEFAULT_TOOL_REGISTRY = None
    _DEFAULT_COMPLIANCE = None
    _DEFAULT_USDC = None
    _DEFAULT_ESCROW = None
else:
    raise ValueError(
        f"Unknown STELLAR_NETWORK='{STELLAR_NETWORK}'. Use 'testnet' or 'public'."
    )

HORIZON_URL = os.getenv("STELLAR_HORIZON_URL", _DEFAULT_HORIZON)
RPC_URL = os.getenv("STELLAR_RPC_URL", _DEFAULT_RPC)

TOOL_REGISTRY_CONTRACT_ID = os.getenv(
    "TOOL_REGISTRY_CONTRACT_ID", _DEFAULT_TOOL_REGISTRY
)
COMPLIANCE_CONTRACT_ID = os.getenv("COMPLIANCE_CONTRACT_ID", _DEFAULT_COMPLIANCE)
USDC_TOKEN_ID = os.getenv("X402_USDC_ADDRESS", _DEFAULT_USDC)
ESCROW_CONTRACT_ID = os.getenv("X402_ESCROW_CONTRACT", _DEFAULT_ESCROW)


@lru_cache(maxsize=4)
def get_soroban_server(rpc_url: str | None = None):
    """Return a shared SorobanServer for `rpc_url`, creating it once.

    Each SorobanServer owns an HTTP client and therefore its own connection
    pool. Constructing one per call — which is what the code used to do, at a
    dozen call sites — throws the connection away every time and pays the full
    setup cost on every request.

    That cost is not theoretical. Measured on a developer machine, 2026-10-20:
    the first call through a fresh server took **13.6 s** and the second,
    through the same one, took **0.28 s**. `soroban-testnet.stellar.org`
    publishes four IPv6 addresses alongside its IPv4 ones, and where IPv6 is
    present but degraded each attempt burns a connect timeout (~3 s here)
    before anything falls back to IPv4, which connects in 0.03 s. Reusing the
    server skips all of that after the first call.

    Thread-safe by way of httpx.Client, which is safe to share across threads —
    which matters because FastAPI runs synchronous endpoints in a threadpool.

    Cached by URL so tests can point at a different RPC without clobbering the
    process-wide instance.
    """
    from stellar_sdk import SorobanServer

    return SorobanServer(rpc_url or RPC_URL)


def require_contract_id(name: str, value: str | None) -> str:
    """Raise a clear error if a contract ID is needed but not configured.

    Use this at the call site instead of letting `None` propagate into the
    Stellar SDK (which would error with a cryptic XDR message).
    """
    if value is None:
        raise RuntimeError(
            f"{name} is not configured. Set the environment variable "
            f"(STELLAR_NETWORK={STELLAR_NETWORK} has no default for it)."
        )
    return value
