"""Wallet sign-in by SEP-53 message signature.

SEP-53 ("Sign and Verify Messages", Final) defines how a Stellar account signs
arbitrary bytes. The payload is hashed under a fixed prefix before signing:

    sha256(b"Stellar Signed Message:\\n" + message)

The prefix is the point. Without it, a signature over arbitrary bytes is
indistinguishable from a signature over something else the key is asked to sign
— a transaction hash, an auth entry — so a login challenge could be replayed as
consent to something the user never saw. The prefix puts message signing in its
own domain, where it cannot collide with those.

We verify what wallets produce: Freighter and the other stellar-wallets-kit
modules apply this framing inside `signMessage`, so the caller passes the raw
challenge and the wallet handles the rest.

⚠ What a valid signature proves, per SEP-53 §Security Concerns: control of the
private key, NOT control of the Stellar account. For a multisig account the two
are different — a signer below the account's threshold still produces a valid
signature here. Callers that need account control (rather than key possession)
must check the account's signers and thresholds on top of this.
"""

from __future__ import annotations

import base64
import binascii
import hashlib

from stellar_sdk import Keypair

#: The domain-separation prefix fixed by SEP-53. Changing it breaks every
#: wallet, which is the intended property.
SEP53_PREFIX = b"Stellar Signed Message:\n"


def sep53_digest(message: str | bytes) -> bytes:
    """Return the 32-byte payload a wallet signs for `message` under SEP-53."""
    if isinstance(message, str):
        message = message.encode("utf-8")
    return hashlib.sha256(SEP53_PREFIX + message).digest()


def _decode_signature(signature: str) -> bytes | None:
    """Accept the encodings wallets return: hex or base64.

    SEP-43's `signMessage` does not pin one — implementations differ — so we try
    both rather than rejecting a correctly signed message on formatting.
    Returns None when the input is neither.
    """
    try:
        raw = bytes.fromhex(signature)
        if len(raw) == 64:
            return raw
    except (ValueError, binascii.Error):
        pass

    try:
        raw = base64.b64decode(signature, validate=True)
        if len(raw) == 64:
            return raw
    except (ValueError, binascii.Error):
        pass

    return None


def verify_wallet_signature(
    public_key: str, challenge: str, signature: str
) -> bool:
    """Verify a SEP-53 message signature over `challenge`.

    Args:
        public_key: Stellar public key (G...).
        challenge: The challenge string the wallet was asked to sign, exactly as
            issued — it is hashed under the SEP-53 prefix here, so the caller
            passes it unmodified.
        signature: The 64-byte ed25519 signature, hex or base64 encoded.

    Returns:
        True when the signature is valid for this key and challenge.

    Never raises: a malformed key, a malformed signature and a wrong signature
    are all simply "not authenticated".
    """
    if not challenge or not signature or not public_key:
        return False

    signature_bytes = _decode_signature(signature)
    if signature_bytes is None:
        return False

    try:
        kp = Keypair.from_public_key(public_key)
        kp.verify(sep53_digest(challenge), signature_bytes)
        return True
    except Exception:
        return False
