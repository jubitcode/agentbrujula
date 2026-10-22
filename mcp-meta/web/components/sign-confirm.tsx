"use client"

/**
 * What you are about to sign, in words, before the wallet opens.
 *
 * Freighter shows a Soroban transaction: accurate, and unreadable unless you
 * already know what you are looking at. Someone clicking "pay" deserves the
 * amount and the consequences in a sentence first — especially here, where
 * the figure that gets locked is the **ceiling**, not the estimate, and those
 * two numbers are different.
 *
 * It states both, because the gap between them is real and hiding it is how
 * "why did it take 0.45 when it said 0.10" happens. Nothing is computed here:
 * every number comes from the same values passed to `buildPayment`, so this
 * panel cannot describe a transaction other than the one about to be sent.
 */

import { useEffect, useRef } from "react"
import { useI18n } from "@/lib/i18n"

export type SignDetails = {
  agent: string
  /** What gets locked — the ceiling. */
  amount: string
  /** What it is expected to actually cost, when the server offered one. */
  estimated: string | null
  network: string
  payTo: string
  task: string
  resolve: (confirmed: boolean) => void
}

export function SignConfirm({
  details,
  onDecide,
}: {
  details: SignDetails
  onDecide: (confirmed: boolean) => void
}) {
  const { t, tl } = useI18n()
  const cancelRef = useRef<HTMLButtonElement>(null)

  // Focus lands on Cancel, not on Confirm: the safe option should be the one
  // a stray keypress hits. Escape cancels, for the same reason.
  useEffect(() => {
    cancelRef.current?.focus()
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onDecide(false)
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [onDecide])

  const differs = details.estimated && details.estimated !== details.amount

  return (
    <div className="sign-overlay" role="dialog" aria-modal="true" aria-labelledby="sign-title">
      <div className="sign-modal">
        <span className="section-label" style={{ marginBottom: 6, display: "block" }}>
          {t("sign.kicker")}
        </span>
        <h2 id="sign-title" className="t-h2" style={{ fontSize: 21 }}>
          {t("sign.title", { amount: details.amount })}
        </h2>
        <p className="t-small" style={{ margin: "0 0 16px" }}>
          {t("sign.subtitle", { agent: details.agent })}
        </p>

        <div className="sign-flow">
          <div className="row">
            <span>{t("sign.locks")}</span>
            <strong>{details.amount} USDC</strong>
          </div>
          {differs && (
            <div className="row">
              <span>{t("sign.estimated")}</span>
              <strong>{details.estimated} USDC</strong>
            </div>
          )}
          <div className="row">
            <span>{t("sign.network")}</span>
            <strong>{details.network}</strong>
          </div>
          {details.payTo && (
            <div className="row">
              <span>{t("sign.escrow")}</span>
              <strong className="mono-id">
                {details.payTo.slice(0, 6)}…{details.payTo.slice(-6)}
              </strong>
            </div>
          )}
        </div>

        {/* The gap between ceiling and estimate, said plainly rather than
            discovered later in a wallet. */}
        {differs && (
          <p className="t-small" style={{ color: "var(--amber)", margin: "0 0 14px" }}>
            {t("sign.ceilingNote", { amount: details.amount, estimated: details.estimated! })}
          </p>
        )}

        <p className="field-label">{t("sign.whatYouAuthorise")}</p>
        <ul className="sign-list">
          {tl("sign.authoriseList").map((item) => (
            <li key={item}>{item}</li>
          ))}
        </ul>

        <div className="sign-actions">
          <button ref={cancelRef} className="btn-ghost" onClick={() => onDecide(false)}>
            {t("common.cancel")}
          </button>
          <button className="btn-primary" onClick={() => onDecide(true)}>
            {t("sign.confirm")} →
          </button>
        </div>
      </div>
    </div>
  )
}
