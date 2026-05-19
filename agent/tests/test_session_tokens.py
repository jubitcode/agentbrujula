import os
import time

import pytest


@pytest.fixture(autouse=True)
def stable_secret(monkeypatch):
    """session_tokens reads SESSION_TOKEN_SECRET at import — pin to a deterministic value and reload."""
    monkeypatch.setenv("SESSION_TOKEN_SECRET", "a" * 64)
    import importlib
    import sys
    if "research_agent.session_tokens" in sys.modules:
        importlib.reload(sys.modules["research_agent.session_tokens"])


class TestIssue:
    def test_returns_string_token_and_ttl(self):
        from research_agent.session_tokens import issue
        token, ttl = issue("GABCD" + "X" * 51)
        assert isinstance(token, str) and len(token) > 50
        assert ttl == 3600

    def test_custom_ttl(self):
        from research_agent.session_tokens import issue
        _, ttl = issue("GABCD" + "X" * 51, ttl_seconds=60)
        assert ttl == 60


class TestVerify:
    def test_verify_returns_pubkey(self):
        from research_agent.session_tokens import issue, verify
        pubkey = "GABCD" + "X" * 51
        token, _ = issue(pubkey)
        assert verify(token) == pubkey

    def test_verify_expired_raises(self):
        from research_agent.session_tokens import issue, verify
        import jwt as _jwt
        token, _ = issue("GABCD" + "X" * 51, ttl_seconds=1)
        time.sleep(2)
        with pytest.raises(_jwt.ExpiredSignatureError):
            verify(token)

    def test_verify_tampered_signature_raises(self):
        from research_agent.session_tokens import issue, verify
        import jwt as _jwt
        token, _ = issue("GABCD" + "X" * 51)
        # Mutate last char of the signature segment
        tampered = token[:-1] + ("A" if token[-1] != "A" else "B")
        with pytest.raises(_jwt.InvalidTokenError):
            verify(tampered)

    def test_verify_malformed_raises(self):
        from research_agent.session_tokens import verify
        import jwt as _jwt
        with pytest.raises(_jwt.InvalidTokenError):
            verify("not.a.jwt")

    def test_verify_token_signed_with_other_secret_raises(self, monkeypatch):
        import importlib
        import sys
        import jwt as _jwt

        monkeypatch.setenv("SESSION_TOKEN_SECRET", "a" * 64)
        importlib.reload(sys.modules["research_agent.session_tokens"])
        from research_agent.session_tokens import issue
        token, _ = issue("GABCD" + "X" * 51)

        monkeypatch.setenv("SESSION_TOKEN_SECRET", "b" * 64)
        importlib.reload(sys.modules["research_agent.session_tokens"])
        from research_agent.session_tokens import verify
        with pytest.raises(_jwt.InvalidTokenError):
            verify(token)


class TestSecretLoading:
    def test_short_secret_rejected(self, monkeypatch):
        """Refuse to start with a weak HMAC key (<32 chars)."""
        import importlib
        import sys
        monkeypatch.setenv("SESSION_TOKEN_SECRET", "tooshort")
        with pytest.raises(RuntimeError, match="at least 32 chars"):
            if "research_agent.session_tokens" in sys.modules:
                importlib.reload(sys.modules["research_agent.session_tokens"])
            else:
                import research_agent.session_tokens  # noqa: F401

    def test_unset_secret_generates_ephemeral(self, monkeypatch):
        """Unset env → 64-char random hex secret valid within a single process lifetime."""
        import importlib
        import sys
        monkeypatch.delenv("SESSION_TOKEN_SECRET", raising=False)
        if "research_agent.session_tokens" in sys.modules:
            importlib.reload(sys.modules["research_agent.session_tokens"])
        from research_agent.session_tokens import issue, verify
        pubkey = "GABCD" + "X" * 51
        token, _ = issue(pubkey)
        assert verify(token) == pubkey


class TestKnownWeakSecretGuard:
    """Refuse to start with a known-public secret on non-testnet networks."""

    @pytest.mark.parametrize("weak", [
        "local-dev-session-secret-do-not-use-in-prod-32chars-min",
        "dev-session-secret-do-not-use-in-prod",
    ])
    def test_known_weak_rejected_on_public_network(self, monkeypatch, weak):
        import importlib
        import sys
        monkeypatch.setenv("SESSION_TOKEN_SECRET", weak)
        monkeypatch.setenv("STELLAR_NETWORK", "public")
        with pytest.raises(RuntimeError, match="known-public default"):
            if "research_agent.session_tokens" in sys.modules:
                importlib.reload(sys.modules["research_agent.session_tokens"])
            else:
                import research_agent.session_tokens  # noqa: F401

    def test_known_weak_allowed_on_testnet(self, monkeypatch):
        """Testnet convenience — defaults keep working for local dev."""
        import importlib
        import sys
        monkeypatch.setenv(
            "SESSION_TOKEN_SECRET",
            "local-dev-session-secret-do-not-use-in-prod-32chars-min",
        )
        monkeypatch.setenv("STELLAR_NETWORK", "testnet")
        if "research_agent.session_tokens" in sys.modules:
            importlib.reload(sys.modules["research_agent.session_tokens"])
        from research_agent.session_tokens import issue, verify
        pubkey = "GABCD" + "X" * 51
        token, _ = issue(pubkey)
        assert verify(token) == pubkey

    def test_strong_secret_allowed_on_public_network(self, monkeypatch):
        import importlib
        import sys
        monkeypatch.setenv("SESSION_TOKEN_SECRET", "z" * 64)
        monkeypatch.setenv("STELLAR_NETWORK", "public")
        if "research_agent.session_tokens" in sys.modules:
            importlib.reload(sys.modules["research_agent.session_tokens"])
        from research_agent.session_tokens import issue, verify
        pubkey = "GABCD" + "X" * 51
        token, _ = issue(pubkey)
        assert verify(token) == pubkey
