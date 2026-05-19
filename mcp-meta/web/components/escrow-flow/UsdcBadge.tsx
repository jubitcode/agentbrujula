import { interpolate, spring, useCurrentFrame, useVideoConfig } from "remotion";
import { COLORS } from "./constants";

interface UsdcBadgeProps {
  enterFrame: number;
  x: number;
  y: number;
  amount: string;
  status: "locked" | "released";
}

export function UsdcBadge({ enterFrame, x, y, amount, status }: UsdcBadgeProps) {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();

  const appear = spring({ frame: frame - enterFrame, fps, config: { damping: 10 } });
  if (frame < enterFrame) return null;

  const scale = interpolate(appear, [0, 1], [0.5, 1]);
  const opacity = interpolate(appear, [0, 1], [0, 1]);
  const color = status === "locked" ? COLORS.amber : COLORS.green;

  return (
    <div
      style={{
        position: "absolute",
        left: x,
        top: y,
        transform: `translateX(-50%) scale(${scale})`,
        opacity,
        display: "flex",
        alignItems: "center",
        gap: 4,
        padding: "3px 8px",
        background: `${color}15`,
        border: `1px solid ${color}40`,
        borderRadius: 12,
        whiteSpace: "nowrap",
      }}
    >
      <span style={{ fontSize: 9, fontWeight: 600, fontFamily: "'Courier New', monospace", color }}>
        {amount} USDC
      </span>
      <span style={{ fontSize: 8, color: COLORS.dim }}>
        {status === "locked" ? "LOCKED" : "RELEASED"}
      </span>
    </div>
  );
}
