"use client"

/**
 * Where the simulated contract is, and what happened so far.
 *
 * The step row is the part worth having: it shows the money sitting still
 * while the work moves, which is the one thing a paragraph about escrow
 * never manages to convey.
 */

import { useI18n, type Key } from "@/lib/i18n"
import type { DemoContract, DemoStage } from "@/lib/pm-contract"

/** The cycle, in order. `disputed` is a branch, not a step. */
const STAGES: { stage: DemoStage; key: Key }[] = [
  { stage: "deposited", key: "contract.stage.deposited" },
  { stage: "working", key: "contract.stage.working" },
  { stage: "delivered", key: "contract.stage.delivered" },
  { stage: "released", key: "contract.stage.released" },
]

export function ContractState({ contract }: { contract: DemoContract }) {
  const { t } = useI18n()
  const index = STAGES.findIndex((s) => s.stage === contract.stage)
  const disputed = contract.stage === "disputed"

  return (
    <div className="card" style={{ marginBottom: 18 }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: 10 }}>
        <span className="field-label" style={{ margin: 0 }}>{t("contract.state")}</span>
        <span className="demo-badge">{t("demo.badge")}</span>
      </div>

      <div style={{ display: "flex", alignItems: "baseline", gap: 6, marginTop: 10 }}>
        <strong style={{ fontFamily: "var(--mono)", fontSize: 19 }}>{contract.amount}</strong>
        <span className="t-small" style={{ margin: 0 }}>USDC</span>
      </div>

      <div className="stage-row">
        {disputed ? (
          <span className="stage-pill now">{t("contract.stage.disputed")}</span>
        ) : (
          STAGES.map((s, i) => (
            <span
              key={s.stage}
              className={`stage-pill${i < index ? " done" : i === index ? " now" : ""}`}
            >
              {t(s.key)}
            </span>
          ))
        )}
      </div>

      {contract.events.length > 0 && (
        <>
          <div className="field-label" style={{ marginTop: 16 }}>{t("contract.activity")}</div>
          <div className="activity">
            {/* Newest first: the current state is the question being asked. */}
            {[...contract.events].reverse().map((e, i) => (
              <div key={`${e.at}-${i}`} className="activity-item">
                <strong>{e.title}</strong>
                <span>{e.detail}</span>
              </div>
            ))}
          </div>
        </>
      )}
    </div>
  )
}
