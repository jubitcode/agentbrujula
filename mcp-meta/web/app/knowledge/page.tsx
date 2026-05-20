"use client";

import { useState, useEffect, useRef, useCallback } from "react";
import * as d3 from "d3";
import { authHeaders } from "@/lib/api";

const API = process.env.NEXT_PUBLIC_API_URL || "http://localhost:5050";

interface GraphNode {
  id: string;
  label: string;
  content: string;
  type: string;
  importance: number;
  access_count: number;
  // d3 simulation fields
  x?: number;
  y?: number;
  fx?: number | null;
  fy?: number | null;
}

interface GraphEdge {
  source: string | GraphNode;
  target: string | GraphNode;
  weight: number;
  relation: string;
  co_activations: number;
}

interface GraphData {
  nodes: GraphNode[];
  edges: GraphEdge[];
  total_nodes: number;
  total_edges: number;
}

const NODE_COLORS: Record<string, string> = {
  fact: "#3b82f6",
  finding: "#10b981",
  episode: "#f59e0b",
  concept: "#8b5cf6",
};

export default function KnowledgePage() {
  const svgRef = useRef<SVGSVGElement>(null);
  const [graphData, setGraphData] = useState<GraphData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [selectedNode, setSelectedNode] = useState<GraphNode | null>(null);

  const fetchGraph = useCallback(async () => {
    setLoading(true);
    try {
      const resp = await fetch(`${API}/v1/execute`, {
        method: "POST",
        headers: authHeaders({ "Content-Type": "application/json" }),
        body: JSON.stringify({ tool: "hebbian.graph", arguments: {} }),
      });
      const data = await resp.json();
      if (data.error) {
        setError(data.error);
      } else {
        setGraphData(data.result);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load graph");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchGraph();
  }, [fetchGraph]);

  useEffect(() => {
    if (!graphData || !svgRef.current || graphData.nodes.length === 0) return;

    const svg = d3.select(svgRef.current);
    const width = svgRef.current.clientWidth;
    const height = svgRef.current.clientHeight;

    svg.selectAll("*").remove();

    const g = svg.append("g");

    // Zoom
    const zoom = d3.zoom<SVGSVGElement, unknown>()
      .scaleExtent([0.3, 4])
      .on("zoom", (event) => g.attr("transform", event.transform));
    svg.call(zoom);

    // Simulation
    const nodes = graphData.nodes.map((d) => ({ ...d }));
    const edges = graphData.edges.map((d) => ({ ...d }));

    const simulation = d3
      .forceSimulation(nodes)
      .force("link", d3.forceLink<GraphNode, GraphEdge>(edges)
        .id((d) => d.id)
        .distance(100)
        .strength((d) => Math.min(d.weight * 2, 1))
      )
      .force("charge", d3.forceManyBody().strength(-200))
      .force("center", d3.forceCenter(width / 2, height / 2))
      .force("collision", d3.forceCollide().radius(30));

    // Edges
    const link = g
      .append("g")
      .selectAll("line")
      .data(edges)
      .join("line")
      .attr("stroke", "var(--border2)")
      .attr("stroke-width", (d) => Math.max(1, d.weight * 4))
      .attr("stroke-opacity", (d) => 0.3 + d.weight * 0.5);

    // Edge labels
    const linkLabel = g
      .append("g")
      .selectAll("text")
      .data(edges.filter((d) => d.weight > 0.3))
      .join("text")
      .text((d) => d.relation)
      .attr("font-size", "8px")
      .attr("fill", "var(--dim)")
      .attr("text-anchor", "middle");

    // Nodes
    const node = g
      .append("g")
      .selectAll("circle")
      .data(nodes)
      .join("circle")
      .attr("r", (d) => 8 + d.importance * 12)
      .attr("fill", (d) => NODE_COLORS[d.type] || "#6b7280")
      .attr("stroke", "var(--border)")
      .attr("stroke-width", 2)
      .attr("cursor", "pointer")
      .on("click", (_, d) => setSelectedNode(d))
      .call(
        d3.drag<any, GraphNode>()
          .on("start", (event, d) => {
            if (!event.active) simulation.alphaTarget(0.3).restart();
            d.fx = d.x;
            d.fy = d.y;
          })
          .on("drag", (event, d) => {
            d.fx = event.x;
            d.fy = event.y;
          })
          .on("end", (event, d) => {
            if (!event.active) simulation.alphaTarget(0);
            d.fx = null;
            d.fy = null;
          })
      );

    // Node labels
    const label = g
      .append("g")
      .selectAll("text")
      .data(nodes)
      .join("text")
      .text((d) => d.label.length > 25 ? d.label.slice(0, 25) + "…" : d.label)
      .attr("font-size", "10px")
      .attr("fill", "var(--text)")
      .attr("dx", 15)
      .attr("dy", 4)
      .attr("pointer-events", "none");

    simulation.on("tick", () => {
      link
        .attr("x1", (d) => (d.source as GraphNode).x || 0)
        .attr("y1", (d) => (d.source as GraphNode).y || 0)
        .attr("x2", (d) => (d.target as GraphNode).x || 0)
        .attr("y2", (d) => (d.target as GraphNode).y || 0);

      linkLabel
        .attr("x", (d) => (((d.source as GraphNode).x || 0) + ((d.target as GraphNode).x || 0)) / 2)
        .attr("y", (d) => (((d.source as GraphNode).y || 0) + ((d.target as GraphNode).y || 0)) / 2);

      node.attr("cx", (d) => d.x || 0).attr("cy", (d) => d.y || 0);
      label.attr("x", (d) => d.x || 0).attr("y", (d) => d.y || 0);
    });

    return () => { simulation.stop(); };
  }, [graphData]);

  return (
    <div className="max-w-6xl mx-auto">
      <div className="flex items-center justify-between mb-4">
        <div>
          <h1 className="text-xl font-medium" style={{ color: "var(--text)" }}>Knowledge Graph</h1>
          <p className="text-sm" style={{ color: "var(--dim)" }}>
            {graphData
              ? `${graphData.total_nodes} nodes · ${graphData.total_edges} edges`
              : "Loading..."}
          </p>
        </div>
        <button
          onClick={fetchGraph}
          className="text-xs border rounded-lg px-3 py-2 transition-colors"
          style={{ color: "var(--muted)", borderColor: "var(--border)" }}
        >
          Refresh
        </button>
      </div>

      {/* Legend */}
      <div className="flex gap-4 mb-4">
        {Object.entries(NODE_COLORS).map(([type, color]) => (
          <div key={type} className="flex items-center gap-1 text-xs" style={{ color: "var(--muted)" }}>
            <span className="w-3 h-3 rounded-full" style={{ backgroundColor: color }} />
            {type}
          </div>
        ))}
        <div className="text-xs ml-auto" style={{ color: "var(--dim)" }}>
          Node size = importance · Edge thickness = Hebbian weight
        </div>
      </div>

      {error && (
        <div className="rounded-xl p-4 text-sm mb-4" style={{ background: "rgba(239,68,68,0.08)", borderColor: "rgba(239,68,68,0.2)", color: "#ef4444" }}>
          {error}
        </div>
      )}

      {loading && !graphData && (
        <div className="border rounded-xl p-12 text-center" style={{ background: "var(--surface)", borderColor: "var(--border)", color: "var(--dim)" }}>
          Loading knowledge graph...
        </div>
      )}

      {graphData && graphData.nodes.length === 0 && (
        <div className="border rounded-xl p-12 text-center" style={{ background: "var(--surface)", borderColor: "var(--border)", color: "var(--dim)" }}>
          No knowledge yet. Use the research agent or store findings to build the graph.
        </div>
      )}

      {/* Graph */}
      <div className="border rounded-xl overflow-hidden" style={{ height: "500px", background: "var(--surface)", borderColor: "var(--border)" }}>
        <svg ref={svgRef} width="100%" height="100%" />
      </div>

      {/* Selected node detail */}
      {selectedNode && (
        <div className="mt-4 border rounded-xl p-4" style={{ background: "var(--surface)", borderColor: "var(--border)" }}>
          <div className="flex items-center justify-between mb-2">
            <h3 className="font-medium" style={{ color: "var(--text)" }}>{selectedNode.label}</h3>
            <button
              onClick={() => setSelectedNode(null)}
              className="text-xs"
              style={{ color: "var(--dim)" }}
            >
              ✕
            </button>
          </div>
          <p className="text-sm mb-2" style={{ color: "var(--muted)" }}>{selectedNode.content}</p>
          <div className="flex gap-4 text-xs" style={{ color: "var(--dim)" }}>
            <span>Type: {selectedNode.type}</span>
            <span>Importance: {selectedNode.importance}</span>
            <span>Accessed: {selectedNode.access_count}x</span>
          </div>
        </div>
      )}
    </div>
  );
}
