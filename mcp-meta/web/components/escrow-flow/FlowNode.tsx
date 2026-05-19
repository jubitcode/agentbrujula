import { interpolate, spring, useCurrentFrame, useVideoConfig } from "remotion";
import { COLORS, type FlowNode as FlowNodeType } from "./constants";

const ICONS: Record<string, (color: string) => React.ReactNode> = {
  user: (c) => (
    <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke={c} strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
      <circle cx="12" cy="8" r="4" />
      <path d="M4 21v-1a6 6 0 0 1 6-6h4a6 6 0 0 1 6 6v1" />
    </svg>
  ),
  escrow: (c) => (
    <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke={c} strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
      <rect x="3" y="11" width="18" height="11" rx="2" />
      <path d="M7 11V7a5 5 0 0 1 10 0v4" />
    </svg>
  ),
  agent: (c) => (
    <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke={c} strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
      <rect x="4" y="4" width="16" height="16" rx="2" />
      <circle cx="9" cy="11" r="1.5" fill={c} stroke="none" />
      <circle cx="15" cy="11" r="1.5" fill={c} stroke="none" />
      <path d="M9 16h6" />
      <path d="M8 1v3M16 1v3" />
    </svg>
  ),
  verify: (c) => (
    <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke={c} strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
      <path d="M12 2L3 7v5c0 5.25 3.75 10.15 9 11.25C17.25 22.15 21 17.25 21 12V7l-9-5z" />
      <path d="M9 12l2 2 4-4" />
    </svg>
  ),
  settle: (c) => (
    <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke={c} strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
      <circle cx="12" cy="12" r="10" />
      <path d="M12 6v2M12 16v2" />
      <path d="M15 9.5c0-1.38-1.34-2.5-3-2.5s-3 1.12-3 2.5 1.34 2.5 3 2.5 3 1.12 3 2.5-1.34 2.5-3 2.5" />
    </svg>
  ),
};

export function FlowNode({ node }: { node: FlowNodeType }) {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();

  const appear = spring({ frame: frame - node.enterFrame, fps, config: { damping: 12, mass: 0.8 } });
  const scale = interpolate(appear, [0, 1], [0.3, 1]);
  const opacity = interpolate(appear, [0, 1], [0, 1]);

  const glowPulse = frame > node.enterFrame + 20
    ? 0.3 + 0.15 * Math.sin((frame - node.enterFrame) * 0.08)
    : 0;

  return (
    <div
      style={{
        position: "absolute",
        left: node.x - 48,
        top: node.y - 48,
        width: 96,
        height: 96,
        transform: `scale(${scale})`,
        opacity,
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
      }}
    >
      {/* Glow */}
      <div
        style={{
          position: "absolute",
          width: 72,
          height: 72,
          borderRadius: "50%",
          background: node.color,
          opacity: glowPulse,
          filter: "blur(18px)",
          top: -4,
        }}
      />

      {/* Circle */}
      <div
        style={{
          width: 56,
          height: 56,
          borderRadius: "50%",
          background: COLORS.surface,
          border: `2px solid ${node.color}`,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          position: "relative",
          zIndex: 1,
        }}
      >
        {ICONS[node.id]?.(node.color)}
      </div>

      {/* Label */}
      <div
        style={{
          marginTop: 8,
          textAlign: "center",
          position: "relative",
          zIndex: 1,
        }}
      >
        <div style={{ fontSize: 12, fontWeight: 600, color: COLORS.text, letterSpacing: 0.3 }}>
          {node.label}
        </div>
        <div style={{ fontSize: 9, color: COLORS.dim, fontFamily: "'Courier New', monospace", marginTop: 2 }}>
          {node.sublabel}
        </div>
      </div>
    </div>
  );
}
