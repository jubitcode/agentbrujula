"""Terminal table formatter for agent cost summaries."""

from __future__ import annotations

from dataclasses import dataclass


@dataclass
class AgentRow:
    handle: str
    model_tier: str
    cost_xlm: float
    rep_score: int
    hebbian_pick: bool


def print_cost_table(agents: list[AgentRow]) -> str:
    """Print a formatted cost table and return the rendered string."""
    header = f"{'Agent':<20} {'Tier':<8} {'Cost':<10} {'REP':<6} {'Hebbian pick'}"
    sep = "\u2500" * len(header)
    lines = [header, sep]

    for a in agents:
        pick = "yes" if a.hebbian_pick else "no"
        cost = f"{a.cost_xlm:.1f} XLM"
        lines.append(
            f"{a.handle:<20} {a.model_tier:<8} {cost:<10} {a.rep_score:<6} {pick}"
        )

    table = "\n".join(lines)
    print(table)
    return table
