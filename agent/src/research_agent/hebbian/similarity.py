"""Shared TF-IDF and embedding cosine similarity utilities."""

from __future__ import annotations

import math
import re
from collections import Counter

import numpy as np


_STOP_WORDS = frozenset(
    {
        "the",
        "is",
        "are",
        "was",
        "were",
        "be",
        "been",
        "being",
        "have",
        "has",
        "had",
        "do",
        "doe",
        "did",
        "will",
        "would",
        "could",
        "should",
        "may",
        "might",
        "shall",
        "can",
        "and",
        "but",
        "or",
        "nor",
        "not",
        "no",
        "so",
        "if",
        "then",
        "than",
        "that",
        "this",
        "these",
        "those",
        "what",
        "which",
        "who",
        "whom",
        "how",
        "when",
        "where",
        "why",
        "of",
        "in",
        "on",
        "at",
        "to",
        "for",
        "with",
        "by",
        "from",
        "into",
        "about",
        "between",
        "through",
        "it",
        "its",
        "an",
        "some",
        "any",
        "all",
        "each",
        "every",
    }
)


def _stem(word: str) -> str:
    """Minimal suffix stripping — handles plurals and common endings."""
    if len(word) <= 3:
        return word
    if word.endswith("ies") and len(word) > 4:
        return word[:-3] + "y"
    if word.endswith("es") and len(word) > 4:
        return word[:-2]
    if word.endswith("s") and not word.endswith("ss") and len(word) > 3:
        return word[:-1]
    if word.endswith("ing") and len(word) > 5:
        return word[:-3]
    if word.endswith("ed") and len(word) > 4:
        return word[:-2]
    return word


def _tokenize(text: str) -> list[str]:
    tokens = re.findall(r"[a-z][a-z0-9]+", text.lower())
    return [_stem(t) for t in tokens if t not in _STOP_WORDS]


def tfidf_most_similar(
    query: str,
    corpus: dict[str, str],
    top_k: int = 5,
) -> list[tuple[str, float]]:
    """Return (id, score) pairs sorted descending by TF-IDF cosine similarity.

    Args:
        query: text to compare against corpus
        corpus: {id: text} mapping
        top_k: max results to return
    """
    if not corpus:
        return []

    query_tokens = _tokenize(query)
    if not query_tokens:
        return []

    all_docs = list(corpus.values()) + [query]
    n_docs = len(all_docs)

    df: Counter = Counter()
    for doc in all_docs:
        for term in set(_tokenize(doc)):
            df[term] += 1

    query_tf = Counter(query_tokens)
    query_vec = {
        term: (count / len(query_tokens)) * math.log(n_docs / df[term])
        for term, count in query_tf.items()
        if df[term] < n_docs
    }
    if not query_vec:
        return []

    query_norm = math.sqrt(sum(v * v for v in query_vec.values()))
    if query_norm == 0:
        return []

    results = []
    for doc_id, text in corpus.items():
        tokens = _tokenize(text)
        if not tokens:
            continue

        tf = Counter(tokens)
        doc_vec = {
            term: (count / len(tokens)) * math.log(n_docs / df[term])
            for term, count in tf.items()
            if df[term] < n_docs
        }

        dot = sum(query_vec.get(t, 0) * v for t, v in doc_vec.items())
        doc_norm = math.sqrt(sum(v * v for v in doc_vec.values()))
        if doc_norm == 0:
            continue

        sim = dot / (query_norm * doc_norm)
        if sim > 0:
            results.append((doc_id, sim))

    results.sort(key=lambda x: x[1], reverse=True)
    return results[:top_k]


def embedding_most_similar(
    query_embedding: np.ndarray,
    candidates: dict[str, np.ndarray],
    top_k: int = 5,
) -> list[tuple[str, float]]:
    """Return (id, score) pairs sorted descending by cosine similarity.

    Args:
        query_embedding: normalized query vector
        candidates: {node_id: normalized_embedding} mapping
        top_k: max results to return
    """
    if not candidates:
        return []

    ids = list(candidates.keys())
    matrix = np.stack([candidates[nid] for nid in ids])  # (N, dim)

    query = query_embedding.reshape(1, -1)  # (1, dim)
    sims = (matrix @ query.T).squeeze()  # (N,)

    if sims.ndim == 0:
        sims = np.array([float(sims)])

    top_indices = np.argsort(sims)[::-1][:top_k]
    return [(ids[i], float(sims[i])) for i in top_indices if sims[i] > 0]
