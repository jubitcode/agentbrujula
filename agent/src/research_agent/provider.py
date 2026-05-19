from __future__ import annotations

import os
from dataclasses import dataclass


@dataclass
class ProviderConfig:
    base_url: str
    api_key: str
    model: str


LM_STUDIO = ProviderConfig(
    base_url="http://localhost:1234/v1",
    api_key="lm-studio",
    model="qwen2.5-7b-instruct",
)

OPENROUTER = ProviderConfig(
    base_url="https://openrouter.ai/api/v1",
    api_key=os.environ.get("OPENROUTER_API_KEY", ""),
    model=os.environ.get("OPENROUTER_MODEL", "google/gemini-2.0-flash-001"),
)

CLAUDE = ProviderConfig(
    base_url="https://api.anthropic.com/v1",
    api_key="",
    model="claude-sonnet-4-20250514",
)

LIGHTING = ProviderConfig(
    base_url="http://localhost:8090/v1",
    api_key="lighting",
    model="llama-3.1-8b-instruct",  # model name in API (server ignores this)
)

LIGHTING_QWEN = ProviderConfig(
    base_url="http://localhost:8090/v1",
    api_key="lighting",
    model="qwen2.5-7b-instruct",
)
