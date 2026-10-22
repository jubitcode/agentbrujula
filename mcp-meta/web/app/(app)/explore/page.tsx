"use client";

import { useState, useEffect } from "react";
import { useRouter } from "next/navigation";
import { getAgentCard } from "@/lib/api";
import type { Skill } from "@/lib/types";
import { useI18n } from "@/lib/i18n";
import { EmptyState, ErrorState, Loading, resolveState } from "@/components/ui/states";

const TOOL_REGISTRY_CONTRACT = "CARJRAXMYOM7TNNCR2K4OVZTO6BYUTUIW5AB2ZHDSF5UZXBSWU7YZQBW";
const REGISTRY_EXPLORER_URL = `https://stellar.expert/explorer/testnet/contract/${TOOL_REGISTRY_CONTRACT}`;

export default function ExplorePage() {
  const { t } = useI18n();
  const router = useRouter();
  const [skills, setSkills] = useState<Skill[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    getAgentCard()
      // The catch used to be empty, so a backend that was down and a registry
      // that was genuinely empty produced the same screen: "No skills
      // available". Keeping the error is presentation — the request is
      // unchanged.
      .then((card) => !cancelled && setSkills(card.skills || []))
      .catch((e) => !cancelled && setError(e instanceof Error ? e.message : String(e)))
      .finally(() => !cancelled && setLoading(false));
    return () => {
      cancelled = true;
    };
  }, []);

  const handleTry = (text: string) => {
    router.push(`/task/new?q=${encodeURIComponent(text)}`);
  };

  const state = resolveState({ loading, error, empty: skills.length === 0 });

  return (
    <div className="page">
      <section style={{ paddingTop: 12, paddingBottom: 24 }}>
        <div className="section-label">{`// ${t("explore.kicker")}`}</div>
        <h1 className="t-h2">{t("explore.title")}</h1>
        <p className="t-intro">
          {t("explore.intro", { n: skills.length })}{" "}
          <a
            href={REGISTRY_EXPLORER_URL}
            target="_blank"
            rel="noreferrer noopener"
            style={{ color: "var(--accent)", textDecoration: "none" }}
          >
            {t("explore.registry")} ↗
          </a>
        </p>
      </section>

      <section style={{ paddingBottom: 64 }}>
        {state === "loading" && <Loading />}

        {state === "error" && (
          <ErrorState title={t("explore.error")} detail={error} />
        )}

        {state === "empty" && (
          <EmptyState title={t("explore.empty")} hint={t("explore.emptyHint")} />
        )}

        {state === "ready" && (
          <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
            {skills.map((skill) => (
              <div key={skill.id} className="card">
                <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", gap: 12, flexWrap: "wrap" }}>
                  <div style={{ flex: "1 1 240px" }}>
                    <h2 className="t-h3 t-wrap">{skill.name}</h2>
                    <p className="t-small t-wrap" style={{ margin: 0 }}>{skill.description}</p>
                  </div>
                  <a
                    href={REGISTRY_EXPLORER_URL}
                    target="_blank"
                    rel="noreferrer noopener"
                    className="tag"
                    style={{ textDecoration: "none", padding: "4px 10px", flexShrink: 0 }}
                  >
                    {t("explore.verifyOn")} ↗
                  </a>
                </div>

                {skill.tags && skill.tags.length > 0 && (
                  <div style={{ display: "flex", gap: 6, marginTop: 10, flexWrap: "wrap" }}>
                    {skill.tags.map((tag) => (
                      <span key={tag} className="tag">{tag}</span>
                    ))}
                  </div>
                )}

                {skill.examples && skill.examples.length > 0 ? (
                  <div style={{ marginTop: 12, display: "flex", flexDirection: "column", gap: 6, alignItems: "flex-start" }}>
                    {skill.examples.map((ex) => (
                      <button
                        key={ex}
                        onClick={() => handleTry(ex)}
                        className="t-small t-wrap"
                        style={{
                          color: "var(--accent)",
                          background: "none",
                          border: "none",
                          cursor: "pointer",
                          padding: 0,
                          textAlign: "left",
                          fontSize: 12,
                        }}
                      >
                        {t("explore.try", { example: ex })}
                      </button>
                    ))}
                  </div>
                ) : (
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
                    {t("explore.tryPlain")} →
                  </button>
                )}
              </div>
            ))}
          </div>
        )}
      </section>
    </div>
  );
}
