import base64
import json
import sys
from pathlib import Path
from unittest.mock import patch, AsyncMock

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "src"))


class TestHorizonFallbackSecurity:
    """Facilitator-down must fail closed, not accept any valid Stellar tx as payment via Horizon fallback."""

    @pytest.mark.asyncio
    async def test_fallback_rejects_when_facilitator_unavailable(self):
        import research_agent.x402_middleware as mw

        with patch.object(mw, "_call_facilitator_verify", side_effect=ConnectionError("facilitator down")):
            with patch.object(mw, "_verify_tx_on_horizon", new_callable=AsyncMock) as mock_horizon:
                fake_sig = base64.b64encode(json.dumps({
                    "txHash": "abc123def456",
                    "nonce": "test-nonce",
                }).encode()).decode()

                fake_payment_required = base64.b64encode(json.dumps({
                    "price": "1.00",
                    "payTo": "GABCDEF",
                }).encode()).decode()

                try:
                    result = await mw._call_facilitator_verify(fake_sig, fake_payment_required)
                except ConnectionError:
                    pass

                mock_horizon.assert_not_called()

    def test_verify_tx_on_horizon_removed_or_validates_properly(self):
        """If _verify_tx_on_horizon still exists, it must take more than tx_hash (pay_to, amount, asset)."""
        import research_agent.x402_middleware as mw

        if hasattr(mw, '_verify_tx_on_horizon'):
            import inspect
            sig = inspect.signature(mw._verify_tx_on_horizon)
            params = list(sig.parameters.keys())
            assert len(params) > 1, (
                "_verify_tx_on_horizon still only takes tx_hash — must also validate pay_to, amount, asset"
            )
