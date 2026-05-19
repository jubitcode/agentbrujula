import { interpolate, spring, useVideoConfig } from "remotion";
import { C } from "./constants";

export function Scene5Result({ sceneFrame }: { frame: number; sceneFrame: number }) {
  const { fps } = useVideoConfig();

  // Compliance check
  const complianceScale = spring({ frame: sceneFrame, fps, config: { damping: 10, mass: 0.7 } });
  const complianceOpacity = interpolate(complianceScale, [0, 1], [0, 1]);

  const ratioOpacity = interpolate(sceneFrame, [12, 18], [0, 1], { extrapolateLeft: "clamp", extrapolateRight: "clamp" });
  const passBadge = interpolate(sceneFrame, [22, 30], [0, 1], { extrapolateLeft: "clamp", extrapolateRight: "clamp" });

  // Settlement
  const settlementOpacity = interpolate(sceneFrame, [35, 42], [0, 1], { extrapolateLeft: "clamp", extrapolateRight: "clamp" });
  const txOpacity = interpolate(sceneFrame, [45, 55], [0, 1], { extrapolateLeft: "clamp", extrapolateRight: "clamp" });

  // Result preview
  const resultScale = sceneFrame > 60 ? spring({ frame: sceneFrame - 60, fps, config: { damping: 12, mass: 0.8 } }) : 0;
  const resultOpacity = interpolate(resultScale, [0, 1], [0, 1]);

  // Complete badge
  const completeBadge = interpolate(sceneFrame, [85, 92], [0, 1], { extrapolateLeft: "clamp", extrapolateRight: "clamp" });

  return (
    <div style={{ display: "flex", flexDirection: "column", alignItems: "center", justifyContent: "center", height: "100%", gap: 12 }}>
      {/* Compliance + Settlement row */}
      <div style={{ display: "flex", gap: 12, width: 600 }}>
        {/* Compliance card */}
        <div
          style={{
            flex: 1,
            background: C.surface,
            border: `1px solid ${C.border}`,
            borderRadius: 10,
            padding: 20,
            opacity: complianceOpacity,
            transform: `scale(${interpolate(complianceScale, [0, 1], [0.95, 1])})`,
          }}
        >
          <div style={{ fontSize: 9, fontFamily: "'Courier New', monospace", color: C.dim, textTransform: "uppercase", letterSpacing: 1, marginBottom: 12 }}>
            compliance check
          </div>

          <div style={{ opacity: ratioOpacity, display: "flex", flexDirection: "column", gap: 6, marginBottom: 10 }}>
            <div style={{ display: "flex", justifyContent: "space-between", fontSize: 11 }}>
              <span style={{ color: C.dim }}>Compression ratio</span>
              <span style={{ color: C.text, fontFamily: "'Courier New', monospace", fontWeight: 600 }}>145%</span>
            </div>
            <div style={{ display: "flex", justifyContent: "space-between", fontSize: 11 }}>
              <span style={{ color: C.dim }}>Max allowed</span>
              <span style={{ color: C.muted, fontFamily: "'Courier New', monospace" }}>200%</span>
            </div>

            {/* Progress bar for ratio */}
            <div style={{ height: 4, background: C.border, borderRadius: 2, marginTop: 4 }}>
              <div
                style={{
                  height: "100%",
                  width: "72.5%",
                  background: C.green,
                  borderRadius: 2,
                }}
              />
            </div>
          </div>

          {/* PASS badge */}
          <div style={{ opacity: passBadge, display: "flex", alignItems: "center", gap: 6 }}>
            <div
              style={{
                padding: "3px 10px",
                background: `${C.green}20`,
                border: `1px solid ${C.green}50`,
                borderRadius: 12,
                fontSize: 10,
                fontWeight: 700,
                fontFamily: "'Courier New', monospace",
                color: C.green,
              }}
            >
              PASS ✓
            </div>
          </div>
        </div>

        {/* Settlement card */}
        <div
          style={{
            flex: 1,
            background: C.surface,
            border: `1px solid ${C.border}`,
            borderRadius: 10,
            padding: 20,
            opacity: settlementOpacity,
          }}
        >
          <div style={{ fontSize: 9, fontFamily: "'Courier New', monospace", color: C.dim, textTransform: "uppercase", letterSpacing: 1, marginBottom: 12 }}>
            settlement
          </div>

          <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
            <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke={C.green} strokeWidth="2" strokeLinecap="round">
                <circle cx="12" cy="12" r="10" />
                <path d="M8 12l3 3 5-5" />
              </svg>
              <span style={{ fontSize: 11, color: C.text }}>Funds released</span>
            </div>

            <div style={{ display: "flex", justifyContent: "space-between", fontSize: 11 }}>
              <span style={{ color: C.dim }}>Paid</span>
              <span style={{ color: C.green, fontWeight: 600, fontFamily: "'Courier New', monospace" }}>
                0.22 USDC
              </span>
            </div>

            <div style={{ opacity: txOpacity, fontSize: 10, fontFamily: "'Courier New', monospace", color: C.dim, marginTop: 4 }}>
              settle_tx: a2f8c1...9d3e
            </div>
          </div>
        </div>
      </div>

      {/* Result preview */}
      <div
        style={{
          width: 600,
          background: C.surface,
          border: `1px solid ${C.border}`,
          borderRadius: 10,
          padding: 20,
          opacity: resultOpacity,
          transform: `scale(${interpolate(resultScale, [0, 1], [0.95, 1])})`,
        }}
      >
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 12 }}>
          <span style={{ fontSize: 9, fontFamily: "'Courier New', monospace", color: C.dim, textTransform: "uppercase", letterSpacing: 1 }}>
            result
          </span>
          <span style={{ fontSize: 10, fontFamily: "'Courier New', monospace", color: C.muted }}>
            847 tokens
          </span>
        </div>

        <div style={{ fontSize: 12, color: C.text, lineHeight: 1.6, marginBottom: 8 }}>
          <span style={{ fontWeight: 600 }}>DeFi Yield Farming: 2026 Landscape</span>
        </div>
        <div style={{ fontSize: 11, color: C.muted, lineHeight: 1.5 }}>
          The DeFi yield farming ecosystem has consolidated around three major strategies: liquid staking derivatives, real-world asset tokenization, and cross-chain aggregation...
        </div>

        {/* Complete badge */}
        <div
          style={{
            opacity: completeBadge,
            marginTop: 14,
            display: "flex",
            justifyContent: "center",
          }}
        >
          <div
            style={{
              padding: "6px 20px",
              background: `${C.green}15`,
              border: `1px solid ${C.green}40`,
              borderRadius: 20,
              fontSize: 11,
              fontWeight: 600,
              color: C.green,
              display: "flex",
              alignItems: "center",
              gap: 6,
            }}
          >
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke={C.green} strokeWidth="2.5" strokeLinecap="round">
              <path d="M20 6L9 17l-5-5" />
            </svg>
            Task complete — trustless delivery
          </div>
        </div>
      </div>
    </div>
  );
}
