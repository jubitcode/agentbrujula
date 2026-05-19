from .embeddings import EmbeddingProvider


def re_embed(graph, new_provider: EmbeddingProvider, batch_size: int = 32) -> None:
    """
    Re-embed all nodes with a new provider.
    Run this when deliberately switching embedding models.
    Updates dim metadata on completion.

    Usage:
        from research_agent.hebbian.embeddings import get_provider
        from research_agent.hebbian.migrations import re_embed

        new_provider = get_provider("qwen3")
        re_embed(graph, new_provider)
    """
    conn = graph._conn
    nodes = conn.execute("SELECT id, content FROM nodes").fetchall()
    total = len(nodes)

    print(
        f"Re-embedding {total} nodes with {new_provider.__class__.__name__} "
        f"({new_provider.dim} dims)..."
    )

    for i in range(0, total, batch_size):
        batch = nodes[i : i + batch_size]
        texts = [row[1] for row in batch]
        ids = [row[0] for row in batch]

        embeddings = new_provider.embed_batch(texts)

        for node_id, vec in zip(ids, embeddings):
            conn.execute(
                "UPDATE nodes SET embedding = ? WHERE id = ?", (vec.tobytes(), node_id)
            )

        done = min(i + batch_size, total)
        print(f"  {done}/{total} ({100 * done // total}%)")

    conn.execute(
        "UPDATE graph_metadata SET value = ? WHERE key = 'embedding_dim'",
        (str(new_provider.dim),),
    )
    conn.commit()
    print("Done.")
