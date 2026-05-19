import { interpolate, spring, useVideoConfig } from "remotion";
import { C } from "./constants";

const TX_HASH = "7f3a9b2c...e81d";

export function Scene3Escrow({ sceneFrame }: { frame: number; sceneFrame: number }) {
  const { fps } = useVideoConfig();

  const cardScale = spring({ frame: sceneFrame, fps, config: { damping: 12, mass: 0.8 } });
  const cardOpacity = interpolate(cardScale, [0, 1], [0, 1]);

  // Details populate
  const detail1 = interpolate(sceneFrame, [15, 22], [0, 1], { extrapolateLeft: "clamp", extrapolateRight: "clamp" });
  const detail2 = interpolate(sceneFrame, [22, 29], [0, 1], { extrapolateLeft: "clamp", extrapolateRight: "clamp" });
  const detail3 = interpolate(sceneFrame, [29, 36], [0, 1], { extrapolateLeft: "clamp", extrapolateRight: "clamp" });

  // Button press
  const btnPressed = sceneFrame >= 50 && sceneFrame <= 55;
  const btnDone = sceneFrame > 55;

  // Lock animation
  const lockProgress = interpolate(sceneFrame, [58, 80], [0, 1], { extrapolateLeft: "clamp", extrapolateRight: "clamp" });
  const lockBounce = sceneFrame > 58 ? spring({ frame: sceneFrame - 58, fps, config: { damping: 8, mass: 0.6 } }) : 0;

  // Badge
  const badgeOpacity = interpolate(sceneFrame, [80, 88], [0, 1], { extrapolateLeft: "clamp", extrapolateRight: "clamp" });

  // TX hash typing
  const txChars = Math.floor(interpolate(sceneFrame, [90, 115], [0, TX_HASH.length], { extrapolateLeft: "clamp", extrapolateRight: "clamp" }));

  return (
    <div style={{ display: "flex", flexDirection: "column", alignItems: "center", justifyContent: "center", height: "100%" }}>
      <div
        style={{
          width: 440,
          background: C.surface,
          border: `1px solid ${C.border}`,
          borderRadius: 10,
          padding: 28,
          opacity: cardOpacity,
          transform: `scale(${interpolate(cardScale, [0, 1], [0.9, 1])})`,
        }}
      >
        <div style={{ fontSize: 11, fontFamily: "'Courier New', monospace", color: C.dim, textTransform: "uppercase", letterSpacing: 1, marginBottom: 20 }}>
          // escrow details
        </div>

        {/* Details */}
        <div style={{ display: "flex", flexDirection: "column", gap: 12, marginBottom: 20 }}>
          <div style={{ opacity: detail1, display: "flex", justifyContent: "space-between", fontSize: 12 }}>
            <span style={{ color: C.dim }}>Agent</span>
            <span style={{ color: C.accent, fontWeight: 600, fontFamily: "'Courier New', monospace" }}>deep-analyst</span>
          </div>
          <div style={{ opacity: detail2, display: "flex", justifyContent: "space-between", fontSize: 12 }}>
            <span style={{ color: C.dim }}>Amount</span>
            <span style={{ color: C.text, fontWeight: 600, fontFamily: "'Courier New', monospace" }}>
              0.22 <span style={{ color: C.muted, fontSize: 10 }}>USDC</span>
            </span>
          </div>
          <div style={{ opacity: detail3, display: "flex", justifyContent: "space-between", fontSize: 12 }}>
            <span style={{ color: C.dim }}>Deadline</span>
            <span style={{ color: C.muted, fontFamily: "'Courier New', monospace" }}>5 min</span>
          </div>
        </div>

        {/* Divider */}
        <div style={{ height: 1, background: C.border, marginBottom: 20 }} />

        {/* Lock animation area */}
        <div style={{ display: "flex", flexDirection: "column", alignItems: "center", gap: 12, marginBottom: 16 }}>
          {/* Lock icon */}
          {lockProgress > 0 && (
            <div style={{ transform: `scale(${lockBounce})`, display: "flex", flexDirection: "column", alignItems: "center", gap: 8 }}>
              <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke={C.amber} strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
                <rect x="3" y="11" width="18" height="11" rx="2" />
                <path d={lockProgress > 0.5 ? "M7 11V7a5 5 0 0 1 10 0v4" : "M7 11V7a5 5 0 0 1 10 0v4"} />
              </svg>

              {/* LOCKED badge */}
              <div
                style={{
                  opacity: badgeOpacity,
                  display: "flex",
                  alignItems: "center",
                  gap: 6,
                  padding: "4px 12px",
                  background: `${C.amber}15`,
                  border: `1px solid ${C.amber}40`,
                  borderRadius: 12,
                }}
              >
                <span style={{ fontSize: 10, fontWeight: 600, fontFamily: "'Courier New', monospace", color: C.amber }}>
                  0.22 USDC
                </span>
                <span style={{ fontSize: 9, color: C.amber }}>LOCKED</span>
              </div>
            </div>
          )}

          {/* TX hash */}
          {txChars > 0 && (
            <div style={{ fontSize: 10, fontFamily: "'Courier New', monospace", color: C.dim }}>
              tx: {TX_HASH.slice(0, txChars)}
              {txChars < TX_HASH.length && <span style={{ color: C.accent }}>|</span>}
            </div>
          )}
        </div>

        {/* Button */}
        <div
          style={{
            background: btnDone ? C.surface2 : btnPressed ? C.accentDim : C.accent,
            borderRadius: 6,
            padding: "10px 0",
            textAlign: "center",
            fontSize: 12,
            fontWeight: 600,
            color: btnDone ? C.dim : "#fff",
            transform: btnPressed ? "scale(0.97)" : "scale(1)",
          }}
        >
          {btnDone ? "Approved ✓" : "Approve & Lock"}
        </div>
      </div>
    </div>
  );
}
