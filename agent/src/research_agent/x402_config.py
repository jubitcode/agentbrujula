import os

from .stellar_config import (
    ESCROW_CONTRACT_ID,
    STELLAR_NETWORK,
    USDC_TOKEN_ID,
)

X402_ENABLED = os.getenv("X402_ENABLED", "true").lower() == "true"
X402_FACILITATOR_URL = os.getenv(
    "X402_FACILITATOR_URL",
    "https://x402.org/facilitator",  # Free testnet facilitator
)
_X402_NETWORK_FROM_STELLAR = (
    "stellar:mainnet" if STELLAR_NETWORK == "public" else "stellar:testnet"
)
X402_NETWORK = os.getenv("X402_NETWORK", _X402_NETWORK_FROM_STELLAR)

X402_USDC_ADDRESS = USDC_TOKEN_ID
X402_ESCROW_CONTRACT = ESCROW_CONTRACT_ID

if X402_ENABLED and STELLAR_NETWORK == "public":
    _missing = [
        name
        for name, value in (
            ("X402_USDC_ADDRESS", X402_USDC_ADDRESS),
            ("X402_ESCROW_CONTRACT", X402_ESCROW_CONTRACT),
        )
        if value is None
    ]
    if _missing:
        raise RuntimeError(
            f"STELLAR_NETWORK=public requires {', '.join(_missing)} to be set "
            f"(no default exists on mainnet)."
        )
