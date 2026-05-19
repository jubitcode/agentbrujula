from collections import defaultdict

from .knowledge_graph import KnowledgeGraph


class ManifestGenerator:
    def __init__(self, graph: KnowledgeGraph, max_tokens: int = 800):
        self.graph = graph
        self.max_tokens = max_tokens

    def cluster_nodes(self) -> dict[str, list[str]]:
        domain_groups: dict[str, list[str]] = defaultdict(list)
        no_domain: list[str] = []

        for nid, node in self.graph.nodes.items():
            domain = node.metadata.get("domain", "")
            if domain:
                domain_groups[domain].append(nid)
            else:
                no_domain.append(nid)

        parent: dict[str, str] = {}
        domain_of: dict[str, str] = {}
        for domain, members in domain_groups.items():
            parent[domain] = domain
            for nid in members:
                domain_of[nid] = domain

        def find(x: str) -> str:
            while parent.get(x, x) != x:
                parent[x] = parent.get(parent[x], parent[x])
                x = parent[x]
            return x

        def union(a: str, b: str):
            ra, rb = find(a), find(b)
            if ra != rb:
                if len(domain_groups.get(ra, [])) >= len(domain_groups.get(rb, [])):
                    parent[rb] = ra
                else:
                    parent[ra] = rb

        for edge in self.graph.edges:
            if edge.weight >= 0.6:
                d1 = domain_of.get(edge.source_id)
                d2 = domain_of.get(edge.target_id)
                if d1 and d2 and d1 != d2:
                    union(d1, d2)

        clusters: dict[str, list[str]] = defaultdict(list)
        for domain, members in domain_groups.items():
            root = find(domain)
            clusters[root].extend(members)

        for nid in no_domain:
            node = self.graph.get_node(nid)
            label = node.node_type if node else "other"
            clusters[label].append(nid)

        named: dict[str, list[str]] = {}
        for cluster_id, members in clusters.items():
            base_id = cluster_id
            final_id = base_id
            counter = 1
            while final_id in named:
                final_id = f"{base_id}_{counter}"
                counter += 1
            named[final_id] = members

        return named

    def generate(self) -> str:
        clusters = self.cluster_nodes()

        lines_with_meta = []
        for cluster_id, members in clusters.items():
            summaries = []
            total_importance = 0.0
            for nid in members:
                node = self.graph.get_node(nid)
                if node:
                    summaries.append(node.summary or node.label)
                    total_importance += node.importance
            avg_importance = total_importance / len(members) if members else 0.0
            summary_text = ", ".join(summaries[:5])
            if len(members) > 5:
                summary_text += f", ... +{len(members) - 5} more"
            line = f"[{cluster_id}] {summary_text} | {len(members)} nodes | {avg_importance:.2f}"
            lines_with_meta.append((line, avg_importance))

        lines_with_meta.sort(key=lambda x: x[1], reverse=True)

        result_lines = []
        total_chars = 0
        char_budget = self.max_tokens * 4

        for line, _ in lines_with_meta:
            if total_chars + len(line) + 1 > char_budget:
                break
            result_lines.append(line)
            total_chars += len(line) + 1

        return "\n".join(result_lines)

    def get_cluster_ids(self) -> list[str]:
        """Return sorted list of cluster IDs."""
        return sorted(self.cluster_nodes().keys())

    def expand_cluster(self, cluster_id: str) -> list[str]:
        """Return node IDs belonging to a cluster."""
        clusters = self.cluster_nodes()
        return clusters.get(cluster_id, [])
