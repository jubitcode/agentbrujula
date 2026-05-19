from __future__ import annotations

import os
import secrets
import time
import uuid

import jwt

_SECRET_ENV = "SESSION_TOKEN_SECRET"
_DEFAULT_TTL_SECONDS = int(os.environ.get("SESSION_TOKEN_TTL_SECONDS", "3600"))
_ALGORITHM = "HS256"

_KNOWN_WEAK_SECRETS = frozenset({
    "local-dev-session-secret-do-not-use-in-prod-32chars-min",
    "dev-session-secret-do-not-use-in-prod",
})

_TESTNET_USDC_SAC = "CDUQPQI6SCMDBWK4ZKBMJGMGSJHHO5ARKDW45M7GJSEFNC44RUPD2WJQ"


def _looks_like_testnet() -> bool:
    network = os.environ.get("STELLAR_NETWORK", "testnet").strip().lower()
    if network != "testnet":
        return False
    horizon = os.environ.get("STELLAR_HORIZON_URL", "").lower()
    if horizon and "testnet" not in horizon:
        return False
    rpc = os.environ.get("STELLAR_RPC_URL", "").lower()
    if rpc and "testnet" not in rpc:
        return False
    usdc = os.environ.get("X402_USDC_ADDRESS", "").strip()
    if usdc and usdc != _TESTNET_USDC_SAC:
        return False
    return True


def _load_secret() -> str:
    secret = os.environ.get(_SECRET_ENV, "").strip()
    if not secret:
        secret = secrets.token_hex(32)
    if len(secret) < 32:
        raise RuntimeError(
            f"{_SECRET_ENV} must be at least 32 chars (got {len(secret)})"
        )
    if secret in _KNOWN_WEAK_SECRETS and not _looks_like_testnet():
        raise RuntimeError(
            f"{_SECRET_ENV} is a known-public default but the environment "
            f"does not consistently look like testnet "
            f"(STELLAR_NETWORK, STELLAR_HORIZON_URL, STELLAR_RPC_URL, "
            f"X402_USDC_ADDRESS). Rotate the secret."
        )
    return secret


_SECRET = _load_secret()


def issue(public_key: str, ttl_seconds: int | None = None) -> tuple[str, int]:
    """Issue a JWT bound to public_key.

    Returns (token, expires_in_seconds).
    """
    ttl = ttl_seconds if ttl_seconds is not None else _DEFAULT_TTL_SECONDS
    now = int(time.time())
    payload = {
        "sub": public_key,
        "iat": now,
        "exp": now + ttl,
        "jti": uuid.uuid4().hex,
    }
    token = jwt.encode(payload, _SECRET, algorithm=_ALGORITHM)
    return token, ttl


def verify(token: str) -> str:
    """Verify a JWT and return the public_key (sub claim).

    Raises jwt.InvalidTokenError (or subclass) on any failure:
    - expired token (ExpiredSignatureError)
    - bad signature (InvalidSignatureError)
    - malformed (DecodeError)
    """
    payload = jwt.decode(token, _SECRET, algorithms=[_ALGORITHM])
    sub = payload.get("sub")
    if not isinstance(sub, str) or not sub:
        raise jwt.InvalidTokenError("Token missing sub claim")
    return sub
