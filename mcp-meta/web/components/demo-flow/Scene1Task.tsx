import { interpolate } from "remotion";
import { C } from "./constants";

const TASK_TEXT = "Summarize the latest DeFi yield farming strategies and risks";
const CAPS = ["Web Search", "PDF Analysis", "Analytics"];

export function Scene1Task({ sceneFrame }: { frame: number; sceneFrame: number }) {
  const charCount = Math.floor(
    interpolate(sceneFrame, [10, 80], [0, TASK_TEXT.length], {
      extrapolateLeft: "clamp",
      extrapolateRight: "clamp",
    })
  );
  const typedText = TASK_TEXT.slice(0, charCount);
  const showCursor = sceneFrame > 10 && sceneFrame < 82 && sceneFrame % 15 < 10;

  const capOpacities = CAPS.map((_, i) =>
    interpolate(sceneFrame, [85 + i * 7, 92 + i * 7], [0, 1], {
      extrapolateLeft: "clamp",
      extrapolateRight: "clamp",
    })
  );

  const budgetOpacity = interpolate(sceneFrame, [108, 115], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  const btnOpacity = interpolate(sceneFrame, [118, 125], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });
  const btnScale = sceneFrame > 125 ? 1 + 0.03 * Math.sin((sceneFrame - 125) * 0.3) : 1;

  return (
    <div style={{ display: "flex", flexDirection: "column", alignItems: "center", justifyContent: "center", height: "100%" }}>
      <div
        style={{
          width: 520,
          background: C.surface,
          border: `1px solid ${C.border}`,
          borderRadius: 10,
          padding: 32,
        }}
      >
        <div
          style={{
            fontSize: 11,
            fontFamily: "'Courier New', monospace",
            color: C.dim,
            textTransform: "uppercase",
            letterSpacing: 1,
            marginBottom: 16,
          }}
        >
          // describe your task
        </div>

        {/* Input field */}
        <div
          style={{
            background: C.bg,
            border: `1px solid ${C.border2}`,
            borderRadius: 6,
            padding: "12px 14px",
            marginBottom: 16,
            minHeight: 44,
          }}
        >
          {charCount === 0 ? (
            <span style={{ color: C.dim, fontSize: 13 }}>What do you need the agent to do?</span>
          ) : (
            <>
              <span style={{ fontSize: 13, color: C.text, lineHeight: 1.5 }}>{typedText}</span>
              {showCursor && <span style={{ color: C.accent, fontWeight: 300 }}>|</span>}
            </>
          )}
        </div>

        {/* Capabilities */}
        <div style={{ marginBottom: 16 }}>
          <div style={{ fontSize: 10, color: C.dim, marginBottom: 8, fontFamily: "'Courier New', monospace" }}>
            capabilities
          </div>
          <div style={{ display: "flex", gap: 6 }}>
            {CAPS.map((cap, i) => (
              <span
                key={cap}
                style={{
                  opacity: capOpacities[i],
                  fontSize: 10,
                  padding: "4px 10px",
                  background: `${C.accent}20`,
                  border: `1px solid ${C.accent}50`,
                  borderRadius: 12,
                  color: C.accent,
                  fontWeight: 500,
                }}
              >
                {cap}
              </span>
            ))}
          </div>
        </div>

        {/* Budget */}
        <div
          style={{
            opacity: budgetOpacity,
            display: "flex",
            justifyContent: "space-between",
            alignItems: "center",
            marginBottom: 20,
          }}
        >
          <span style={{ fontSize: 10, color: C.dim, fontFamily: "'Courier New', monospace" }}>budget</span>
          <span style={{ fontSize: 14, fontFamily: "'Courier New', monospace", fontWeight: 600, color: C.text }}>
            0.25 <span style={{ fontSize: 10, color: C.muted }}>USDC</span>
          </span>
        </div>

        {/* Button */}
        <div style={{ opacity: btnOpacity, transform: `scale(${btnScale})`, transformOrigin: "center" }}>
          <div
            style={{
              background: C.accent,
              borderRadius: 6,
              padding: "10px 0",
              textAlign: "center",
              fontSize: 13,
              fontWeight: 600,
              color: "#fff",
            }}
          >
            Find Agents →
          </div>
        </div>
      </div>
    </div>
  );
}
