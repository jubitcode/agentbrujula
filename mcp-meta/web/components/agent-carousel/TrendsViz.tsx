"use client";

import { useState, useEffect } from "react";

const BARS = [
  { label: "GPU Clusters", value: 127, color: "#3b82f6" },
  { label: "Edge Compute", value: 89, color: "#8b5cf6" },
  { label: "WASM Runtime", value: 64, color: "#22c55e" },
  { label: "Vector DBs", value: 52, color: "#f59e0b" },
  { label: "LLM Infra", value: 143, color: "#ef4444" },
];

const MAX_VAL = Math.max(...BARS.map((b) => b.value));
const CHART_H = 120;

const INSIGHTS = [
  { text: "LLM Infrastructure leads growth at +143% YoY", color: "#ef4444" },
  { text: "GPU Cluster demand up +127%, driven by training workloads", color: "#3b82f6" },
  { text: "Edge Compute adoption accelerating in latency-sensitive apps", color: "#8b5cf6" },
];

export function TrendsViz({ active }: { active: boolean }) {
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
      if (current > BARS.length + INSIGHTS.length) {
        clearInterval(id);
        return;
      }
      setStep(current);
    }, 600);
    return () => clearInterval(id);
  }, [active]);

  // steps 0-4: bars appear
  // steps 5-7: insights appear

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
        &gt; &quot;What is trending in AI infrastructure?&quot;
      </div>

      {/* Chart */}
      <div style={{ display: "flex", gap: 2, alignItems: "flex-end", height: CHART_H, marginBottom: 6, padding: "0 20px" }}>
        {BARS.map((bar, i) => {
          const visible = step >= i;
          const barH = (bar.value / MAX_VAL) * (CHART_H - 20);
          return (
            <div key={bar.label} style={{ flex: 1, display: "flex", flexDirection: "column", alignItems: "center", gap: 4 }}>
              {/* Value label */}
              <span
                style={{
                  fontSize: 9,
                  fontFamily: "'Courier New', monospace",
                  fontWeight: 600,
                  color: bar.color,
                  opacity: visible ? 1 : 0,
                  transition: "opacity 0.3s ease 0.3s",
                }}
              >
                +{bar.value}%
              </span>
              {/* Bar */}
              <div
                style={{
                  width: "70%",
                  height: visible ? barH : 0,
                  background: `linear-gradient(180deg, ${bar.color}, ${bar.color}60)`,
                  borderRadius: "3px 3px 0 0",
                  transition: "height 0.6s cubic-bezier(0.34, 1.56, 0.64, 1)",
                  position: "relative",
                }}
              >
                {/* Glow effect at top */}
                <div
                  style={{
                    position: "absolute",
                    top: 0,
                    left: "50%",
                    transform: "translateX(-50%)",
                    width: "120%",
                    height: 4,
                    background: bar.color,
                    borderRadius: 2,
                    filter: "blur(4px)",
                    opacity: visible ? 0.5 : 0,
                    transition: "opacity 0.3s 0.5s",
                  }}
                />
              </div>
            </div>
          );
        })}
      </div>

      {/* X-axis labels */}
      <div style={{ display: "flex", gap: 2, padding: "0 20px", borderTop: "1px solid #222", paddingTop: 6, marginBottom: 14 }}>
        {BARS.map((bar, i) => (
          <div
            key={bar.label}
            style={{
              flex: 1,
              textAlign: "center",
              fontSize: 8,
              fontFamily: "'Courier New', monospace",
              color: step >= i ? "#888" : "#333",
              transition: "color 0.3s",
              lineHeight: 1.3,
            }}
          >
            {bar.label}
          </div>
        ))}
      </div>

      {/* Insights */}
      <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
        {INSIGHTS.map((insight, i) => {
          const visible = step >= BARS.length + i;
          return (
            <div
              key={i}
              style={{
                opacity: visible ? 1 : 0,
                transform: visible ? "translateX(0)" : "translateX(-10px)",
                transition: "all 0.4s ease",
                display: "flex",
                alignItems: "center",
                gap: 8,
                padding: "6px 10px",
                background: "#0a0a0a",
                borderRadius: 4,
                borderLeft: `2px solid ${insight.color}`,
              }}
            >
              <svg
                width="12"
                height="12"
                viewBox="0 0 24 24"
                fill="none"
                stroke={insight.color}
                strokeWidth="2"
                strokeLinecap="round"
              >
                <polyline points="23 6 13.5 15.5 8.5 10.5 1 18" />
                <polyline points="17 6 23 6 23 12" />
              </svg>
              <span style={{ fontSize: 10, color: "#888", lineHeight: 1.4 }}>{insight.text}</span>
            </div>
          );
        })}
      </div>
    </div>
  );
}
