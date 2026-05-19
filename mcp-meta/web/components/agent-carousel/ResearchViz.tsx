"use client";

import { useState, useEffect } from "react";

const SOURCES = [
  { id: 1, title: "Mozilla Research — WASM Benchmarks 2026", type: "PDF" },
  { id: 2, title: "W3C — WASI 2.0 Specification", type: "WEB" },
  { id: 3, title: "GitHub — State of WebAssembly Survey", type: "DATA" },
];

export function ResearchViz({ active }: { active: boolean }) {
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
      if (current > SOURCES.length + 2) {
        clearInterval(id);
        return;
      }
      setStep(current);
    }, 1000);
    return () => clearInterval(id);
  }, [active]);

  // step 0: query visible
  // step 1-3: sources appear
  // step 4: synthesizing text
  // step 5: report appears

  const typeColors: Record<string, string> = { PDF: "#ef4444", WEB: "#3b82f6", DATA: "#22c55e" };

  return (
    <div style={{ minHeight: 220 }}>
      {/* Query */}
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
        &gt; &quot;Research WebAssembly adoption in 2026&quot;
      </div>

      {/* Sources */}
      <div style={{ marginBottom: 14 }}>
        <div
          style={{
            fontSize: 9,
            fontFamily: "'Courier New', monospace",
            color: "#555",
            textTransform: "uppercase",
            letterSpacing: 1,
            marginBottom: 8,
          }}
        >
          {step >= 1 ? `sources found: ${Math.min(step, 3)}/3` : "scanning..."}
        </div>

        <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
          {SOURCES.map((src, i) => {
            const visible = step >= i + 1;
            return (
              <div
                key={src.id}
                style={{
                  opacity: visible ? 1 : 0,
                  transform: visible ? "translateX(0)" : "translateX(-16px)",
                  transition: "all 0.4s ease",
                  display: "flex",
                  alignItems: "center",
                  gap: 10,
                  padding: "8px 12px",
                  background: "#0a0a0a",
                  borderRadius: 6,
                  border: "1px solid #222",
                }}
              >
                {/* Source number */}
                <span
                  style={{
                    fontSize: 10,
                    fontFamily: "'Courier New', monospace",
                    color: "#555",
                    flexShrink: 0,
                    width: 20,
                  }}
                >
                  [{src.id}]
                </span>

                {/* Type badge */}
                <span
                  style={{
                    fontSize: 8,
                    padding: "2px 6px",
                    background: `${typeColors[src.type]}18`,
                    border: `1px solid ${typeColors[src.type]}40`,
                    borderRadius: 4,
                    color: typeColors[src.type],
                    fontFamily: "'Courier New', monospace",
                    fontWeight: 600,
                    flexShrink: 0,
                  }}
                >
                  {src.type}
                </span>

                {/* Title */}
                <span style={{ fontSize: 11, color: "#888", flex: 1 }}>{src.title}</span>

                {/* Check */}
                <svg
                  width="14"
                  height="14"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="#22c55e"
                  strokeWidth="2"
                  strokeLinecap="round"
                  style={{ opacity: visible ? 1 : 0, transition: "opacity 0.3s 0.3s" }}
                >
                  <path d="M20 6L9 17l-5-5" />
                </svg>
              </div>
            );
          })}
        </div>
      </div>

      {/* Synthesizing indicator */}
      <div
        style={{
          opacity: step >= 4 && step < 5 ? 1 : step >= 5 ? 0.3 : 0,
          transition: "opacity 0.3s",
          fontSize: 10,
          fontFamily: "'Courier New', monospace",
          color: "#3b82f6",
          marginBottom: 10,
          display: "flex",
          alignItems: "center",
          gap: 6,
        }}
      >
        <div
          style={{
            width: 6,
            height: 6,
            borderRadius: "50%",
            background: "#3b82f6",
            animation: step === 4 ? "pulse-dot 1s infinite" : "none",
          }}
        />
        Synthesizing 3 sources...
      </div>

      {/* Report */}
      <div
        style={{
          opacity: step >= 5 ? 1 : 0,
          transform: step >= 5 ? "translateY(0)" : "translateY(10px)",
          transition: "all 0.5s ease",
          padding: "12px 14px",
          background: "#0a0a0a",
          border: "1px solid rgba(59, 130, 246, 0.25)",
          borderRadius: 6,
        }}
      >
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 6 }}>
          <span style={{ fontSize: 10, fontFamily: "'Courier New', monospace", color: "#3b82f6", fontWeight: 600 }}>
            REPORT
          </span>
          <span style={{ fontSize: 9, color: "#555", fontFamily: "'Courier New', monospace" }}>847 tokens</span>
        </div>
        <div style={{ fontSize: 12, fontWeight: 600, color: "#e0e0e0", marginBottom: 4 }}>
          WebAssembly Adoption: 2026 Landscape
        </div>
        <div style={{ fontSize: 11, color: "#888", lineHeight: 1.5, marginBottom: 6 }}>
          WASM adoption grew 340% across server-side workloads. WASI 2.0 enabled portable binaries, reducing deployment friction by 60%.
        </div>
        <div style={{ display: "flex", gap: 6 }}>
          {[1, 2, 3].map((n) => (
            <span
              key={n}
              style={{
                fontSize: 9,
                padding: "1px 6px",
                background: "rgba(59, 130, 246, 0.1)",
                border: "1px solid rgba(59, 130, 246, 0.2)",
                borderRadius: 3,
                color: "#3b82f6",
                fontFamily: "'Courier New', monospace",
              }}
            >
              [{n}]
            </span>
          ))}
        </div>
      </div>
    </div>
  );
}
