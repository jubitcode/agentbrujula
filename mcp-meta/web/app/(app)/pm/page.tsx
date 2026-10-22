"use client"

/**
 * Brief → plan → revision.
 *
 * One screen, one vertical path, no payment. The agent runs through
 * `/api/pm`, a same-origin route that holds the API key server-side; the
 * browser never sees it and there is no CORS in the way.
 *
 * Every version of the plan stays on screen. A revision produces the next one
 * rather than editing the last, so the page keeps them stacked and dims the
 * superseded ones: what changed between v1 and v2 is often the most useful
 * thing a person can read, and replacing the document in place throws it away.
 *
 * Two presentation defects were fixed here, and the code reads oddly without
 * the reason:
 *
 *   The brief used to vanish. It rendered under `plans.length === 0`, so the
 *   moment a plan arrived the idea it came from was gone — and the plan is a
 *   reading of that idea, which you cannot check against something you can no
 *   longer see. It stays now, collapsed, above the plans.
 *
 *   The error used to appear 3.360px from the button that caused it. A single
 *   `error` string rendered in one fixed place above the plan list, so a
 *   failed revision put its message off-screen, above two full plans. The
 *   failure now carries where it came from and renders beside that field.
 *
 * The screen has two views, which is the third fix. Every version used to
 * render in full, stacked, dimmed to 0.62 — two plans made the page 4.800px
 * tall and nothing marked where one ended. Now the brief gets a workspace of
 * its own, and the plan is a document beside a fixed column of controls, with
 * the versions as a row of pills. Only the selected one is rendered; the
 * others are one click away rather than one scroll.
 */

import { useEffect, useRef, useState } from "react"
import type { Plan, PlanResponse, SkippedFile } from "@/lib/plan"
import { useI18n, type Key } from "@/lib/i18n"
import {
  advance,
  clearContract,
  onContractChange,
  readContract,
  type DemoContract,
} from "@/lib/pm-contract"
import { PmContract } from "@/components/pm-contract"
import { DemoNotice } from "@/components/demo-notice"
import { ContractState } from "@/components/contract-state"

/** Text we can actually put in a prompt. Anything else is reported, not read. */
const READABLE = [".md", ".txt", ".json", ".yaml", ".yml", ".csv"]
const MAX_FILE_BYTES = 200_000
const MAX_TOTAL_BYTES = 600_000
const MIN_BRIEF = 12
/** Not a hard limit — the counter is there to show the box has room. */
const BRIEF_SOFT_MAX = 1200

type ContextFile = { name: string; content: string }

/**
 * Where a failure belongs on screen.
 *
 * A message is only useful next to the control that produced it, and this
 * page has two: the brief box and the revision box.
 */
type Failure = { where: "brief" | "revision"; message: string }

export default function PmPage() {
  const { t, tl } = useI18n()
  const [brief, setBrief] = useState("")
  /** The brief the current plans were made from, kept so it can stay on screen. */
  const [submittedBrief, setSubmittedBrief] = useState("")
  const [plans, setPlans] = useState<Plan[]>([])
  const [feedback, setFeedback] = useState("")
  const [busy, setBusy] = useState(false)
  const [step, setStep] = useState("")
  const [failure, setFailure] = useState<Failure | null>(null)
  const [files, setFiles] = useState<ContextFile[]>([])
  const [skipped, setSkipped] = useState<SkippedFile[]>([])
  const fileInput = useRef<HTMLInputElement>(null)
  const revisionBox = useRef<HTMLTextAreaElement>(null)
  /** Index into `plans`. A new version selects itself; older ones are a click. */
  const [shown, setShown] = useState(0)
  /**
   * The simulated contract. Null until funds are "locked".
   *
   * The brief is gated on it so the demonstration shows the order that
   * matters: you agree, the money is held, *then* the work starts. Letting
   * someone write the brief first would show the opposite of what escrow is.
   */
  const [contract, setContract] = useState<DemoContract | null>(null)
  const [contractRead, setContractRead] = useState(false)

  useEffect(() => {
    const read = () => {
      setContract(readContract())
      setContractRead(true)
    }
    read()
    return onContractChange(read)
  }, [])

  // Move the cursor to the field the message is about. Putting the error
  // beside the box is half of the fix; the other half is not making someone
  // find the box again.
  useEffect(() => {
    if (failure?.where === "revision") revisionBox.current?.focus()
  }, [failure])

  async function call(op: "plan" | "revise", params: Record<string, unknown>) {
    setBusy(true)
    setFailure(null)
    setStep(op === "plan" ? t("pm.stepPlan") : t("pm.stepRevise"))
    advance("working", t("activity.workingTitle"), t("activity.workingDetail"))
    try {
      const res = await fetch("/api/pm", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ op, params }),
      })
      const payload = await res.json()

      // Two different failures arrive differently and both are worth showing
      // as they are: the route's own (bad status) and the pod's (200 with
      // `error` inside `result`, which is how mcp-meta reports tool failures).
      if (!res.ok) throw new Error(payload?.error || `HTTP ${res.status}`)
      const result: PlanResponse = payload?.result ?? payload
      if (result?.error) throw new Error(result.error)
      if (!result?.plan) throw new Error(t("pm.noPlan"))

      advance("delivered", t("activity.deliveredTitle"), t("activity.deliveredDetail"))
      setPlans((prev) => {
        const next = [...prev, result.plan as Plan]
        // Jump to what just arrived rather than leaving the reader on the
        // version they asked to replace.
        setShown(next.length - 1)
        return next
      })
      setSkipped(result.skipped_files ?? [])
      setFeedback("")
    } catch (e) {
      setFailure({
        where: op === "plan" ? "brief" : "revision",
        message: e instanceof Error ? e.message : String(e),
      })
    } finally {
      setBusy(false)
      setStep("")
    }
  }

  function submitBrief() {
    const text = brief.trim()
    // Checked here as well as in the pod. Sending three words produces a plan
    // about nothing, which looks like the product working.
    if (text.length < MIN_BRIEF) {
      setFailure({ where: "brief", message: t("pm.briefTooShort", { n: MIN_BRIEF }) })
      return
    }
    setSubmittedBrief(text)
    void call("plan", { brief: text, context_files: files })
  }

  function submitRevision() {
    const text = feedback.trim()
    if (!text) {
      setFailure({ where: "revision", message: t("pm.revisionEmpty") })
      return
    }
    // Revise the version being read, not blindly the last one: with the
    // picker, those can differ.
    const base = plans[Math.min(shown, plans.length - 1)]
    void call("revise", { plan: base, feedback: text, context_files: files })
  }

  async function pickFiles(event: React.ChangeEvent<HTMLInputElement>) {
    const chosen = Array.from(event.target.files ?? [])
    const kept: ContextFile[] = []
    const left: SkippedFile[] = []
    let budget = MAX_TOTAL_BYTES

    for (const file of chosen) {
      const name = file.webkitRelativePath || file.name
      const ext = name.slice(name.lastIndexOf(".")).toLowerCase()
      if (!READABLE.includes(ext)) {
        left.push({ name, reason: `no es texto (${ext || "sin extensión"})` })
        continue
      }
      if (file.size > MAX_FILE_BYTES) {
        left.push({ name, reason: `${Math.round(file.size / 1024)} KB, pasa el límite por archivo` })
        continue
      }
      if (file.size > budget) {
        left.push({ name, reason: "no entró en el presupuesto de contexto" })
        continue
      }
      try {
        kept.push({ name, content: await file.text() })
        budget -= file.size
      } catch (e) {
        left.push({ name, reason: e instanceof Error ? e.message : "no se pudo leer" })
      }
    }

    setFiles(kept)
    // Shown before sending, not only after. Noticing that half the folder was
    // skipped matters most while there is still time to pick another one.
    setSkipped(left)
  }

  const current = plans.length ? plans[Math.min(shown, plans.length - 1)] : null
  const isLatest = shown === plans.length - 1

  // ── View 0: the agreement ───────────────────────────────────────────────
  //
  // Nothing happens before the contract exists. That ordering *is* the
  // demonstration: you see the terms, the funds are held, and only then does
  // the agent start. Letting the brief come first would show the opposite of
  // what escrow means.
  if (!contractRead) return null
  if (!contract) {
    return <PmContract onStart={() => setContract(readContract())} />
  }

  // ── View A: the brief, with room to think ───────────────────────────────
  if (plans.length === 0) {
    return (
      <div className="page">
        <DemoNotice short />
        <ContractState contract={contract} />
        <section style={{ paddingBottom: 22 }}>
          <div className="section-label">{`// ${t("pm.wsKicker")}`}</div>
          <h1 className="t-hero">{t("pm.title")}</h1>
          <p className="t-intro">{t("pm.intro")}</p>
        </section>

        <section className="workspace">
          <div className="workspace-grid">
            <div>
              <label className="field-label" htmlFor="pm-brief">{t("pm.briefLabel")}</label>
              <textarea
                id="pm-brief"
                className="workspace-input t-wrap"
                value={brief}
                onChange={(e) => setBrief(e.target.value)}
                disabled={busy}
                placeholder={t("pm.briefPlaceholder")}
                aria-invalid={failure?.where === "brief" || undefined}
                aria-describedby={failure?.where === "brief" ? "pm-brief-error" : undefined}
              />
              <div className="workspace-hint">
                <span>{t("pm.moreContext")}</span>
                <span style={{ fontFamily: "var(--mono)" }}>
                  {t("pm.chars", { n: brief.length, max: BRIEF_SOFT_MAX })}
                </span>
              </div>

              <div style={{ borderTop: "1px solid var(--border)", paddingTop: 16, marginTop: 18 }}>
                <div className="field-label">{t("pm.contextLabel")}</div>
                <p className="t-small" style={{ margin: "0 0 10px" }}>
                  {t("pm.contextHelp", { formats: READABLE.join(", ") })}
                </p>
                <input
                  ref={fileInput}
                  type="file"
                  multiple
                  onChange={pickFiles}
                  disabled={busy}
                  aria-label={t("pm.contextPick")}
                  style={{ fontSize: 13, color: "var(--muted)", maxWidth: "100%" }}
                />
                {files.length > 0 && (
                  <p className="t-small" style={{ color: "var(--green)", margin: "8px 0 0" }}>
                    {t(files.length === 1 ? "pm.contextCount" : "pm.contextCountPlural", { n: files.length })}
                  </p>
                )}
              </div>
            </div>

            {/* What comes back, stated before the wait rather than after it. */}
            <aside className="workspace-side">
              <div>
                <strong style={{ fontSize: 14 }}>{t("pm.wsReturns")}</strong>
                <ul>
                  {tl("pm.wsReturnsList").map((item) => (
                    <li key={item}>{item}</li>
                  ))}
                </ul>
              </div>
              <div>
                <button className="btn-primary" onClick={submitBrief} disabled={busy} style={{ width: "100%" }}>
                  {busy ? (
                    <>
                      <span className="spinner" aria-hidden="true" />
                      {step || t("pm.working")}
                    </>
                  ) : (
                    `${t("pm.submit")} →`
                  )}
                </button>
                {busy && (
                  <p className="t-small" style={{ color: "var(--dim)", margin: "10px 0 0" }}>
                    {t("pm.waitHint")}
                  </p>
                )}
                {failure?.where === "brief" && (
                  <FieldError id="pm-brief-error" message={failure.message} />
                )}
              </div>
            </aside>
          </div>
        </section>

        {skipped.length > 0 && <SkippedFiles skipped={skipped} t={t} />}
      </div>
    )
  }

  // ── View B: the plan, as a document ─────────────────────────────────────
  return (
    <div className="page">
      <DemoNotice short />
      <section style={{ paddingBottom: 20 }}>
        <div className="section-label">{`// ${t("pm.reviewKicker")}`}</div>
        <h1 className="t-h2">{t("pm.docTitle")}</h1>
        <p className="t-intro">{t("pm.reviewIntro")}</p>
      </section>

      <div className="plan-layout">
        <aside className="plan-controls">
          {/* The decision. The plan is in, the money is not: this is the
              moment escrow exists for, so it leads the column. */}
          {contract.stage === "delivered" && (
            <div className="decide-card">
              <strong style={{ fontSize: 14, display: "block" }}>{t("contract.decide")}</strong>
              <p className="t-small" style={{ margin: "7px 0 0" }}>{t("contract.decideHint")}</p>
              <div className="decide-actions">
                <button
                  className="btn-primary"
                  onClick={() =>
                    advance("released", t("activity.approvedTitle"), t("activity.approvedDetail"))
                  }
                >
                  {t("contract.approve")}
                </button>
                <button
                  className="btn-dispute"
                  onClick={() =>
                    advance("disputed", t("activity.disputedTitle"), t("activity.disputedDetail"))
                  }
                >
                  {t("contract.dispute")}
                </button>
              </div>
            </div>
          )}

          {contract.stage === "released" && (
            <div className="decide-card" style={{ background: "rgba(34,197,94,.06)", borderColor: "rgba(34,197,94,.28)" }}>
              <strong style={{ fontSize: 14, display: "block", color: "var(--green)" }}>
                ✓ {t("contract.released")}
              </strong>
              <p className="t-small" style={{ margin: "7px 0 0" }}>
                {t("contract.releasedDetail", { amount: contract.amount })}
              </p>
              <button
                className="btn-ghost btn-sm btn-inline"
                style={{ marginTop: 14 }}
                onClick={() => {
                  clearContract()
                  setPlans([])
                  setBrief("")
                  setSubmittedBrief("")
                  setShown(0)
                }}
              >
                {t("contract.newContract")}
              </button>
            </div>
          )}

          {contract.stage === "disputed" && (
            <div className="decide-card" style={{ background: "rgba(239,68,68,.06)", borderColor: "rgba(239,68,68,.28)" }}>
              <strong style={{ fontSize: 14, display: "block", color: "#ef4444" }}>
                {t("contract.disputed")}
              </strong>
              <p className="t-small" style={{ margin: "7px 0 0" }}>{t("contract.disputedDetail")}</p>
            </div>
          )}

          <ContractState contract={contract} />

          {/* The brief: kept, collapsed, one click from view. It used to
              disappear the moment a plan arrived — and the plan is a reading
              of it, which you cannot check against something you cannot see. */}
          <details className="disclosure" style={{ marginBottom: 18 }}>
            <summary>
              <span className="field-label" style={{ margin: 0 }}>{t("pm.briefSummary")}</span>
            </summary>
            <p className="t-small t-wrap" style={{ margin: "12px 0 0", color: "var(--text)" }}>
              {submittedBrief}
            </p>
            {files.length > 0 && (
              <p className="t-small" style={{ margin: "10px 0 0", color: "var(--dim)" }}>
                {t(files.length === 1 ? "pm.briefFiles" : "pm.briefFilesPlural", { n: files.length })}
                {": "}
                {files.map((f) => f.name).join(", ")}
              </p>
            )}
          </details>

          {plans.length > 1 && (
            <div>
              <div className="field-label">{t("pm.versions")}</div>
              <div className="version-row">
                {plans.map((plan, i) => (
                  <button
                    key={plan.version}
                    className="version-pill"
                    aria-pressed={i === shown}
                    onClick={() => setShown(i)}
                  >
                    {t("pm.versionShort", { n: plan.version })}
                  </button>
                ))}
              </div>
            </div>
          )}

          {!isLatest && (
            <p className="t-small" style={{ color: "var(--amber)", margin: "0 0 14px" }}>
              {t("pm.viewingOld")}{" "}
              <button
                onClick={() => setShown(plans.length - 1)}
                style={{ background: "none", border: 0, padding: 0, color: "var(--accent)", cursor: "pointer", font: "inherit" }}
              >
                {t("pm.viewLatest")} →
              </button>
            </p>
          )}

          {/* Revision, beside the document it is about. */}
          <label className="field-label" htmlFor="pm-revision">{t("pm.reviseLabel")}</label>
          <p className="t-small" style={{ margin: "0 0 10px" }}>{t("pm.reviseHelp")}</p>
          <textarea
            id="pm-revision"
            ref={revisionBox}
            className="input t-wrap"
            value={feedback}
            onChange={(e) => setFeedback(e.target.value)}
            rows={4}
            disabled={busy}
            placeholder={t("pm.revisePlaceholder")}
            aria-invalid={failure?.where === "revision" || undefined}
            aria-describedby={failure?.where === "revision" ? "pm-revision-error" : undefined}
            style={{ marginBottom: 12, lineHeight: 1.6 }}
          />
          <button className="btn-primary" onClick={submitRevision} disabled={busy} style={{ width: "100%" }}>
            {busy ? <span className="spinner" aria-hidden="true" /> : "↻"} {t("pm.reviseSubmit")}
          </button>

          {busy && (
            <p className="t-small" style={{ color: "var(--dim)", margin: "10px 0 0" }}>{t("pm.waitHint")}</p>
          )}
          {failure?.where === "revision" && (
            <FieldError id="pm-revision-error" message={failure.message} />
          )}

          {skipped.length > 0 && (
            <div style={{ marginTop: 16 }}>
              <SkippedFiles skipped={skipped} t={t} compact />
            </div>
          )}
        </aside>

        {current && <PlanDocument plan={current} t={t} tl={tl} />}
      </div>
    </div>
  )
}

/** Files offered as context that could not be read, and why. */
function SkippedFiles({
  skipped,
  t,
  compact,
}: {
  skipped: SkippedFile[]
  t: (k: Key, vars?: Record<string, string | number>) => string
  compact?: boolean
}) {
  return (
    <section className="card" style={{ marginTop: compact ? 0 : 20, borderColor: "rgba(245,158,11,.3)" }}>
      <div className="field-label" style={{ color: "var(--amber)" }}>{t("pm.skippedTitle")}</div>
      <ul style={{ margin: 0, paddingLeft: 18 }}>
        {skipped.map((s) => (
          <li key={s.name} className="t-small t-wrap">
            <span style={{ fontFamily: "var(--mono)", fontSize: 12 }}>{s.name}</span> — {s.reason}
          </li>
        ))}
      </ul>
      {!compact && (
        <p className="t-small" style={{ color: "var(--dim)", margin: "10px 0 0" }}>{t("pm.skippedHint")}</p>
      )}
    </section>
  )
}

/**
 * An error that belongs to one field, rendered under it.
 *
 * `role="alert"` so it is announced and not only drawn: this message can
 * appear without the page moving, which is the point, but that also means
 * nothing else signals it.
 */
function FieldError({ id, message }: { id: string; message: string }) {
  return (
    <p
      id={id}
      role="alert"
      className="t-small t-wrap"
      style={{
        margin: "12px 0 0",
        padding: "10px 12px",
        borderRadius: "var(--r)",
        background: "rgba(239,68,68,0.08)",
        border: "1px solid rgba(239,68,68,.28)",
        color: "#ef4444",
      }}
    >
      {message}
    </p>
  )
}

/**
 * The plan as a document rather than a stack of cards.
 *
 * A plan is something a person reads end to end and shows to someone else, so
 * it is set as a page: one column, real headings, a top line and a sign-off.
 * The card layout it replaces made every section look like a widget of equal
 * weight, and the objective — the one sentence that matters most — looked
 * like the rest.
 */
function PlanDocument({
  plan,
  t,
  tl,
}: {
  plan: Plan
  t: (k: Key, vars?: Record<string, string | number>) => string
  tl: (k: Key) => string[]
}) {
  return (
    <article className="plan-document">
      <div className="doc-topline">
        <span>{t("pm.docKicker")}</span>
        <span>{t("pm.docVersionLabel", { n: plan.version })}</span>
      </div>

      <div className="doc-body">
        <h2>{t("pm.docTitle")}</h2>

        {plan.revisionOf && (
          <p className="t-wrap" style={{ fontStyle: "italic", paddingLeft: 12, borderLeft: "2px solid var(--border2)" }}>
            {t("pm.planRevisionOf", { feedback: plan.revisionOf.feedback })}
          </p>
        )}

        <h3>{t("pm.objective")}</h3>
        <p className="t-wrap" style={{ fontSize: 17, lineHeight: 1.6, color: "var(--text)" }}>
          {plan.objective}
        </p>

        <h3>{t("pm.scope")}</h3>
        <div className="doc-meta">
          <div>
            <span>{t("pm.scopeIn")}</span>
            {plan.scope.included.length ? (
              <ul style={{ marginTop: 6 }}>
                {plan.scope.included.map((x, i) => <li key={i} className="t-wrap">{x}</li>)}
              </ul>
            ) : (
              <strong style={{ color: "var(--dim)", fontStyle: "italic" }}>— {t("pm.nothingDeclared")}</strong>
            )}
          </div>
          <div>
            <span>{t("pm.scopeOut")}</span>
            {plan.scope.excluded.length ? (
              <ul style={{ marginTop: 6 }}>
                {plan.scope.excluded.map((x, i) => <li key={i} className="t-wrap">{x}</li>)}
              </ul>
            ) : (
              <strong style={{ color: "var(--dim)", fontStyle: "italic" }}>— {t("pm.nothingDeclared")}</strong>
            )}
          </div>
        </div>

        {plan.milestones.length > 0 && (
          <>
            <h3>{t("pm.milestones")}</h3>
            {plan.milestones.map((m) => (
              <p key={m.name} className="t-wrap" style={{ marginBottom: 10 }}>
                <strong style={{ color: "var(--text)" }}>{m.name}</strong>
                <br />
                {m.outcome}
              </p>
            ))}
          </>
        )}

        {plan.tasks.length > 0 && (
          <>
            <h3>{t("pm.tasks")}</h3>
            <ul>
              {plan.tasks.map((task) => (
                <li key={task.id} className="t-wrap">
                  {task.title}
                  {task.dependsOn.length > 0 && (
                    <span style={{ color: "var(--dim)", fontSize: 12 }}>
                      {" "}— {t("pm.taskDepends")}:{" "}
                      {task.dependsOn
                        .map((id) => plan.tasks.find((x) => x.id === id)?.title ?? id)
                        .join("; ")}
                    </span>
                  )}
                </li>
              ))}
            </ul>
          </>
        )}

        {plan.risks.length > 0 && (
          <>
            <h3>{t("pm.risks")}</h3>
            {plan.risks.map((r, i) => (
              <p key={i} className="t-wrap" style={{ marginBottom: 10 }}>
                {r.statement}
                <br />
                <span style={{ color: "var(--dim)" }}>→ {r.mitigation}</span>
              </p>
            ))}
          </>
        )}

        {/* Never collapsed and never last-but-forgotten: a plan that hides
            what it does not know reads as more certain than it is. */}
        <h3 style={{ color: "var(--amber)", borderColor: "rgba(245,158,11,.3)" }}>
          {t("pm.openQuestions")}
        </h3>
        <ul style={{ color: "var(--amber)" }}>
          {plan.openQuestions.map((q, i) => <li key={i} className="t-wrap">{q}</li>)}
        </ul>

        {plan.doneCriteria.length > 0 && (
          <>
            <h3>{t("pm.doneCriteria")}</h3>
            <ul>
              {plan.doneCriteria.map((d, i) => <li key={i} className="t-wrap">{d}</li>)}
            </ul>
          </>
        )}

        {plan.entities && plan.entities.length > 0 && (
          <>
            <h3>{t("pm.entities")}</h3>
            <ul>
              {plan.entities.map((e) => (
                <li key={e.name} className="t-wrap">
                  <strong style={{ color: "var(--text)" }}>{e.name}</strong> — {e.description}
                </li>
              ))}
            </ul>
          </>
        )}

        {plan.flows && plan.flows.length > 0 && (
          <>
            <h3>{t("pm.flows")}</h3>
            {plan.flows.map((f) => (
              <div key={f.name} style={{ marginBottom: 10 }}>
                <strong style={{ color: "var(--text)" }}>{f.name}</strong>
                <ol style={{ marginTop: 4 }}>
                  {f.steps.map((x, i) => <li key={i} className="t-wrap">{x}</li>)}
                </ol>
              </div>
            ))}
          </>
        )}

        {plan.constraints && plan.constraints.length > 0 && (
          <>
            <h3>{t("pm.constraints")}</h3>
            <ul>
              {plan.constraints.map((c, i) => (
                <li key={i} className="t-wrap">
                  <span style={{ fontFamily: "var(--mono)", fontSize: 11, color: "var(--dim)" }}>{c.kind}</span>{" "}
                  {c.statement}
                </li>
              ))}
            </ul>
          </>
        )}
      </div>

      <div className="doc-signoff">
        <span>{t("pm.preparedBy")}</span>
        <strong>{t("pm.preparedByValue")}</strong>
        <span>{t("pm.docStatus")}</span>
      </div>
    </article>
  )
}
