"use client";

import { useState } from "react";
import { useI18n, type Key } from "@/lib/i18n";
import { ErrorState } from "@/components/ui/states";

const MARKETPLACE_API =
  process.env.NEXT_PUBLIC_MARKETPLACE_URL || "http://localhost:5050";

/** What the contract records about a task's evidence. */
type Attestation = {
  task_id: string;
  attested: boolean;
  verdict?: "passed" | "failed";
  evidence_hash?: string;
  agent?: string;
  attested_at?: number;
  contract?: string | null;
  explorer?: string | null;
  note?: string;
};

type Check = { name: string; passed: boolean; detail: string };

/** The answer to "is what I am holding what was checked?" */
type Comparison = Attestation & {
  computed_hash: string;
  attested_hash?: string;
  matches: boolean;
  recomputed_verdict?: "passed" | "failed";
  checks?: Check[];
  conclusion: string;
};

function Mono({ children }: { children: React.ReactNode }) {
  return (
    <span style={{ fontFamily: "var(--mono)", fontSize: 12, wordBreak: "break-all" }}>
      {children}
    </span>
  );
}

function Verdict({ value, fallback }: { value?: string; fallback: string }) {
  const pass = value === "passed";
  return (
    <span
      style={{
        fontFamily: "var(--mono)",
        fontSize: 11,
        textTransform: "uppercase",
        letterSpacing: ".06em",
        padding: "3px 8px",
        borderRadius: 4,
        color: pass ? "#16a34a" : "#dc2626",
        background: pass ? "rgba(22,163,74,.1)" : "rgba(220,38,38,.1)",
        border: `1px solid ${pass ? "rgba(22,163,74,.3)" : "rgba(220,38,38,.3)"}`,
      }}
    >
      {value ?? fallback}
    </span>
  );
}

export default function VerifyPage() {
  const { t } = useI18n();
  const [taskId, setTaskId] = useState("");
  const [brief, setBrief] = useState("");
  const [output, setOutput] = useState("");
  const [agentHandle, setAgentHandle] = useState("");
  const [lookup, setLookup] = useState<Attestation | null>(null);
  const [comparison, setComparison] = useState<Comparison | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  function reset() {
    setError(null);
    setLookup(null);
    setComparison(null);
  }

  async function readChain() {
    if (!taskId.trim()) return;
    reset();
    setBusy(true);
    try {
      const r = await fetch(
        `${MARKETPLACE_API}/api/verify/${encodeURIComponent(taskId.trim())}`,
      );
      const body = await r.json();
      if (!r.ok) throw new Error(body?.detail ?? `HTTP ${r.status}`);
      setLookup(body);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  async function compare() {
    if (!taskId.trim()) return;
    reset();
    setBusy(true);
    try {
      const r = await fetch(`${MARKETPLACE_API}/api/verify`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          task_id: taskId.trim(),
          brief,
          output,
          agent_handle: agentHandle,
          called_tools: [],
        }),
      });
      const body = await r.json();
      if (!r.ok) throw new Error(body?.detail ?? `HTTP ${r.status}`);
      setComparison(body);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  const result = comparison ?? lookup;

  return (
    <div className="page">
      <section style={{ paddingTop: 12, paddingBottom: 20 }}>
        <div className="section-label">{`// ${t("verify.kicker")}`}</div>
        <h1 className="t-hero">{t("verify.title")}</h1>
        <p className="t-intro">{t("verify.intro")}</p>
      </section>

      <section
        className="card"
        style={{
          marginBottom: 24,
          background: "rgba(59,130,246,.06)",
          borderColor: "rgba(59,130,246,.2)",
        }}
      >
        <div className="t-small t-wrap" style={{ color: "var(--accent)", margin: 0 }}>
          <strong style={{ fontWeight: 600 }}>{t("verify.whyTitle")}</strong>{" "}
          {t("verify.whyBody")}
        </div>
      </section>

      <section className="card" style={{ marginBottom: 20 }}>
        <label className="field-label" htmlFor="v-task">{t("verify.taskId")}</label>
        <input
          id="v-task"
          className="input"
          value={taskId}
          onChange={(e) => setTaskId(e.target.value)}
          /* Was a real 32-character id, which reads as a value already loaded
             rather than as an example of what to type. */
          placeholder={t("verify.taskIdPlaceholder")}
          style={{ marginBottom: 14 }}
        />

        <button
          className="btn-primary btn-inline"
          onClick={readChain}
          disabled={busy || !taskId.trim()}
          style={{ marginBottom: 22 }}
        >
          {busy ? t("verify.reading") : t("verify.read")}
        </button>

        <div
          style={{
            borderTop: "1px solid var(--border)",
            paddingTop: 18,
          }}
        >
          <p className="t-small" style={{ margin: "0 0 14px" }}>{t("verify.compareHelp")}</p>

          <label className="field-label" htmlFor="v-agent">{t("verify.agentHandle")}</label>
          <input
            id="v-agent"
            className="input"
            value={agentHandle}
            onChange={(e) => setAgentHandle(e.target.value)}
            placeholder={t("verify.agentPlaceholder")}
            style={{ marginBottom: 12 }}
          />

          <label className="field-label" htmlFor="v-brief">{t("verify.brief")}</label>
          <textarea
            id="v-brief"
            className="input"
            value={brief}
            onChange={(e) => setBrief(e.target.value)}
            rows={3}
            placeholder={t("verify.briefPlaceholder")}
            style={{ marginBottom: 12, fontFamily: "var(--mono)", fontSize: 12 }}
          />

          <label className="field-label" htmlFor="v-output">{t("verify.deliverable")}</label>
          <textarea
            id="v-output"
            className="input"
            value={output}
            onChange={(e) => setOutput(e.target.value)}
            rows={6}
            placeholder={t("verify.deliverablePlaceholder")}
            style={{ marginBottom: 14, fontFamily: "var(--mono)", fontSize: 12 }}
          />

          <button
            className="btn-ghost btn-inline"
            onClick={compare}
            disabled={busy || !taskId.trim()}
          >
            {busy ? t("verify.comparing") : t("verify.compare")}
          </button>
        </div>
      </section>

      {/* An error and a result are mutually exclusive: `reset()` clears both
          before either request, so only one of these two blocks can render. */}
      {error && (
        <div style={{ marginBottom: 20 }}>
          <ErrorState
            title={t("verify.errorTitle")}
            hint={t("verify.errorHint")}
            detail={error}
          />
        </div>
      )}

      {result && (
        <section className="card" style={{ marginBottom: 20 }}>
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: 12,
              marginBottom: 16,
            }}
          >
            <Verdict value={result.verdict} fallback={t("verify.notAttested")} />
            {comparison && (
              <span
                style={{
                  fontFamily: "var(--mono)",
                  fontSize: 11,
                  textTransform: "uppercase",
                  letterSpacing: ".06em",
                  padding: "3px 8px",
                  borderRadius: 4,
                  color: comparison.matches ? "#16a34a" : "#dc2626",
                  background: comparison.matches
                    ? "rgba(22,163,74,.1)"
                    : "rgba(220,38,38,.1)",
                  border: `1px solid ${
                    comparison.matches
                      ? "rgba(22,163,74,.3)"
                      : "rgba(220,38,38,.3)"
                  }`,
                }}
              >
                {comparison.matches ? t("verify.hashMatches") : t("verify.hashDiffers")}
              </span>
            )}
          </div>

          {comparison && (
            <p
              style={{
                fontSize: 15,
                lineHeight: 1.6,
                margin: "0 0 18px",
                color: comparison.matches && comparison.verdict === "passed"
                  ? "var(--fg)"
                  : "#dc2626",
              }}
            >
              {comparison.conclusion}
            </p>
          )}

          {!result.attested && result.note && (
            <p style={{ fontSize: 14, color: "var(--muted)", lineHeight: 1.6, margin: "0 0 18px" }}>
              {result.note}
            </p>
          )}

          <dl style={{ display: "grid", gap: 10, margin: 0, fontSize: 13 }}>
            {comparison && (
              <>
                <Row label={t("verify.yourHash")}>
                  <Mono>{comparison.computed_hash}</Mono>
                </Row>
                {comparison.attested_hash && (
                  <Row label={t("verify.chainHash")}>
                    <Mono>{comparison.attested_hash}</Mono>
                  </Row>
                )}
              </>
            )}
            {!comparison && result.evidence_hash && (
              <Row label={t("verify.evidenceHash")}>
                <Mono>{result.evidence_hash}</Mono>
              </Row>
            )}
            {result.agent && (
              <Row label={t("verify.agentPaid")}>
                <Mono>{result.agent}</Mono>
              </Row>
            )}
            {result.attested_at && (
              <Row label={t("verify.attestedAt")}>
                <Mono>
                  {new Date(result.attested_at * 1000).toISOString()}
                </Mono>
              </Row>
            )}
            {result.contract && (
              <Row label={t("verify.contract")}>
                {result.explorer ? (
                  <a
                    href={result.explorer}
                    target="_blank"
                    rel="noopener noreferrer"
                    style={{ color: "var(--accent)" }}
                  >
                    <Mono>{result.contract}</Mono>
                  </a>
                ) : (
                  <Mono>{result.contract}</Mono>
                )}
              </Row>
            )}
          </dl>

          {comparison?.checks && comparison.checks.length > 0 && (
            <div style={{ marginTop: 20, borderTop: "1px solid var(--border)", paddingTop: 16 }}>
              <div className="field-label" style={{ marginBottom: 10 }}>
                {t("verify.checksTitle")}
              </div>
              <div style={{ display: "grid", gap: 8 }}>
                {comparison.checks.map((c) => (
                  <div
                    key={c.name}
                    style={{ display: "flex", gap: 10, alignItems: "baseline", fontSize: 12 }}
                  >
                    <span
                      style={{
                        fontFamily: "var(--mono)",
                        color: c.passed ? "#16a34a" : "#dc2626",
                        minWidth: 14,
                      }}
                    >
                      {c.passed ? "ok" : "✗"}
                    </span>
                    {/* The backend names these in English snake_case. They
                        are mapped to a phrase by key here; the raw name stays
                        beside it, because it is what a developer greps for.
                        The backend is untouched. */}
                    <span className="t-wrap" style={{ flex: 1 }}>
                      {checkLabel(c.name, t)}
                      <span style={{ fontFamily: "var(--mono)", fontSize: 10, color: "var(--dim)" }}>
                        {" "}{c.name}
                      </span>
                      <div style={{ color: "var(--muted)" }}>{c.detail}</div>
                    </span>
                  </div>
                ))}
              </div>
              <p className="t-small t-wrap" style={{ marginTop: 14 }}>
                {t("verify.checksNote")}
              </p>
            </div>
          )}
        </section>
      )}
    </div>
  );
}

/**
 * Label and value. The hard `190px 1fr` grid left about 140px for a 64-character
 * hash on a phone; `auto-fit` drops it to one column when there is no room.
 */
function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(190px, 1fr))", gap: "4px 12px" }}>
      <dt style={{ color: "var(--muted)" }}>{label}</dt>
      <dd style={{ margin: 0 }}>{children}</dd>
    </div>
  );
}

/** The four structural checks, by the name the backend sends. */
function checkLabel(name: string, t: (k: Key) => string): string {
  const key = `verify.check.${name}` as Key;
  const label = t(key);
  // `t` returns the key itself when there is no entry — a new check added
  // backend-side should show its own name rather than "verify.check.whatever".
  return label === key ? name : label;
}
