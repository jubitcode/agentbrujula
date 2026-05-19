from __future__ import annotations

from dataclasses import dataclass, field
from typing import Callable


@dataclass
class Tool:
    name: str
    description: str
    category: str
    parameters: dict
    executor: Callable[[dict], str]
    tags: list[str] = field(default_factory=list)
    use_cases: list[str] = field(default_factory=list)
    example_intents: list[str] = field(default_factory=list)

    def to_schema_dict(self) -> dict:
        return {
            "name": self.name,
            "description": self.description,
            "category": self.category,
            "parameters": self.parameters,
            "tags": self.tags,
            "use_cases": self.use_cases,
            "example_intents": self.example_intents,
        }

    @classmethod
    def from_schema(cls, schema: dict, executor: Callable[[dict], str]) -> Tool:
        return cls(
            name=schema["name"],
            description=schema.get("description", ""),
            category=schema.get("category", ""),
            parameters=schema.get("parameters", {}),
            executor=executor,
            tags=schema.get("tags", []),
            use_cases=schema.get("use_cases", []),
            example_intents=schema.get("example_intents", []),
        )
