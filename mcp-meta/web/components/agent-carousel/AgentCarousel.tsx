"use client";

import { useState, useEffect, useCallback } from "react";
import { DebateViz } from "./DebateViz";
import { ResearchViz } from "./ResearchViz";
import { TrendsViz } from "./TrendsViz";

const AGENTS = [
  {
    type: "debate",
    title: "Architecture debate",
    desc: "Several agents argue a technical decision from opposite sides. The output is an ADR you can paste into a repo.",
    price: "0.25",
  },
  {
    type: "research",
    title: "Deep research",
    desc: "Searches the web, reads the sources, writes a report with citations. You get markdown back, not a pdf.",
    price: "0.10",
  },
  {
    type: "trends",
    title: "Trend analysis",
    desc: "Pulls signals from a few sources and tells you what is moving. Useful when you do not have time to read everything.",
    price: "0.10",
  },
];

const VIZS = [DebateViz, ResearchViz, TrendsViz] as const;
const INTERVAL = 9500;

export function AgentCarousel() {
  const [active, setActive] = useState(0);
  const [paused, setPaused] = useState(false);

  useEffect(() => {
    if (paused) return;
    const timer = setInterval(() => {
      setActive((prev) => (prev + 1) % 3);
    }, INTERVAL);
    return () => clearInterval(timer);
  }, [paused, active]);

  const goTo = useCallback((i: number) => {
    setActive(i);
  }, []);

  return (
    <div onMouseEnter={() => setPaused(true)} onMouseLeave={() => setPaused(false)}>
      {/* Carousel */}
      <div
        style={{
          position: "relative",
          overflow: "hidden",
          borderRadius: 10,
          border: "1px solid var(--border)",
        }}
      >
        <div
          style={{
            display: "flex",
            transform: `translateX(-${active * 100}%)`,
            transition: "transform 0.55s cubic-bezier(0.4, 0, 0.2, 1)",
          }}
        >
          {AGENTS.map((agent, i) => {
            const Viz = VIZS[i];
            return (
              <div
                key={agent.type}
                style={{
                  minWidth: "100%",
                  background: "var(--surface)",
                  padding: "28px 32px",
                }}
              >
                {/* Header */}
                <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", marginBottom: 4 }}>
                  <div>
                    <div
                      style={{
                        fontSize: 10,
                        fontFamily: "var(--mono)",
                        textTransform: "uppercase",
                        letterSpacing: 1,
                        color: "var(--accent)",
                        marginBottom: 6,
                      }}
                    >
                      {agent.type}
                    </div>
                    <div style={{ fontSize: 16, fontWeight: 600, color: "var(--text)", marginBottom: 4 }}>
                      {agent.title}
                    </div>
                  </div>
                  <span
                    style={{
                      fontSize: 13,
                      fontFamily: "var(--mono)",
                      fontWeight: 600,
                      color: "var(--text)",
                      padding: "4px 12px",
                      background: "var(--bg)",
                      borderRadius: 6,
                      border: "1px solid var(--border)",
                    }}
                  >
                    {agent.price} USDC
                  </span>
                </div>
                <p style={{ fontSize: 12, color: "var(--muted)", lineHeight: 1.5, marginBottom: 20 }}>
                  {agent.desc}
                </p>

                {/* Viz */}
                <Viz active={active === i} />
              </div>
            );
          })}
        </div>

        {/* Prev arrow */}
        <button
          onClick={() => goTo((active - 1 + 3) % 3)}
          aria-label="Previous agent"
          style={{
            position: "absolute",
            left: 10,
            top: "50%",
            transform: "translateY(-50%)",
            width: 32,
            height: 32,
            borderRadius: "50%",
            border: "1px solid var(--border)",
            background: "var(--surface)",
            color: "var(--muted)",
            cursor: "pointer",
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            fontSize: 16,
            opacity: 0.7,
            transition: "opacity 0.2s",
          }}
          onMouseEnter={(e) => (e.currentTarget.style.opacity = "1")}
          onMouseLeave={(e) => (e.currentTarget.style.opacity = "0.7")}
        >
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
            <path d="M15 18l-6-6 6-6" />
          </svg>
        </button>

        {/* Next arrow */}
        <button
          onClick={() => goTo((active + 1) % 3)}
          aria-label="Next agent"
          style={{
            position: "absolute",
            right: 10,
            top: "50%",
            transform: "translateY(-50%)",
            width: 32,
            height: 32,
            borderRadius: "50%",
            border: "1px solid var(--border)",
            background: "var(--surface)",
            color: "var(--muted)",
            cursor: "pointer",
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            fontSize: 16,
            opacity: 0.7,
            transition: "opacity 0.2s",
          }}
          onMouseEnter={(e) => (e.currentTarget.style.opacity = "1")}
          onMouseLeave={(e) => (e.currentTarget.style.opacity = "0.7")}
        >
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
            <path d="M9 18l6-6-6-6" />
          </svg>
        </button>
      </div>

      {/* Dots + labels */}
      <div style={{ display: "flex", justifyContent: "center", gap: 20, marginTop: 16 }}>
        {AGENTS.map((agent, i) => (
          <button
            key={agent.type}
            onClick={() => goTo(i)}
            style={{
              background: "none",
              border: "none",
              cursor: "pointer",
              display: "flex",
              flexDirection: "column",
              alignItems: "center",
              gap: 6,
              padding: 0,
            }}
          >
            <div
              style={{
                width: active === i ? 28 : 8,
                height: 4,
                borderRadius: 2,
                background: active === i ? "var(--accent)" : "var(--border)",
                transition: "all 0.35s ease",
              }}
            />
            <span
              style={{
                fontSize: 9,
                fontFamily: "var(--mono)",
                textTransform: "uppercase",
                letterSpacing: 0.5,
                color: active === i ? "var(--text)" : "var(--dim)",
                transition: "color 0.3s",
              }}
            >
              {agent.type}
            </span>
          </button>
        ))}
      </div>
    </div>
  );
}
