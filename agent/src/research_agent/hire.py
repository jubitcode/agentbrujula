from __future__ import annotations


def _match_capabilities(agent_caps: list[str], required: list[str]) -> bool:
    """True if any required capability appears in agent_caps (case-insensitive).

    Empty `required` → no match. Empty `agent_caps` → no match. Both empty
    → no match. Otherwise: case-insensitive set membership of any required
    item in the agent's capabilities.
    """
    agent_set = {c.lower() for c in agent_caps}
    return any(r.lower() in agent_set for r in required)
