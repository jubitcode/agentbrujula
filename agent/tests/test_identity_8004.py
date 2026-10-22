"""Where an agent gets paid, read from a registry we do not control.

Before this, the payment address came from our own database, so "the agent was
paid correctly" was a claim only we could check. The 8004 registry holds the
id -> wallet link on chain, which makes the destination verifiable by the
client against a source neither side owns.

The properties worth fixing are about honesty of provenance rather than about
the lookup itself: a local value must never be presented as an on-chain one,
and a registry that cannot be reached must not silently become one.

The registry is doubled here. The live contract is exercised in
`manual_8004_e2e.py` against testnet, where agent id 100 is registered.
"""

import pytest

from research_agent import identity_8004 as idn


REGISTRY = "CDE3K4COIAGWNNJQQLL26SYI3KBJF5FUDHXG5FA6GYDJCG7T5V7FIWZH"
AGENT_WALLET = "GDEEV6O53VSZDKCLSOE2IVK3B4YRYVVLIVVY4434DNPCT56IJVM57KDA"
LOCAL_FALLBACK = "GCB7WHXIIFPGOX36FLV53Z7BF35TFCKZKZFPLL6BKUDWCD2VS2T2PF7Y"


class TestRegistryConfiguration:
    def test_testnet_has_a_default(self, monkeypatch):
        monkeypatch.delenv(idn.ENV_REGISTRY, raising=False)
        monkeypatch.setattr(idn, "NETWORK_PASSPHRASE",
                            "Test SDF Network ; September 2015")
        assert idn.registry_contract() == idn.DEFAULT_TESTNET_REGISTRY

    def test_outside_testnet_it_refuses_to_guess(self, monkeypatch):
        """The mainnet registry is a different contract. Defaulting there
        would link agents to identities nobody can resolve."""
        monkeypatch.delenv(idn.ENV_REGISTRY, raising=False)
        monkeypatch.setattr(idn, "NETWORK_PASSPHRASE",
                            "Public Global Stellar Network ; September 2015")
        with pytest.raises(idn.IdentityNotConfigured):
            idn.registry_contract()

    def test_the_environment_wins_over_the_default(self, monkeypatch):
        monkeypatch.setenv(idn.ENV_REGISTRY, "CSOMEWHEREELSE")
        assert idn.registry_contract() == "CSOMEWHEREELSE"

    def test_an_explicit_argument_wins_over_both(self, monkeypatch):
        monkeypatch.setenv(idn.ENV_REGISTRY, "CFROMENV")
        assert idn.registry_contract("CEXPLICIT") == "CEXPLICIT"


class TestPaymentAddressProvenance:
    """`resolve_payment_address` returns (address, source). The source is the
    whole point: "the chain says so" and "we say so" are different claims and
    must not be reported as the same one."""

    def test_a_linked_wallet_is_used_and_labelled_as_on_chain(self, monkeypatch):
        monkeypatch.setattr(idn, "get_agent_wallet", lambda *a, **k: AGENT_WALLET)
        addr, source = idn.resolve_payment_address(100, LOCAL_FALLBACK)
        assert addr == AGENT_WALLET
        assert source == "8004"

    def test_no_agent_id_falls_back_and_says_so(self):
        addr, source = idn.resolve_payment_address(None, LOCAL_FALLBACK)
        assert addr == LOCAL_FALLBACK
        assert source == "local"

    def test_an_unregistered_id_falls_back_and_says_so(self, monkeypatch):
        monkeypatch.setattr(idn, "get_agent_wallet", lambda *a, **k: None)
        addr, source = idn.resolve_payment_address(999999, LOCAL_FALLBACK)
        assert addr == LOCAL_FALLBACK
        assert source == "local"

    def test_an_unreachable_registry_falls_back_and_says_so(self, monkeypatch):
        """Refusing to hire a working agent because a third-party registry is
        down would be the wrong trade. Pretending its answer was on-chain
        would be worse."""
        def boom(*a, **k):
            raise ConnectionError("soroban-testnet.stellar.org unreachable")

        monkeypatch.setattr(idn, "get_agent_wallet", boom)
        addr, source = idn.resolve_payment_address(100, LOCAL_FALLBACK)
        assert addr == LOCAL_FALLBACK
        assert source == "local"

    def test_the_fallback_is_never_labelled_on_chain(self, monkeypatch):
        """The one mislabelling that would matter: a local value presented as
        something the client can verify against the registry."""
        for wallet in (None, ""):
            monkeypatch.setattr(idn, "get_agent_wallet", lambda *a, **k: wallet)
            _, source = idn.resolve_payment_address(100, LOCAL_FALLBACK)
            assert source == "local"


class TestReadsNeedNoSigner:
    """Reads go through simulation. A client checking where their money went
    must not need an account, a key, or our permission."""

    @pytest.mark.parametrize("fn,args", [
        (idn.get_agent_wallet, (100,)),
        (idn.agent_exists, (100,)),
        (idn.find_owner, (100,)),
        (idn.agent_uri, (100,)),
    ])
    def test_read_functions_take_no_keypair(self, fn, args):
        import inspect

        params = list(inspect.signature(fn).parameters)
        assert "kp" not in params and "keypair" not in params, (
            f"{fn.__name__} takes a signer; reads must not"
        )

    def test_writes_refuse_without_one(self):
        with pytest.raises(ValueError, match="needs a keypair"):
            idn._invoke(None, "set_agent_wallet", [], read_only=False)
