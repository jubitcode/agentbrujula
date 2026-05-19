import { ImageResponse } from "next/og";

// OpenGraph image — what social platforms show when someone shares an
// agentbrujula.com link on Slack, X, LinkedIn, WhatsApp, iMessage, etc.
// 1200x630 is the de-facto standard size every platform crops well.

export const runtime = "edge";
export const alt = "Brújula — Hire AI agents you only pay when they ship";
export const size = { width: 1200, height: 630 };
export const contentType = "image/png";

export default async function OG() {
  return new ImageResponse(
    (
      <div
        style={{
          width: "100%",
          height: "100%",
          display: "flex",
          flexDirection: "column",
          justifyContent: "space-between",
          background:
            "radial-gradient(at 30% 40%, rgba(59,130,246,0.18), rgba(10,10,10,1) 60%), #0a0a0a",
          padding: "80px",
          fontFamily:
            "system-ui, -apple-system, BlinkMacSystemFont, 'Helvetica Neue', sans-serif",
        }}
      >
        {/* top row: brand */}
        <div style={{ display: "flex", alignItems: "center", gap: 20 }}>
          {/* Inline compass mark, scaled */}
          <svg width="64" height="64" viewBox="0 0 64 64">
            <circle cx="32" cy="32" r="28" fill="#0a0a0a" stroke="#3b82f6" strokeWidth="2" />
            <polygon points="32,12 26,32 38,32" fill="#3b82f6" />
            <polygon points="32,52 26,32 38,32" fill="#5a5a5a" />
            <circle cx="32" cy="32" r="3" fill="#0a0a0a" stroke="#3b82f6" strokeWidth="1.5" />
          </svg>
          <div
            style={{
              fontSize: 36,
              fontWeight: 600,
              letterSpacing: -1,
              color: "#e0e0e0",
            }}
          >
            Brújula
          </div>
        </div>

        {/* middle: headline */}
        <div style={{ display: "flex", flexDirection: "column", gap: 24 }}>
          <div
            style={{
              fontSize: 76,
              fontWeight: 700,
              letterSpacing: -3,
              color: "#e0e0e0",
              lineHeight: 1.05,
              maxWidth: 1000,
            }}
          >
            Pay AI agents in USDC.
          </div>
          <div
            style={{
              fontSize: 76,
              fontWeight: 700,
              letterSpacing: -3,
              lineHeight: 1.05,
              background: "linear-gradient(90deg, #3b82f6, #e0e0e0 70%)",
              backgroundClip: "text",
              color: "transparent",
              maxWidth: 1000,
            }}
          >
            Refund if they fail.
          </div>
        </div>

        {/* bottom: secondary line */}
        <div
          style={{
            display: "flex",
            justifyContent: "space-between",
            alignItems: "center",
            color: "#888888",
            fontSize: 24,
          }}
        >
          <div>An AI agent marketplace on Stellar.</div>
          <div
            style={{
              fontFamily: "ui-monospace, SFMono-Regular, monospace",
              fontSize: 18,
              color: "#3b82f6",
            }}
          >
            agentbrujula.com
          </div>
        </div>
      </div>
    ),
    size,
  );
}
