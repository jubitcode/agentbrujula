"""slowapi key function must read rightmost X-Forwarded-For.

Default slowapi.get_remote_address reads XFF leftmost (attacker-controlled).
Behind Front Door (which APPENDS client IP rightmost), the attacker can rotate
`X-Forwarded-For: 10.0.0.<N>` to bypass every rate limit. _real_ip_key keys
on the rightmost segment instead."""

import sys
from pathlib import Path
from unittest.mock import MagicMock

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "src"))


def _make_request(xff: str | None = None, client_host: str = "10.1.2.3"):
    req = MagicMock()
    req.headers = {"X-Forwarded-For": xff} if xff is not None else {}
    req.client = MagicMock()
    req.client.host = client_host
    return req


class TestRealIpKey:
    def test_no_xff_uses_socket_host(self):
        from server import _real_ip_key
        assert _real_ip_key(_make_request(xff=None, client_host="10.1.2.3")) == "10.1.2.3"

    def test_single_xff_value_used(self):
        from server import _real_ip_key
        assert _real_ip_key(_make_request(xff="203.0.113.7")) == "203.0.113.7"

    def test_rightmost_xff_segment_wins(self):
        """Attacker spoofs leftmost XFF; FD appends real client IP rightmost — key must use the rightmost."""
        from server import _real_ip_key
        spoofed = "1.2.3.4, 203.0.113.7"  # attacker-supplied, FD-appended-real-IP
        assert _real_ip_key(_make_request(xff=spoofed)) == "203.0.113.7"

    def test_multi_hop_xff_takes_rightmost(self):
        from server import _real_ip_key
        chain = "1.1.1.1, 2.2.2.2, 203.0.113.7"
        assert _real_ip_key(_make_request(xff=chain)) == "203.0.113.7"

    def test_attacker_rotating_leftmost_xff_keyed_consistently(self):
        """100 spoofed leftmost-XFFs from one real client must collapse to a single rate-limit key."""
        from server import _real_ip_key
        keys = set()
        for n in range(100):
            spoofed = f"10.0.0.{n}, 203.0.113.7"  # FD appends real IP
            keys.add(_real_ip_key(_make_request(xff=spoofed)))
        assert keys == {"203.0.113.7"}, (
            f"Rate-limit key must be stable for one real client regardless of "
            f"spoofed XFF; got {len(keys)} distinct keys: {keys}"
        )

    def test_whitespace_in_xff_stripped(self):
        from server import _real_ip_key
        assert _real_ip_key(_make_request(xff="1.2.3.4,  203.0.113.7  ")) == "203.0.113.7"

    def test_empty_xff_falls_back_to_socket_host(self):
        from server import _real_ip_key
        assert _real_ip_key(_make_request(xff="", client_host="10.1.2.3")) == "10.1.2.3"

    def test_no_client_returns_unknown(self):
        from server import _real_ip_key
        req = MagicMock()
        req.headers = {}
        req.client = None
        assert _real_ip_key(req) == "unknown"
