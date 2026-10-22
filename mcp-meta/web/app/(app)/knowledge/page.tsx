"use client";

import { useState, useEffect, useRef, useCallback } from "react";
import * as d3 from "d3";
import { authHeaders } from "@/lib/api";
import { useI18n, type Key } from "@/lib/i18n";
import { EmptyState, ErrorState, Loading, resolveState } from "@/components/ui/states";

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

/** Node types, in the order the legend shows them. */
const TYPE_LABELS: Record<string, Key> = {
  fact: "knowledge.type.fact",
  finding: "knowledge.type.finding",
  episode: "knowledge.type.episode",
  concept: "knowledge.type.concept",
};

export default function KnowledgePage() {
  const { t } = useI18n();
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

  // Exactly one state, decided in one place.
  //
  // Before this the page could show three at once: the subtitle read
  // `graphData ? ... : "Loading..."`, which never leaves "Loading..." when the
  // request fails because `graphData` stays null; the error box rendered on
  // its own condition; and the 500px canvas rendered unconditionally
  // underneath both. A visitor saw a graph that was loading, had failed, and
  // was empty, all at the same time.
  //
  // The request itself is untouched. `unauthorized: missing API key` still
  // happens — it is kept, as the machine's own words, under a sentence a
  // person can read.
  const state = resolveState({
    loading,
    error,
    empty: !!graphData && graphData.nodes.length === 0,
  });

  return (
    <div className="page">
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          flexWrap: "wrap",
          gap: 12,
          marginBottom: 16,
        }}
      >
        <div>
          <h1 className="t-h2" style={{ marginBottom: 4 }}>{t("knowledge.title")}</h1>
          <p className="t-small" style={{ margin: 0 }}>
            {state === "ready" && graphData
              ? t("knowledge.subtitle", {
                  nodes: graphData.total_nodes,
                  edges: graphData.total_edges,
                })
              : state === "loading"
                ? t("common.loading")
                : "\u00a0"}
          </p>
        </div>
        <button className="btn-ghost btn-sm btn-inline" onClick={fetchGraph} disabled={loading}>
          {t("knowledge.refresh")}
        </button>
      </div>

      {/* The legend describes the canvas, so it only belongs on screen when
          there is a canvas. It used to sit above an error box explaining the
          colours of a graph that had failed to load. */}
      {state === "ready" && (
        <div style={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: 14, marginBottom: 16 }}>
          {Object.entries(NODE_COLORS).map(([type, color]) => (
            <div key={type} className="t-small" style={{ display: "flex", alignItems: "center", gap: 6, margin: 0 }}>
              <span style={{ width: 10, height: 10, borderRadius: "50%", backgroundColor: color }} aria-hidden="true" />
              {TYPE_LABELS[type] ? t(TYPE_LABELS[type]) : type}
            </div>
          ))}
          <div className="t-small" style={{ marginLeft: "auto", color: "var(--dim)", margin: 0 }}>
            {t("knowledge.legendNote")}
          </div>
        </div>
      )}

      {state === "loading" && <Loading title={t("knowledge.loading")} />}

      {state === "error" && (
        <ErrorState
          title={t("knowledge.error")}
          detail={error}
          onRetry={fetchGraph}
        />
      )}

      {state === "empty" && (
        <EmptyState title={t("knowledge.empty")} hint={t("knowledge.emptyHint")} />
      )}

      {/* The canvas, only when there is something to draw on it. A 500px
          empty rectangle is not a graph; it is a hole in the page. */}
      {state === "ready" && (
        <div
          className="card"
          style={{ padding: 0, overflow: "hidden", height: "clamp(320px, 60vh, 520px)" }}
        >
          <svg ref={svgRef} width="100%" height="100%" />
        </div>
      )}

      {state === "ready" && selectedNode && (
        <div className="card" style={{ marginTop: 16 }}>
          <div style={{ display: "flex", alignItems: "flex-start", justifyContent: "space-between", gap: 12, marginBottom: 8 }}>
            <h2 className="t-h3 t-wrap" style={{ margin: 0 }}>{selectedNode.label}</h2>
            <button
              onClick={() => setSelectedNode(null)}
              aria-label={t("common.close")}
              style={{ background: "none", border: "none", color: "var(--dim)", cursor: "pointer", fontSize: 14, flexShrink: 0 }}
            >
              ✕
            </button>
          </div>
          <p className="t-small t-wrap" style={{ marginBottom: 10 }}>{selectedNode.content}</p>
          <div style={{ display: "flex", flexWrap: "wrap", gap: 14 }}>
            <span className="t-small" style={{ color: "var(--dim)", margin: 0 }}>
              {t("knowledge.nodeType")}: {TYPE_LABELS[selectedNode.type] ? t(TYPE_LABELS[selectedNode.type]) : selectedNode.type}
            </span>
            <span className="t-small" style={{ color: "var(--dim)", margin: 0 }}>
              {t("knowledge.nodeImportance")}: {selectedNode.importance}
            </span>
            <span className="t-small" style={{ color: "var(--dim)", margin: 0 }}>
              {t("knowledge.nodeAccessed")}: {t("knowledge.nodeAccessedTimes", { n: selectedNode.access_count })}
            </span>
          </div>
        </div>
      )}
    </div>
  );
}
