from __future__ import annotations

import base64
import time

import pytest

from research_agent.stellar_identity import (
    generate_agent_keypair,
    fund_testnet,
    set_capability_manifest,
    establish_rep_trustline,
    read_capability_manifest,
    onboard_agent,
    HORIZON_TESTNET,
    REP_ASSET_CONTRACT,
)

from stellar_sdk import Server


@pytest.fixture(scope="module")
def funded_keypair():
    """Generate and fund a fresh testnet keypair once for the module."""
    kp = generate_agent_keypair()
    funded = fund_testnet(kp.public_key)
    assert funded, f"Friendbot failed to fund {kp.public_key}"
    time.sleep(2)  # let ledger settle
    return kp


@pytest.fixture(scope="module")
def horizon():
    return Server(HORIZON_TESTNET)


class TestKeypairGeneration:
    def test_generate_keypair(self):
        kp = generate_agent_keypair()
        assert kp.public_key.startswith("G")
        assert len(kp.public_key) == 56
        assert kp.secret.startswith("S")
        assert len(kp.secret) == 56

    def test_two_keypairs_are_different(self):
        kp1 = generate_agent_keypair()
        kp2 = generate_agent_keypair()
        assert kp1.public_key != kp2.public_key

    def test_fund_testnet(self):
        kp = generate_agent_keypair()
        result = fund_testnet(kp.public_key)
        assert result is True

    def test_funded_account_exists_on_horizon(self, funded_keypair, horizon):
        account = horizon.accounts().account_id(funded_keypair.public_key).call()
        assert account["id"] == funded_keypair.public_key
        xlm_balance = next(
            b for b in account["balances"] if b["asset_type"] == "native"
        )
        assert float(xlm_balance["balance"]) > 0  # Friendbot funds with 10000 XLM


class TestManageDataManifest:
    def test_set_capabilities(self, funded_keypair, horizon):
        tx_hash = set_capability_manifest(
            keypair=funded_keypair,
            capabilities=["PDF", "OCR", "Extraction"],
            model_tier="HAIKU",
            rate_stroops=800000,
        )
        assert tx_hash is not None
        assert len(tx_hash) == 64

    def test_read_capabilities_from_horizon(self, funded_keypair, horizon):
        time.sleep(2)
        account = horizon.accounts().account_id(funded_keypair.public_key).call()
        data = account.get("data", {})

        assert "cap:PDF" in data
        assert "cap:OCR" in data
        assert "cap:Extraction" in data
        assert "model_tier" in data
        assert "rate_stroops" in data

        # manage_data values are base64-encoded on Horizon
        tier = base64.b64decode(data["model_tier"]).decode()
        assert tier == "HAIKU"

        rate = base64.b64decode(data["rate_stroops"]).decode()
        assert rate == "800000"

    def test_read_manifest_helper(self, funded_keypair):
        manifest = read_capability_manifest(funded_keypair.public_key)
        assert "PDF" in manifest["capabilities"]
        assert "OCR" in manifest["capabilities"]
        assert manifest["model_tier"] == "HAIKU"
        assert manifest["rate_stroops"] == 800000


class TestREPTrustline:
    def test_establish_trustline(self, funded_keypair):
        tx_hash = establish_rep_trustline(funded_keypair)
        assert tx_hash is not None
        assert len(tx_hash) == 64

    def test_trustline_exists_on_horizon(self, funded_keypair, horizon):
        time.sleep(2)
        account = horizon.accounts().account_id(funded_keypair.public_key).call()
        non_native = [
            b for b in account["balances"] if b["asset_type"] != "native"
        ]
        assert len(non_native) >= 1, "No trustline found after establish_rep_trustline"


class TestFullOnboarding:
    def test_onboard_new_agent(self):
        result = onboard_agent(
            capabilities=["Web", "Search"],
            model_tier="SONNET",
            rate_stroops=1200000,
        )
        assert "keypair" in result
        assert "manifest_tx" in result
        assert "trustline_tx" in result
        assert result["keypair"].public_key.startswith("G")

        time.sleep(3)
        manifest = read_capability_manifest(result["keypair"].public_key)
        assert "Web" in manifest["capabilities"]
        assert "Search" in manifest["capabilities"]
        assert manifest["model_tier"] == "SONNET"
        assert manifest["rate_stroops"] == 1200000
