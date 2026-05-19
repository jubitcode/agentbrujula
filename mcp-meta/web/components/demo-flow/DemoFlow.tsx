import { AbsoluteFill, useCurrentFrame, interpolate } from "remotion";
import { C, DEMO_DURATION } from "./constants";

/*
 * Terminal-style animation of the actual hire flow.
 *
 * Loops through ~20s of `brujula` CLI output. Each line fades in on its own
 * frame. A blinking cursor sits at the bottom. The intent is to show what a
 * real run looks like to a developer, not a polished marketing reel.
 */

type LineKind = "cmd" | "out" | "in" | "json" | "log" | "ok";

const PROMPT = "andrea@brujula:~$";

const LINES: Array<{ at: number; kind: LineKind; text: string }> = [
  // chapter 1: discover + hire
  { at: 25, kind: "cmd", text: `${PROMPT} brujula hire research-agent \\` },
  { at: 35, kind: "cmd", text: `        --task "AI infra trends 2026" --budget 0.10` },
  { at: 60, kind: "out", text: "→ POST /api/hire" },
  { at: 78, kind: "in", text: "← 402 Payment Required" },
  { at: 92, kind: "json", text: "  rate         : 0.05 USDC/call" },
  { at: 102, kind: "json", text: "  budget_lock  : 0.15 USDC" },
  { at: 112, kind: "json", text: "  escrow       : CAFERYY…272A" },

  // chapter 2: lock
  { at: 150, kind: "cmd", text: `${PROMPT} brujula pay --sign` },
  { at: 175, kind: "log", text: "  signing with Freighter…" },
  { at: 205, kind: "out", text: "→ POST /api/submit" },
  { at: 225, kind: "in", text: "← tx 7c2bb9…c3e8a4 (in_ledger 5_812_443)" },
  { at: 250, kind: "ok", text: "  ✓ 0.15 USDC locked in escrow" },

  // chapter 3: execute
  { at: 290, kind: "log", text: "  [tool_call] search(\"AI infra trends 2026\")" },
  { at: 320, kind: "log", text: "  [tool_call] summarize(4 sources)" },
  { at: 355, kind: "log", text: "  [complete] output ready (380 tokens)" },

  // chapter 4: verify + settle
  { at: 395, kind: "cmd", text: `${PROMPT} brujula settle --task t-7c2b` },
  { at: 420, kind: "out", text: "→ compliance.verify_compression(in=1240, out=380)" },
  { at: 445, kind: "in", text: "← compliant: true" },
  { at: 470, kind: "out", text: "→ escrow.settle(agent=0.08, client_refund=0.07)" },
  { at: 495, kind: "ok", text: "  ✓ done in 4.2s" },
];

const LINE_HEIGHT = 22;
const FADE_FRAMES = 8;

function colorFor(kind: LineKind): string {
  switch (kind) {
    case "cmd":
      return C.text;
    case "out":
      return C.accent;
    case "in":
      return C.green;
    case "json":
      return C.muted;
    case "log":
      return C.dim;
    case "ok":
      return C.green;
  }
}

export function DemoFlow() {
  const frame = useCurrentFrame();
  const cursorOn = Math.floor(frame / 15) % 2 === 0;
  const progress = (frame / DEMO_DURATION) * 100;

  return (
    <AbsoluteFill
      style={{
        background: C.bg,
        fontFamily: "'SF Mono', Menlo, 'Courier New', monospace",
      }}
    >
      {/* terminal window chrome */}
      <div
        style={{
          position: "absolute",
          top: 24,
          left: 24,
          right: 24,
          bottom: 24,
          background: "#0d0d0d",
          border: `1px solid ${C.border}`,
          borderRadius: 10,
          overflow: "hidden",
          display: "flex",
          flexDirection: "column",
        }}
      >
        {/* title bar */}
        <div
          style={{
            display: "flex",
            alignItems: "center",
            padding: "10px 14px",
            background: C.surface,
            borderBottom: `1px solid ${C.border}`,
            gap: 8,
          }}
        >
          <span style={{ width: 11, height: 11, borderRadius: "50%", background: "#3a3a3a" }} />
          <span style={{ width: 11, height: 11, borderRadius: "50%", background: "#3a3a3a" }} />
          <span style={{ width: 11, height: 11, borderRadius: "50%", background: "#3a3a3a" }} />
          <span
            style={{
              marginLeft: 16,
              fontSize: 11,
              color: C.dim,
              fontFamily: "'SF Mono', Menlo, monospace",
            }}
          >
            brujula-demo.sh
          </span>
        </div>

        {/* terminal body */}
        <div
          style={{
            flex: 1,
            padding: "18px 22px",
            position: "relative",
            overflow: "hidden",
          }}
        >
          {LINES.map((line, i) => {
            if (frame < line.at) return null;
            const opacity = interpolate(
              frame,
              [line.at, line.at + FADE_FRAMES],
              [0, 1],
              { extrapolateRight: "clamp" }
            );
            return (
              <div
                key={i}
                style={{
                  position: "absolute",
                  top: 18 + i * LINE_HEIGHT,
                  left: 22,
                  right: 22,
                  fontSize: 13,
                  lineHeight: 1.4,
                  color: colorFor(line.kind),
                  opacity,
                  whiteSpace: "pre",
                  fontWeight: line.kind === "cmd" ? 500 : 400,
                }}
              >
                {line.text}
              </div>
            );
          })}

          {/* blinking cursor */}
          <div
            style={{
              position: "absolute",
              top: 18 + LINES.filter((l) => frame >= l.at).length * LINE_HEIGHT,
              left: 22,
              fontSize: 13,
              color: C.text,
              opacity: cursorOn ? 1 : 0,
            }}
          >
            ▍
          </div>
        </div>

        {/* footer with progress */}
        <div
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            padding: "8px 14px",
            background: C.surface,
            borderTop: `1px solid ${C.border}`,
            fontSize: 10,
            fontFamily: "'SF Mono', Menlo, monospace",
            color: C.dim,
          }}
        >
          <span>stellar:testnet</span>
          <span>{progress.toFixed(0)}%</span>
        </div>
      </div>

      {/* progress bar */}
      <div style={{ position: "absolute", bottom: 0, left: 0, right: 0, height: 2 }}>
        <div
          style={{
            height: "100%",
            width: `${progress}%`,
            background: C.accent,
          }}
        />
      </div>
    </AbsoluteFill>
  );
}
