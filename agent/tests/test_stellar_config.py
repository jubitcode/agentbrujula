import os
import pytest


def _load_config(**env_overrides):
    """Reload stellar_config under env overrides — module reads env at import."""
    import importlib
    import sys

    old_env = {}
    for k, v in env_overrides.items():
        old_env[k] = os.environ.get(k)
        if v is None:
            os.environ.pop(k, None)
        else:
            os.environ[k] = v

    # Remove cached module so it re-reads env on next import
    sys.modules.pop("research_agent.stellar_config", None)

    try:
        mod = importlib.import_module("research_agent.stellar_config")
        return mod
    finally:
        for k, v in old_env.items():
            if v is None:
                os.environ.pop(k, None)
            else:
                os.environ[k] = v
        sys.modules.pop("research_agent.stellar_config", None)


class TestStellarConfigDefaults:
    """Default (no env vars) must resolve to testnet."""

    def test_testnet_network(self):
        cfg = _load_config(STELLAR_NETWORK=None, STELLAR_HORIZON_URL=None, STELLAR_RPC_URL=None)
        assert cfg.STELLAR_NETWORK == "testnet"

    def test_testnet_passphrase(self):
        from stellar_sdk import Network
        cfg = _load_config(STELLAR_NETWORK=None)
        assert cfg.NETWORK_PASSPHRASE == Network.TESTNET_NETWORK_PASSPHRASE

    def test_testnet_horizon_url(self):
        cfg = _load_config(STELLAR_NETWORK=None, STELLAR_HORIZON_URL=None)
        assert "horizon-testnet" in cfg.HORIZON_URL

    def test_testnet_rpc_url(self):
        cfg = _load_config(STELLAR_NETWORK=None, STELLAR_RPC_URL=None)
        assert "soroban-testnet" in cfg.RPC_URL

    def test_friendbot_available_on_testnet(self):
        cfg = _load_config(STELLAR_NETWORK=None)
        assert cfg.FRIENDBOT_URL is not None
        assert "friendbot" in cfg.FRIENDBOT_URL


class TestStellarConfigPublic:
    """STELLAR_NETWORK=public must resolve to mainnet endpoints."""

    def test_public_network(self):
        cfg = _load_config(STELLAR_NETWORK="public")
        assert cfg.STELLAR_NETWORK == "public"

    def test_public_passphrase(self):
        from stellar_sdk import Network
        cfg = _load_config(STELLAR_NETWORK="public")
        assert cfg.NETWORK_PASSPHRASE == Network.PUBLIC_NETWORK_PASSPHRASE

    def test_public_horizon_url(self):
        cfg = _load_config(STELLAR_NETWORK="public", STELLAR_HORIZON_URL=None)
        assert "horizon-testnet" not in cfg.HORIZON_URL
        assert "horizon" in cfg.HORIZON_URL

    def test_public_rpc_url(self):
        cfg = _load_config(STELLAR_NETWORK="public", STELLAR_RPC_URL=None)
        assert "soroban-testnet" not in cfg.RPC_URL

    def test_no_friendbot_on_public(self):
        cfg = _load_config(STELLAR_NETWORK="public")
        assert cfg.FRIENDBOT_URL is None


class TestStellarConfigCustom:
    def test_custom_horizon_url(self):
        cfg = _load_config(STELLAR_HORIZON_URL="https://my-horizon.example.com")
        assert cfg.HORIZON_URL == "https://my-horizon.example.com"

    def test_custom_rpc_url(self):
        cfg = _load_config(STELLAR_RPC_URL="https://my-rpc.example.com")
        assert cfg.RPC_URL == "https://my-rpc.example.com"

    def test_custom_overrides_network_default(self):
        """Custom URL must beat the network-derived default."""
        cfg = _load_config(STELLAR_NETWORK="public", STELLAR_HORIZON_URL="https://custom.example.com")
        assert cfg.HORIZON_URL == "https://custom.example.com"


class TestStellarConfigInvalid:
    def test_invalid_network_raises(self):
        with pytest.raises(ValueError, match="Unknown STELLAR_NETWORK"):
            _load_config(STELLAR_NETWORK="garbage")
