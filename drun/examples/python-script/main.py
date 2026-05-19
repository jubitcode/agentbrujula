#!/usr/bin/env python3
"""Simple Python script example for drun."""

import sys
from rich.console import Console
from rich.table import Table

console = Console()


def get_system_info() -> dict:
    """Gather system information."""
    return {
        "Python Version": sys.version.split()[0],
        "Platform": sys.platform,
        "Executable": sys.executable,
    }


def main() -> None:
    """Main entry point."""
    console.print("[bold green]Hello from Python![/bold green]\n")

    table = Table(title="System Information")
    table.add_column("Property", style="cyan")
    table.add_column("Value", style="magenta")

    for key, value in get_system_info().items():
        table.add_row(key, value)

    console.print(table)


if __name__ == "__main__":
    main()
