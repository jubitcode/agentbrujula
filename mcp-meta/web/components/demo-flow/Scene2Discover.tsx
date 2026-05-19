import { interpolate } from "remotion";
import { C } from "./constants";

const AGENTS = [
  { handle: "alpha-researcher", tier: "SONNET", caps: "Web, PDF", cost: "0.15", rep: "★★★★" },
  { handle: "deep-analyst", tier: "OPUS", caps: "Web, PDF, Analytics", cost: "0.22", rep: "★★★★★" },
  { handle: "quick-scan", tier: "HAIKU", caps: "Web", cost: "0.05", rep: "★★★" },
];

export function Scene2Discover({ sceneFrame }: { frame: number; sceneFrame: number }) {
  const loadingOpacity = interpolate(sceneFrame, [0, 5, 30, 38], [0, 1, 1, 0], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  const tableOpacity = interpolate(sceneFrame, [35, 45], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  const rowOpacities = AGENTS.map((_, i) =>
    interpolate(sceneFrame, [48 + i * 12, 56 + i * 12], [0, 1], {
      extrapolateLeft: "clamp",
      extrapolateRight: "clamp",
    })
  );

  const selectedIdx = 1;
  const highlightOpacity = interpolate(sceneFrame, [95, 105], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  const hireOpacity = interpolate(sceneFrame, [110, 118], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });
  const hireScale = sceneFrame > 120 ? 1 + 0.03 * Math.sin((sceneFrame - 120) * 0.3) : 1;

  const pulseOpacity = sceneFrame > 5 && sceneFrame < 35 ? 0.4 + 0.3 * Math.sin(sceneFrame * 0.2) : 0;

  return (
    <div style={{ display: "flex", flexDirection: "column", alignItems: "center", justifyContent: "center", height: "100%" }}>
      {/* Loading state */}
      <div
        style={{
          opacity: loadingOpacity,
          position: "absolute",
          display: "flex",
          alignItems: "center",
          gap: 8,
        }}
      >
        <div
          style={{
            width: 8,
            height: 8,
            borderRadius: "50%",
            background: C.accent,
            opacity: pulseOpacity,
          }}
        />
        <span style={{ fontSize: 12, fontFamily: "'Courier New', monospace", color: C.muted }}>
          Querying Soroban registry...
        </span>
      </div>

      {/* Agent table */}
      <div
        style={{
          opacity: tableOpacity,
          width: 640,
          background: C.surface,
          border: `1px solid ${C.border}`,
          borderRadius: 10,
          overflow: "hidden",
        }}
      >
        <div
          style={{
            padding: "14px 20px",
            borderBottom: `1px solid ${C.border}`,
            display: "flex",
            justifyContent: "space-between",
            alignItems: "center",
          }}
        >
          <span style={{ fontSize: 11, fontFamily: "'Courier New', monospace", color: C.dim, textTransform: "uppercase", letterSpacing: 1 }}>
            // agents found: 3
          </span>
          <span style={{ fontSize: 10, color: C.muted }}>sorted by reputation</span>
        </div>

        {/* Header row */}
        <div
          style={{
            display: "grid",
            gridTemplateColumns: "2fr 1fr 2fr 1fr 1fr",
            padding: "8px 20px",
            borderBottom: `1px solid ${C.border}`,
            fontSize: 9,
            fontFamily: "'Courier New', monospace",
            color: C.dim,
            textTransform: "uppercase",
            letterSpacing: 0.5,
          }}
        >
          <span>Handle</span>
          <span>Tier</span>
          <span>Capabilities</span>
          <span>Cost</span>
          <span>REP</span>
        </div>

        {/* Agent rows */}
        {AGENTS.map((agent, i) => {
          const isSelected = i === selectedIdx;
          return (
            <div
              key={agent.handle}
              style={{
                opacity: rowOpacities[i],
                display: "grid",
                gridTemplateColumns: "2fr 1fr 2fr 1fr 1fr",
                padding: "10px 20px",
                borderBottom: i < AGENTS.length - 1 ? `1px solid ${C.border}` : "none",
                background: isSelected ? `${C.accent}${Math.round(highlightOpacity * 12).toString(16).padStart(2, "0")}` : "transparent",
                borderLeft: isSelected ? `2px solid ${C.accent}` : "2px solid transparent",
                fontSize: 12,
                alignItems: "center",
                transition: "background 0.2s",
              }}
            >
              <span style={{ color: isSelected && highlightOpacity > 0.5 ? C.accent : C.text, fontWeight: isSelected ? 600 : 400 }}>
                {agent.handle}
              </span>
              <span
                style={{
                  fontSize: 9,
                  padding: "2px 6px",
                  background: `${C.surface2}`,
                  borderRadius: 4,
                  color: C.muted,
                  display: "inline-block",
                  width: "fit-content",
                }}
              >
                {agent.tier}
              </span>
              <span style={{ color: C.muted, fontSize: 11 }}>{agent.caps}</span>
              <span style={{ fontFamily: "'Courier New', monospace", color: C.text }}>
                {agent.cost}
              </span>
              <span style={{ color: C.amber, fontSize: 11 }}>{agent.rep}</span>
            </div>
          );
        })}

        {/* Hire button */}
        <div style={{ padding: "12px 20px", opacity: hireOpacity }}>
          <div
            style={{
              background: C.accent,
              borderRadius: 6,
              padding: "8px 0",
              textAlign: "center",
              fontSize: 12,
              fontWeight: 600,
              color: "#fff",
              transform: `scale(${hireScale})`,
              transformOrigin: "center",
            }}
          >
            Hire deep-analyst — 0.22 USDC
          </div>
        </div>
      </div>
    </div>
  );
}
