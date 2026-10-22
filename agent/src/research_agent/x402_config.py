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

#: Mainnet moves real money, so it is refused unless somebody says so on
#: purpose. Reaching mainnet by forgetting to set STELLAR_NETWORK is not a
#: decision anyone makes deliberately.
X402_ALLOW_MAINNET = os.getenv("X402_ALLOW_MAINNET", "false").lower() == "true"

#: True when this process is configured against mainnet, by either setting.
X402_IS_MAINNET = STELLAR_NETWORK == "public" or X402_NETWORK == "stellar:mainnet"

# The two settings must agree. If they disagree, one of them decides which
# Horizon we read and the other decides what we advertise in the 402 — which is
# exactly how a payment made on testnet ends up accepted by a mainnet
# deployment. Refuse to start rather than resolve it silently.
if X402_NETWORK != _X402_NETWORK_FROM_STELLAR:
    raise RuntimeError(
        f"X402_NETWORK={X402_NETWORK!r} contradicts STELLAR_NETWORK={STELLAR_NETWORK!r} "
        f"(which implies {_X402_NETWORK_FROM_STELLAR!r}). A payment verified against one "
        f"network must never be honoured as payment on the other. Set both, or neither."
    )

if X402_ENABLED and X402_IS_MAINNET and not X402_ALLOW_MAINNET:
    raise RuntimeError(
        "Refusing to start: x402 is enabled against MAINNET but X402_ALLOW_MAINNET "
        "is not set. Real USDC moves on this configuration. Set "
        "X402_ALLOW_MAINNET=true to confirm that is intended."
    )

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
