import { interpolate } from "remotion";
import { C } from "./constants";

const LINES: { text: string; color: string; prefix?: string }[] = [
  { text: "Connecting to deep-analyst...", color: C.muted, prefix: ">" },
  { text: "Task dispatched via A2A protocol", color: C.muted, prefix: ">" },
  { text: 'web_search("DeFi yield farming 2026")', color: C.accent, prefix: "[tool]" },
  { text: "Found 12 sources", color: C.green, prefix: "[result]" },
  { text: "analyze_content(sources=12)", color: C.accent, prefix: "[tool]" },
  { text: "Synthesis complete — 3 key findings", color: C.green, prefix: "[result]" },
  { text: "Generating summary with citations...", color: C.muted, prefix: ">" },
  { text: "Output: 847 tokens ✓", color: C.green, prefix: ">" },
];

export function Scene4Execute({ sceneFrame }: { frame: number; sceneFrame: number }) {
  const visibleLines = LINES.map((_, i) => {
    const lineFrame = 5 + i * 14;
    return interpolate(sceneFrame, [lineFrame, lineFrame + 5], [0, 1], {
      extrapolateLeft: "clamp",
      extrapolateRight: "clamp",
    });
  });

  const cursorBlink = sceneFrame % 15 < 10;
  const lastVisibleIdx = visibleLines.findLastIndex((v) => v > 0);

  return (
    <div style={{ display: "flex", flexDirection: "column", alignItems: "center", justifyContent: "center", height: "100%" }}>
      <div
        style={{
          width: 580,
          background: C.surface,
          border: `1px solid ${C.border}`,
          borderRadius: 10,
          overflow: "hidden",
        }}
      >
        {/* Terminal header */}
        <div
          style={{
            padding: "10px 16px",
            borderBottom: `1px solid ${C.border}`,
            display: "flex",
            alignItems: "center",
            gap: 8,
          }}
        >
          <div style={{ display: "flex", gap: 5 }}>
            <div style={{ width: 8, height: 8, borderRadius: "50%", background: "#ff5f57" }} />
            <div style={{ width: 8, height: 8, borderRadius: "50%", background: "#febc2e" }} />
            <div style={{ width: 8, height: 8, borderRadius: "50%", background: "#28c840" }} />
          </div>
          <span style={{ fontSize: 10, fontFamily: "'Courier New', monospace", color: C.dim, marginLeft: 8 }}>
            agent execution — deep-analyst
          </span>
        </div>

        {/* Terminal body */}
        <div style={{ padding: "14px 16px", minHeight: 200 }}>
          {LINES.map((line, i) => {
            if (visibleLines[i] <= 0) return null;

            const prefixColor = line.prefix === "[tool]" ? C.accent : line.prefix === "[result]" ? C.green : C.dim;

            return (
              <div
                key={i}
                style={{
                  opacity: visibleLines[i],
                  display: "flex",
                  gap: 8,
                  marginBottom: 6,
                  fontSize: 11,
                  fontFamily: "'Courier New', monospace",
                  lineHeight: 1.6,
                }}
              >
                <span style={{ color: prefixColor, flexShrink: 0, width: 48, textAlign: "right" }}>
                  {line.prefix}
                </span>
                <span style={{ color: line.color }}>{line.text}</span>
                {i === lastVisibleIdx && cursorBlink && visibleLines[i] >= 1 && i < LINES.length - 1 && (
                  <span style={{ color: C.accent }}>▊</span>
                )}
              </div>
            );
          })}
        </div>

        {/* Progress bar */}
        <div style={{ height: 2, background: C.border }}>
          <div
            style={{
              height: "100%",
              width: `${interpolate(sceneFrame, [0, 110], [0, 100], { extrapolateLeft: "clamp", extrapolateRight: "clamp" })}%`,
              background: `linear-gradient(90deg, ${C.accent}, ${C.green})`,
            }}
          />
        </div>
      </div>
    </div>
  );
}
