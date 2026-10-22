"use client"

/**
 * The contract step, before the brief.
 *
 * Terms, then a deposit, then the work. It is the cycle `/hire` performs for
 * real against the Soroban contract — shown here for the PM, which has no
 * on-chain price, so every figure is a demonstration and says so.
 *
 * The deposit "runs" in steps rather than completing instantly. Not theatre:
 * the point of the demonstration is the *sequence* — agreement first, funds
 * second, work third — and an instant jump from button to brief shows none
 * of it.
 */

import { useCallback, useState } from "react"
import { DEMO_PRICE_USDC, openContract } from "@/lib/pm-contract"
import { useI18n } from "@/lib/i18n"
import { DemoNotice } from "@/components/demo-notice"

const NETWORK =
  (process.env.NEXT_PUBLIC_STELLAR_NETWORK || "TESTNET").toUpperCase() === "PUBLIC"
    ? "Mainnet"
    : "Testnet"

type Phase = "terms" | "depositing" | "done"

export function PmContract({ onStart }: { onStart: () => void }) {
  const { t, tl } = useI18n()
  const [phase, setPhase] = useState<Phase>("terms")
  const [step, setStep] = useState(0)

  /**
   * Two steps, then the receipt.
   *
   * The contract is **not** written here. Writing it fires the change event,
   * the parent sees a contract and swaps the view, and the receipt — the
   * screen that says what just happened — is gone before anyone reads it.
   * It is created when they press on, below.
   */
  const deposit = useCallback(() => {
    setPhase("depositing")
    setStep(1)
    window.setTimeout(() => setStep(2), 900)
    window.setTimeout(() => setPhase("done"), 1900)
  }, [])

  /** Now the contract exists, and the parent may move on. */
  const start = useCallback(() => {
    openContract("pm-planner")
    onStart()
  }, [onStart])

  return (
    <div className="page">
      <section style={{ paddingBottom: 20 }}>
        <div className="section-label">{`// ${t("contract.kicker")}`}</div>
        <h1 className="t-hero">{t("contract.title")}</h1>
        <p className="t-intro">{t("contract.intro")}</p>
      </section>

      <DemoNotice />

      <section className="contract-card">
        {phase === "terms" && (
          <>
            <div className="field-label">{t("contract.terms")}</div>
            <div className="contract-terms">
              <div className="contract-line">
                <span>{t("contract.agent")}</span>
                <strong>PM Planner</strong>
              </div>
              <div className="contract-line">
                <span>{t("contract.price")}</span>
                <strong className="contract-price">
                  {DEMO_PRICE_USDC} <small>USDC · {t("contract.priceNote")}</small>
                </strong>
              </div>
              <div className="contract-line">
                <span>{t("contract.deliverable")}</span>
                <strong style={{ fontWeight: 400, color: "var(--muted)", maxWidth: "60ch" }}>
                  {t("contract.deliverableValue")}
                </strong>
              </div>
              <div className="contract-line">
                <span>{t("contract.revisions")}</span>
                <strong style={{ fontWeight: 400, color: "var(--muted)" }}>
                  {t("contract.revisionsValue")}
                </strong>
              </div>
              <div className="contract-line">
                <span>{t("contract.network")}</span>
                <strong>{NETWORK}</strong>
              </div>
            </div>

            <div className="field-label">{t("contract.checks")}</div>
            <div className="contract-checks">
              {tl("contract.checksList").map((line) => (
                <div key={line} className="contract-check">
                  <span aria-hidden="true">✓</span>
                  {line}
                </div>
              ))}
            </div>

            <button className="btn-primary btn-inline" onClick={deposit}>
              {t("contract.deposit")} →
            </button>
          </>
        )}

        {phase === "depositing" && (
          <>
            <div className="progress-head">
              <span>{t("contract.depositing")}</span>
              <span style={{ fontFamily: "var(--mono)" }}>{step * 50}%</span>
            </div>
            <div className="progress-track">
              <div className="progress-bar" style={{ width: `${step * 50}%` }} />
            </div>
            <p className="t-small" style={{ margin: 0 }}>
              {step === 1 ? t("contract.step1") : t("contract.step2", { amount: DEMO_PRICE_USDC })}
            </p>
            <p className="t-small" style={{ color: "var(--dim)", margin: "4px 0 0" }}>
              {step === 1 ? t("contract.step1detail") : t("contract.step2detail")}
            </p>
          </>
        )}

        {phase === "done" && (
          <>
            <div className="progress-head">
              <span style={{ color: "var(--green)" }}>✓ {t("contract.depositDone")}</span>
              <span style={{ fontFamily: "var(--mono)" }}>100%</span>
            </div>
            <div className="progress-track">
              <div className="progress-bar" style={{ width: "100%", background: "var(--green)" }} />
            </div>
            <p className="t-small" style={{ margin: "0 0 18px" }}>
              {t("contract.depositDoneDetail")}
            </p>
            <button className="btn-primary btn-inline" onClick={start}>
              {t("contract.toBrief")} →
            </button>
          </>
        )}
      </section>
    </div>
  )
}
