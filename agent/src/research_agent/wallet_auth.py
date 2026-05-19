from __future__ import annotations

from stellar_sdk import Keypair


def verify_wallet_signature(
    public_key: str, challenge: str, signature_hex: str
) -> bool:
    """Verify that a Stellar keypair signed the given challenge.

    Args:
        public_key: Stellar public key (G...)
        challenge: The challenge nonce string
        signature_hex: Hex-encoded signature

    Returns:
        True if valid, False otherwise.
    """
    if not challenge or not signature_hex or not public_key:
        return False

    try:
        kp = Keypair.from_public_key(public_key)
        signature_bytes = bytes.fromhex(signature_hex)
        kp.verify(challenge.encode(), signature_bytes)
        return True
    except Exception:
        return False
