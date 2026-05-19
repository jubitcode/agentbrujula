"use client";

import { useState, useEffect } from "react";
import { useRouter } from "next/navigation";
import { getAgentCard } from "@/lib/api";
import type { Skill } from "@/lib/types";

const TOOL_REGISTRY_CONTRACT = "CARJRAXMYOM7TNNCR2K4OVZTO6BYUTUIW5AB2ZHDSF5UZXBSWU7YZQBW";
const REGISTRY_EXPLORER_URL = `https://stellar.expert/explorer/testnet/contract/${TOOL_REGISTRY_CONTRACT}`;

export default function ExplorePage() {
  const router = useRouter();
  const [skills, setSkills] = useState<Skill[]>([]);

  useEffect(() => {
    getAgentCard()
      .then((card) => setSkills(card.skills || []))
      .catch(() => {});
  }, []);

  const handleTry = (text: string) => {
    router.push(`/task/new?q=${encodeURIComponent(text)}`);
  };

  return (
    <div style={{ maxWidth: 900, margin: "0 auto", padding: "0 24px" }}>
      <section style={{ paddingTop: 48, paddingBottom: 24 }}>
        <div className="section-label">// agents</div>
        <h1
          style={{
            fontSize: 28,
            fontWeight: 700,
            letterSpacing: -1,
            color: "var(--text)",
            marginBottom: 8,
            lineHeight: 1.2,
          }}
        >
          What can this system do?
        </h1>
        <p style={{ fontSize: 13, color: "var(--muted)", lineHeight: 1.7 }}>
          {skills.length} skills available. Each one is registered on-chain via the{" "}
          <a
            href={REGISTRY_EXPLORER_URL}
            target="_blank"
            rel="noreferrer noopener"
            style={{ color: "var(--accent)", textDecoration: "none" }}
          >
            Tool Registry contract
          </a>
          .
        </p>
      </section>

      <section style={{ paddingBottom: 64 }}>
        <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
          {skills.map((skill) => (
            <div
              key={skill.id}
              style={{
                background: "var(--surface)",
                border: "1px solid var(--border)",
                borderRadius: 10,
                padding: 18,
                transition: "border-color 0.2s",
              }}
            >
              <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", gap: 12 }}>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <h2 style={{ fontSize: 15, fontWeight: 600, color: "var(--text)", marginBottom: 4 }}>
                    {skill.name}
                  </h2>
                  <p style={{ fontSize: 13, color: "var(--muted)", lineHeight: 1.5 }}>{skill.description}</p>
                </div>
                <a
                  href={REGISTRY_EXPLORER_URL}
                  target="_blank"
                  rel="noreferrer noopener"
                  title={`Verify "${skill.name}" on Stellar Expert`}
                  style={{
                    fontSize: 10,
                    fontFamily: "var(--mono)",
                    color: "var(--dim)",
                    textDecoration: "none",
                    border: "1px solid var(--border2)",
                    padding: "4px 10px",
                    borderRadius: 6,
                    whiteSpace: "nowrap",
                    flexShrink: 0,
                  }}
                >
                  Ver en Stellar Expert ↗
                </a>
              </div>

              {skill.tags && skill.tags.length > 0 && (
                <div style={{ display: "flex", gap: 6, marginTop: 10, flexWrap: "wrap" }}>
                  {skill.tags.map((tag) => (
                    <span
                      key={tag}
                      style={{
                        fontSize: 10,
                        padding: "2px 8px",
                        borderRadius: 12,
                        background: "var(--surface2)",
                        color: "var(--muted)",
                      }}
                    >
                      {tag}
                    </span>
                  ))}
                </div>
              )}

              {skill.examples && skill.examples.length > 0 && (
                <div style={{ marginTop: 12, display: "flex", gap: 16, flexWrap: "wrap" }}>
                  {skill.examples.map((ex) => (
                    <button
                      key={ex}
                      onClick={() => handleTry(ex)}
                      style={{
                        fontSize: 12,
                        color: "var(--accent)",
                        background: "none",
                        border: "none",
                        cursor: "pointer",
                        padding: 0,
                        textAlign: "left",
                      }}
                    >
                      Try: &ldquo;{ex}&rdquo;
                    </button>
                  ))}
                </div>
              )}

              {(!skill.examples || skill.examples.length === 0) && (
                <button
                  onClick={() => handleTry(skill.description)}
                  style={{
                    marginTop: 12,
                    fontSize: 12,
                    color: "var(--accent)",
                    background: "none",
                    border: "none",
                    cursor: "pointer",
                    padding: 0,
                  }}
                >
                  Try it →
                </button>
              )}
            </div>
          ))}

          {skills.length === 0 && (
            <div style={{ textAlign: "center", padding: "48px 0", color: "var(--dim)", fontSize: 13 }}>
              No skills available. Start MCP-Meta with agent manifests.
            </div>
          )}
        </div>
      </section>
    </div>
  );
}
