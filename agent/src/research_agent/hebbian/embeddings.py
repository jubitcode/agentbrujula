# hebbian_memory/embeddings.py

import os
import numpy as np
import httpx
from abc import ABC, abstractmethod


class EmbeddingProvider(ABC):
    @abstractmethod
    def embed(self, text: str, *, for_query: bool = False) -> np.ndarray:
        pass

    @abstractmethod
    def embed_batch(
        self, texts: list[str], *, for_query: bool = False
    ) -> list[np.ndarray]:
        pass

    @property
    @abstractmethod
    def dim(self) -> int:
        pass


class SentenceTransformerProvider(EmbeddingProvider):
    """Original provider — kept as fallback when no LM Studio server is running."""

    def __init__(self, model_name: str = "all-mpnet-base-v2"):
        from sentence_transformers import SentenceTransformer

        self._model = SentenceTransformer(model_name)
        self._dim = self._model.get_sentence_embedding_dimension()

    def embed(self, text: str, *, for_query: bool = False) -> np.ndarray:
        result = self._model.encode(text, normalize_embeddings=True)
        return np.asarray(result)

    def embed_batch(
        self, texts: list[str], *, for_query: bool = False
    ) -> list[np.ndarray]:
        results = self._model.encode(texts, normalize_embeddings=True)
        return [np.asarray(r) for r in results]

    @property
    def dim(self) -> int:
        return self._dim


class Qwen3EmbeddingProvider(EmbeddingProvider):
    """
    Qwen3-Embedding-0.6B via LM Studio local server.

    Setup:
        1. Open LM Studio
        2. Search and download: "Qwen3-Embedding-0.6B-GGUF"
        3. Load the model
        4. Start the local server (default: http://localhost:1234)

    Key features vs nomic-embed-text:
        - 1024-dim output (vs 768) — better retrieval quality
        - 32K context window (vs 8192) — handles long task outcomes without truncation
        - Instruction-aware — separate prefixes for indexing vs querying improve precision
        - MRL support — can truncate to 768 via `dimensions=768` to avoid re-embedding

    Dimension note:
        Default output is 1024. If you have an existing graph built at 768 dims,
        either set dimensions=768 (slight quality loss, no migration needed) or
        run migrations.re_embed() to rebuild everything at 1024.
    """

    INSTRUCT_TEMPLATE = "Instruct: {instruction}\nQuery: {text}"

    def __init__(
        self,
        model: str = "qwen3-embedding-0.6b",
        base_url: str = "http://localhost:1234/v1",
        timeout: float = 30.0,
        dimensions: int | None = None,
        index_instruction: str = "Represent this agent task outcome for memory storage:",
        query_instruction: str = "Retrieve memory relevant to this agent task:",
    ):
        self._model = model
        self._base_url = base_url.rstrip("/")
        self._timeout = timeout
        self._dimensions = dimensions
        self._index_instruction = index_instruction
        self._query_instruction = query_instruction
        self._dim = self._discover_dim()

    def _format(self, text: str, instruction: str) -> str:
        return self.INSTRUCT_TEMPLATE.format(instruction=instruction, text=text)

    def _call(self, texts: list[str]) -> list[list[float]]:
        body: dict = {"model": self._model, "input": texts}
        if self._dimensions:
            body["dimensions"] = self._dimensions

        with httpx.Client(timeout=self._timeout) as client:
            response = client.post(
                f"{self._base_url}/embeddings",
                json=body,
                headers={"Content-Type": "application/json"},
            )
            response.raise_for_status()
            data = response.json()

        sorted_items = sorted(data["data"], key=lambda x: x["index"])
        return [item["embedding"] for item in sorted_items]

    def _discover_dim(self) -> int:
        result = self._call(["hello world"])
        return len(result[0])

    def embed(self, text: str, *, for_query: bool = False) -> np.ndarray:
        instruction = self._query_instruction if for_query else self._index_instruction
        formatted = self._format(text, instruction)
        result = self._call([formatted])
        vec = np.array(result[0], dtype=np.float32)
        return vec / (np.linalg.norm(vec) + 1e-10)

    def embed_batch(
        self, texts: list[str], *, for_query: bool = False
    ) -> list[np.ndarray]:
        instruction = self._query_instruction if for_query else self._index_instruction
        formatted = [self._format(t, instruction) for t in texts]
        results = self._call(formatted)
        vecs = [np.array(r, dtype=np.float32) for r in results]
        return [v / (np.linalg.norm(v) + 1e-10) for v in vecs]

    @property
    def dim(self) -> int:
        return self._dim


def get_provider(provider: str, **kwargs) -> EmbeddingProvider:
    """
    Factory. Reads HEBBIAN_EMBEDDING_PROVIDER env var if provider not specified.

    HEBBIAN_EMBEDDING_PROVIDER=qwen3     → Qwen3EmbeddingProvider via LM Studio (recommended)
    HEBBIAN_EMBEDDING_PROVIDER=sentence  → SentenceTransformerProvider (offline fallback)
    """
    source = provider or os.getenv("HEBBIAN_EMBEDDING_PROVIDER", "qwen3")

    if source == "qwen3":
        raw_dim = os.getenv("QWEN3_DIMENSIONS", "0")
        dimensions = int(raw_dim) if raw_dim and int(raw_dim) > 0 else None

        return Qwen3EmbeddingProvider(
            model=kwargs.get(
                "model", os.getenv("QWEN3_EMBED_MODEL", "qwen3-embedding-0.6b")
            ),
            base_url=kwargs.get(
                "base_url", os.getenv("LMSTUDIO_BASE_URL", "http://localhost:1234/v1")
            ),
            dimensions=kwargs.get("dimensions", dimensions),
            index_instruction=kwargs.get(
                "index_instruction",
                os.getenv(
                    "QWEN3_INDEX_INSTRUCTION",
                    "Represent this agent task outcome for memory storage:",
                ),
            ),
            query_instruction=kwargs.get(
                "query_instruction",
                os.getenv(
                    "QWEN3_QUERY_INSTRUCTION",
                    "Retrieve memory relevant to this agent task:",
                ),
            ),
        )

    return SentenceTransformerProvider(
        model_name=kwargs.get("model", "all-mpnet-base-v2")
    )
