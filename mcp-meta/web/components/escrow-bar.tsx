"use client"

/**
 * A small panel that follows you while a contract is open.
 *
 * From the prototype, and it earns its place: once funds are locked, "where
 * is my money right now" is the question, and the answer used to require
 * navigating to /escrows and finding the row.
 *
 * It renders nothing when there is no contract in flight, which is most of
 * the time — a bar that is always there stops being read.
 */

import { useEffect, useState } from "react"
import Link from "next/link"
import {
  clearActiveEscrow,
  onEscrowChange,
  readActiveEscrow,
  type ActiveEscrow,
  type EscrowStage,
} from "@/lib/active-escrow"
import { useI18n, type Key } from "@/lib/i18n"

/** The four steps, in order, so the current one can be highlighted. */
const STAGES: { stage: EscrowStage; key: Key }[] = [
  { stage: "locked", key: "escrowBar.locked" },
  { stage: "working", key: "escrowBar.working" },
  { stage: "settled", key: "escrowBar.settled" },
]

export function EscrowBar() {
  const { t } = useI18n()
  const [escrow, setEscrow] = useState<ActiveEscrow | null>(null)

  useEffect(() => {
    const read = () => setEscrow(readActiveEscrow())
    read()
    return onEscrowChange(read)
  }, [])

  // A floating panel covers whatever is under it, and what is under it at
  // the bottom of a page is usually the last thing you need to reach. The
  // content reserves the height instead of fighting for it.
  useEffect(() => {
    document.body.classList.toggle("has-escrow-bar", !!escrow)
    return () => document.body.classList.remove("has-escrow-bar")
  }, [escrow])

  if (!escrow) return null

  const index = STAGES.findIndex((s) => s.stage === escrow.stage)
  const refunded = escrow.stage === "refunded"

  return (
    <aside className="escrow-bar" aria-label={t("escrowBar.title")}>
      <div className="escrow-bar-id">
        {t("escrowBar.title")} · {escrow.taskId.slice(0, 8)}
      </div>

      <div style={{ fontSize: 13, fontWeight: 600 }}>{escrow.agent}</div>

      <div className="escrow-bar-amount">
        {t("escrowBar.amount")} <b>{escrow.amount} USDC</b>
      </div>

      <div className="escrow-bar-flow">
        {refunded ? (
          <span className="active">{t("escrowBar.refunded")}</span>
        ) : (
          STAGES.map((s, i) => (
            <span key={s.stage} className={i <= index ? "active" : undefined}>
              {t(s.key)}
            </span>
          ))
        )}
      </div>

      <Link href="/escrows" className="btn-ghost btn-sm" style={{ textDecoration: "none", width: "100%" }}>
        {t("escrowBar.details")}
      </Link>

      {/* Dismissable once it has resolved. While funds are still locked the
          bar stays: hiding an open contract is how one gets forgotten. */}
      {(escrow.stage === "settled" || refunded) && (
        <button
          onClick={clearActiveEscrow}
          className="t-small"
          style={{ background: "none", border: 0, color: "var(--dim)", cursor: "pointer", padding: 0 }}
        >
          {t("common.close")}
        </button>
      )}
    </aside>
  )
}
