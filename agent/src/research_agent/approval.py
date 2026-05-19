"""Stdin approval gate for the hire flow."""

from __future__ import annotations


def prompt_approval() -> bool:
    """Prompt the user and block until they respond yes or no."""
    try:
        answer = input("\nApprove hire? (yes/no): ").strip().lower()
    except (EOFError, KeyboardInterrupt):
        print()
        return False
    return answer in ("yes", "y")
