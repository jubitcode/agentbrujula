from __future__ import annotations

import os
import secrets
import time
import uuid

import jwt

_SECRET_ENV = "SESSION_TOKEN_SECRET"
_DEFAULT_TTL_SECONDS = int(os.environ.get("SESSION_TOKEN_TTL_SECONDS", "3600"))
_ALGORITHM = "HS256"

# Known-public secrets (committed to docker-compose.yml / .env.example).
# Any deploy using one of these can have JWTs forged by anyone with repo
# access. Refuse to start with one of these unless STELLAR_NETWORK=testnet.
_KNOWN_WEAK_SECRETS = frozenset({
    "local-dev-session-secret-do-not-use-in-prod-32chars-min",
    "dev-session-secret-do-not-use-in-prod",
})


def _load_secret() -> str:
    """Read the HMAC secret from env or generate ephemeral.

    On non-testnet networks, refuses to start with a known-public default.
    Production always provides the secret via Key Vault injection.
    """
    secret = os.environ.get(_SECRET_ENV, "").strip()
    if not secret:
        secret = secrets.token_hex(32)
    if len(secret) < 32:
        raise RuntimeError(
            f"{_SECRET_ENV} must be at least 32 chars (got {len(secret)})"
        )
    network = os.environ.get("STELLAR_NETWORK", "testnet").strip().lower()
    if secret in _KNOWN_WEAK_SECRETS and network != "testnet":
        raise RuntimeError(
            f"{_SECRET_ENV} is set to a known-public default value while "
            f"STELLAR_NETWORK={network!r}. Rotate the secret before deploying "
            f"to a non-testnet environment."
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
