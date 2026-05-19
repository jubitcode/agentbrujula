import { interpolate, spring, useCurrentFrame, useVideoConfig } from "remotion";
import { COLORS, NODES } from "./constants";

interface FlowEdgeProps {
  fromIdx: number;
  toIdx: number;
  startFrame: number;
  label: string;
}

export function FlowEdge({ fromIdx, toIdx, startFrame, label }: FlowEdgeProps) {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();

  const progress = spring({
    frame: frame - startFrame,
    fps,
    config: { damping: 14, mass: 1.2 },
  });

  if (frame < startFrame) return null;

  const from = NODES[fromIdx];
  const to = NODES[toIdx];
  const x1 = from.x + 28;
  const x2 = to.x - 28;
  const y = from.y;

  const lineLength = x2 - x1;
  const currentLength = interpolate(progress, [0, 1], [0, lineLength]);

  const labelOpacity = interpolate(progress, [0.5, 1], [0, 1], { extrapolateLeft: "clamp", extrapolateRight: "clamp" });

  // Particle dot
  const dotX = x1 + currentLength;
  const dotOpacity = progress > 0.05 && progress < 0.95 ? 1 : 0;

  return (
    <>
      {/* Line */}
      <svg
        style={{ position: "absolute", top: 0, left: 0, width: "100%", height: "100%", pointerEvents: "none" }}
      >
        <line
          x1={x1}
          y1={y}
          x2={x1 + currentLength}
          y2={y}
          stroke={COLORS.border}
          strokeWidth={1.5}
          strokeDasharray="4 3"
        />
        {/* Arrow head */}
        {progress > 0.9 && (
          <polygon
            points={`${x2},${y} ${x2 - 6},${y - 4} ${x2 - 6},${y + 4}`}
            fill={COLORS.muted}
            opacity={interpolate(progress, [0.9, 1], [0, 1], { extrapolateLeft: "clamp", extrapolateRight: "clamp" })}
          />
        )}
      </svg>

      {/* Traveling dot */}
      <div
        style={{
          position: "absolute",
          left: dotX - 3,
          top: y - 3,
          width: 6,
          height: 6,
          borderRadius: "50%",
          background: COLORS.accent,
          opacity: dotOpacity,
          boxShadow: `0 0 8px ${COLORS.accent}`,
        }}
      />

      {/* Label */}
      <div
        style={{
          position: "absolute",
          left: (x1 + x2) / 2,
          top: y - 22,
          transform: "translateX(-50%)",
          fontSize: 8,
          fontFamily: "'Courier New', monospace",
          color: COLORS.dim,
          opacity: labelOpacity,
          whiteSpace: "nowrap",
          letterSpacing: 0.5,
        }}
      >
        {label}
      </div>
    </>
  );
}
