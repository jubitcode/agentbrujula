import time

from .knowledge_graph import KnowledgeGraph, GraphNode


class WorkingMemoryBuffer:
    def __init__(self, graph: KnowledgeGraph, token_limit: int = 3000):
        self.graph = graph
        self.token_limit = token_limit
        self.active_nodes: list[str] = []
        self.pinned_nodes: set[str] = set()
        self.dag_inputs: set[str] = set()
        self.evicted_this_session: list[str] = []

    @staticmethod
    def _estimate_tokens(node: GraphNode) -> int:
        text = node.content or node.summary or node.label
        return max(1, len(text) // 4)

    def _total_tokens(self) -> int:
        total = 0
        for nid in self.active_nodes:
            node = self.graph.get_node(nid)
            if node:
                total += self._estimate_tokens(node)
        return total

    def pull(
        self, node_ids: list[str], dag_inputs: list[str] | None = None
    ) -> list[GraphNode]:
        if dag_inputs:
            self.dag_inputs.update(dag_inputs)

        now = time.time()
        pulled = []
        for nid in node_ids:
            node = self.graph.get_node(nid)
            if not node:
                continue
            node.access_count += 1
            node.last_accessed = now
            if nid not in self.active_nodes:
                self.active_nodes.append(nid)
            if node.pinned:
                self.pinned_nodes.add(nid)
            pulled.append(node)

        while self._total_tokens() > self.token_limit:
            evictable = self._get_evictable()
            if not evictable:
                break
            victim = evictable[0]
            self.active_nodes.remove(victim)
            self.evicted_this_session.append(victim)

        return pulled

    def _get_evictable(self) -> list[str]:
        candidates = []
        for nid in self.active_nodes:
            if nid in self.dag_inputs:
                continue
            if nid in self.pinned_nodes:
                continue
            node = self.graph.get_node(nid)
            if node:
                candidates.append((nid, node.importance, node.last_accessed))
        candidates.sort(key=lambda x: (x[1], x[2]))
        return [c[0] for c in candidates]

    def evict(self, n: int = 1) -> list[str]:
        evictable = self._get_evictable()
        evicted = []
        for nid in evictable[:n]:
            self.active_nodes.remove(nid)
            self.evicted_this_session.append(nid)
            evicted.append(nid)
        return evicted

    def pin(self, node_ids: list[str]):
        self.pinned_nodes.update(node_ids)

    def unpin(self, node_ids: list[str]):
        self.pinned_nodes -= set(node_ids)

    def get_state(self) -> dict:
        return {
            "active_nodes": list(self.active_nodes),
            "pinned_nodes": list(self.pinned_nodes),
            "buffer_tokens_used": self._total_tokens(),
            "buffer_tokens_limit": self.token_limit,
            "evicted_this_session": list(self.evicted_this_session),
        }

    def get_active_content(self) -> str:
        parts = []
        for nid in self.active_nodes:
            node = self.graph.get_node(nid)
            if node:
                parts.append(f"[{node.id}] {node.content or node.summary}")
        return "\n".join(parts)
