"use client";

import { useState, useEffect } from "react";

const ARGS = [
  { side: "pro" as const, text: "PostgreSQL provides ACID transactions ensuring data integrity across complex user operations." },
  { side: "con" as const, text: "MongoDB's document model adapts naturally to evolving user profile schemas without migrations." },
  { side: "pro" as const, text: "JSONB columns offer NoSQL flexibility within the relational model — best of both worlds." },
  { side: "con" as const, text: "Built-in horizontal scaling through sharding handles growth without application changes." },
];

const PRO_ARGS = ARGS.filter((a) => a.side === "pro");
const CON_ARGS = ARGS.filter((a) => a.side === "con");

export function DebateViz({ active }: { active: boolean }) {
  const [step, setStep] = useState(-1);

  useEffect(() => {
    if (!active) {
      setStep(-1);
      return;
    }
    setStep(0);
    let current = 0;
    const id = setInterval(() => {
      current++;
      if (current > ARGS.length) {
        clearInterval(id);
        return;
      }
      setStep(current);
    }, 1100);
    return () => clearInterval(id);
  }, [active]);

  return (
    <div style={{ minHeight: 220 }}>
      {/* Input */}
      <div
        style={{
          padding: "8px 12px",
          background: "#0a0a0a",
          borderRadius: 6,
          border: "1px solid #222",
          marginBottom: 16,
          fontSize: 12,
          color: "#555",
          fontFamily: "'Courier New', monospace",
        }}
      >
        &gt; &quot;PostgreSQL vs MongoDB for user profiles?&quot;
      </div>

      {/* Two columns */}
      <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 10, marginBottom: 14 }}>
        {/* PRO */}
        <div>
          <div
            style={{
              fontSize: 9,
              fontFamily: "'Courier New', monospace",
              color: "#3b82f6",
              textTransform: "uppercase",
              letterSpacing: 1,
              marginBottom: 8,
              display: "flex",
              alignItems: "center",
              gap: 6,
            }}
          >
            <div style={{ width: 6, height: 6, borderRadius: "50%", background: "#3b82f6" }} />
            PostgreSQL
          </div>
          {PRO_ARGS.map((arg, i) => {
            const globalIdx = ARGS.indexOf(arg);
            const visible = step >= globalIdx;
            return (
              <div
                key={i}
                style={{
                  opacity: visible ? 1 : 0,
                  transform: visible ? "translateX(0)" : "translateX(-12px)",
                  transition: "all 0.45s ease",
                  padding: "8px 10px",
                  background: "#0a0a0a",
                  borderLeft: "2px solid #3b82f6",
                  borderRadius: "0 4px 4px 0",
                  marginBottom: 6,
                  fontSize: 11,
                  color: "#888",
                  lineHeight: 1.5,
                }}
              >
                {arg.text}
              </div>
            );
          })}
        </div>

        {/* CON */}
        <div>
          <div
            style={{
              fontSize: 9,
              fontFamily: "'Courier New', monospace",
              color: "#f59e0b",
              textTransform: "uppercase",
              letterSpacing: 1,
              marginBottom: 8,
              display: "flex",
              alignItems: "center",
              gap: 6,
            }}
          >
            <div style={{ width: 6, height: 6, borderRadius: "50%", background: "#f59e0b" }} />
            MongoDB
          </div>
          {CON_ARGS.map((arg, i) => {
            const globalIdx = ARGS.indexOf(arg);
            const visible = step >= globalIdx;
            return (
              <div
                key={i}
                style={{
                  opacity: visible ? 1 : 0,
                  transform: visible ? "translateX(0)" : "translateX(12px)",
                  transition: "all 0.45s ease",
                  padding: "8px 10px",
                  background: "#0a0a0a",
                  borderLeft: "2px solid #f59e0b",
                  borderRadius: "0 4px 4px 0",
                  marginBottom: 6,
                  fontSize: 11,
                  color: "#888",
                  lineHeight: 1.5,
                }}
              >
                {arg.text}
              </div>
            );
          })}
        </div>
      </div>

      {/* ADR Result */}
      <div
        style={{
          opacity: step >= ARGS.length ? 1 : 0,
          transform: step >= ARGS.length ? "translateY(0)" : "translateY(10px)",
          transition: "all 0.5s ease",
          padding: "10px 14px",
          background: "#0a0a0a",
          border: "1px solid rgba(34, 197, 94, 0.25)",
          borderRadius: 6,
        }}
      >
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 4 }}>
          <span style={{ fontSize: 11, fontFamily: "'Courier New', monospace", fontWeight: 600, color: "#22c55e" }}>
            ADR-0042
          </span>
          <span
            style={{
              fontSize: 9,
              padding: "2px 8px",
              background: "rgba(34, 197, 94, 0.12)",
              border: "1px solid rgba(34, 197, 94, 0.3)",
              borderRadius: 10,
              color: "#22c55e",
              fontFamily: "'Courier New', monospace",
            }}
          >
            ACCEPTED
          </span>
        </div>
        <div style={{ fontSize: 11, color: "#e0e0e0" }}>
          Decision: <strong>PostgreSQL with JSONB</strong>
        </div>
        <div style={{ fontSize: 10, color: "#555", marginTop: 2 }}>
          ACID compliance + document flexibility covers user profile requirements
        </div>
      </div>
    </div>
  );
}
