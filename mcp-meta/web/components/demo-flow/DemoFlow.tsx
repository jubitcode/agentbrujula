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

/*
 * Every endpoint, contract call and field name below is the real one. The only
 * invented values are the task id and the transaction hash, which stand in for
 * whatever a given run produces — the footer says so.
 *
 * Checked against: api/server.py (/api/hire, /api/build-payment,
 * /api/submit-payment, /api/execute/{id}, /api/result/{id}),
 * contracts/usdc-escrow/src/lib.rs (create_escrow, settle) and
 * contracts/compression-compliance/src/lib.rs (verify_compression).
 */
const LINES: Array<{ at: number; kind: LineKind; text: string }> = [
  // chapter 1: hire, and the 402 that answers it
  { at: 25, kind: "cmd", text: `${PROMPT} curl -sX POST :5050/api/hire \\` },
  { at: 35, kind: "cmd", text: `       -d '{"agent_handle":"research-agent", …}'` },
  { at: 60, kind: "out", text: "→ POST /api/hire        no payment header" },
  { at: 78, kind: "in", text: "← 402 Payment Required" },
  { at: 92, kind: "json", text: "  price   : 0.15 USDC   estimate 0.10 × 1.5" },
  { at: 102, kind: "json", text: "  network : stellar:testnet" },
  { at: 112, kind: "json", text: "  payTo   : CAMPNA3T…OCCO   usdc-escrow" },

  // chapter 2: lock the money
  { at: 150, kind: "cmd", text: `${PROMPT} curl -sX POST :5050/api/build-payment` },
  { at: 170, kind: "in", text: "← unsigned XDR  AAAAAgAAAAD…" },
  { at: 190, kind: "log", text: "  signed in Freighter — the key stays in the browser" },
  { at: 212, kind: "out", text: "→ POST /api/submit-payment" },
  { at: 232, kind: "in", text: "← create_escrow()  tx 7c2bb9…c3e8a4" },
  { at: 255, kind: "ok", text: "  ✓ 0.15 USDC locked — the orchestrator holds nothing" },

  // chapter 3: the agent works
  { at: 295, kind: "cmd", text: `${PROMPT} curl -N :5050/api/execute/3f9c1b72…` },
  { at: 315, kind: "log", text: "  event: log       Sending to A2A" },
  { at: 338, kind: "log", text: "  event: progress  searching sources (1/2)" },
  { at: 360, kind: "log", text: "  event: complete  Execution complete" },

  // chapter 4: verify, then settle
  { at: 398, kind: "cmd", text: `${PROMPT} curl -s :5050/api/result/3f9c1b72…` },
  { at: 420, kind: "out", text: "→ verify_compression(in=1240, out=380, max=200)" },
  { at: 443, kind: "in", text: "← ratio 30%   compliant: true" },
  { at: 466, kind: "out", text: "→ settle(agent=0.10, refund=0.05)   one atomic call" },
  { at: 492, kind: "ok", text: "  ✓ attested on-chain · unused budget returned" },
];

const LINE_HEIGHT = 22;
const FADE_FRAMES = 8;

/*
 * Lines are absolutely positioned, the terminal body clips what overflows, and
 * only ~18 rows fit. Without this the last chapter — the settle, which is the
 * whole point — was rendered below the fold and never seen. Once more lines
 * than this are on screen, everything scrolls up by one row, like a real
 * terminal.
 */
const MAX_VISIBLE_LINES = 17;

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

  const shownCount = LINES.filter((l) => frame >= l.at).length;
  const scrollOffset = Math.max(0, shownCount - MAX_VISIBLE_LINES) * LINE_HEIGHT;

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
                  top: 18 + i * LINE_HEIGHT - scrollOffset,
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
              top: 18 + shownCount * LINE_HEIGHT - scrollOffset,
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
          <span>stellar:testnet · real endpoints · sample task id and tx hash</span>
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
