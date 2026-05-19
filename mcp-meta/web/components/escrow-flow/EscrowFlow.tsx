import { AbsoluteFill, useCurrentFrame } from "remotion";
import { COLORS, NODES, EDGES, DURATION_IN_FRAMES } from "./constants";
import { FlowNode } from "./FlowNode";
import { FlowEdge } from "./FlowEdge";
import { UsdcBadge } from "./UsdcBadge";

export function EscrowFlow() {
  const frame = useCurrentFrame();

  // Subtle grid pattern opacity
  const gridOpacity = 0.04 + 0.01 * Math.sin(frame * 0.02);

  return (
    <AbsoluteFill
      style={{
        background: COLORS.bg,
        fontFamily: "-apple-system, BlinkMacSystemFont, 'Helvetica Neue', sans-serif",
        overflow: "hidden",
      }}
    >
      {/* Subtle grid background */}
      <svg style={{ position: "absolute", width: "100%", height: "100%", opacity: gridOpacity }}>
        <defs>
          <pattern id="grid" width="40" height="40" patternUnits="userSpaceOnUse">
            <path d="M 40 0 L 0 0 0 40" fill="none" stroke={COLORS.muted} strokeWidth="0.5" />
          </pattern>
        </defs>
        <rect width="100%" height="100%" fill="url(#grid)" />
      </svg>

      {/* Title */}
      <div
        style={{
          position: "absolute",
          top: 40,
          left: 0,
          width: "100%",
          textAlign: "center",
          fontFamily: "'Courier New', monospace",
          fontSize: 10,
          textTransform: "uppercase",
          letterSpacing: 2,
          color: COLORS.dim,
        }}
      >
        {"// usdc lifecycle"}
      </div>

      {/* Flow container - centered */}
      <div style={{ position: "absolute", left: "50%", top: "50%", transform: "translate(-50%, -50%)", width: 960, height: 300 }}>
        {/* Edges */}
        {EDGES.map((e, i) => (
          <FlowEdge key={i} fromIdx={e.from} toIdx={e.to} startFrame={e.startFrame} label={e.label} />
        ))}

        {/* Nodes */}
        {NODES.map((node) => (
          <FlowNode key={node.id} node={node} />
        ))}

        {/* USDC badges */}
        <UsdcBadge enterFrame={60} x={280} y={265} amount="0.25" status="locked" />
        <UsdcBadge enterFrame={235} x={880} y={265} amount="0.25" status="released" />
      </div>

      {/* Bottom status line */}
      <div
        style={{
          position: "absolute",
          bottom: 32,
          left: 0,
          width: "100%",
          textAlign: "center",
          fontFamily: "'Courier New', monospace",
          fontSize: 9,
          color: COLORS.dim,
          letterSpacing: 0.5,
        }}
      >
        {frame < 45 && "waiting for payment"}
        {frame >= 45 && frame < 100 && "0.10 USDC sits in the escrow contract"}
        {frame >= 100 && frame < 160 && "agent is running"}
        {frame >= 160 && frame < 220 && "compliance contract checks the output"}
        {frame >= 220 && "settled. agent paid 0.08, refund 0.02"}
      </div>
    </AbsoluteFill>
  );
}
