/**
 * The contract currently in flight.
 *
 * The escrow bar has to survive a navigation — someone locks funds on /hire
 * and then goes to read the plan — so this is the smallest thing that can
 * carry it across screens: one record in `sessionStorage` plus an event so
 * anything rendering the bar re-reads it.
 *
 * `sessionStorage`, not a server call: /api/escrows returns every escrow on
 * the deployment and carries no wallet, so there is no way to ask "which of
 * these is mine". Until the endpoint says whose a row is, the honest source
 * is what this tab did. It is cleared when the work resolves, and on tab
 * close, which is the right lifetime for a thing that says "in progress".
 */

const KEY = "brujula_active_escrow_v1"
const EVENT = "brujula:escrow-changed"

/** Where a hire is. Mirrors the steps the bar draws. */
export type EscrowStage = "locked" | "working" | "settled" | "refunded"

export type ActiveEscrow = {
  taskId: string
  agent: string
  /** USDC, as the string the API returned — never re-parsed into a float. */
  amount: string
  stage: EscrowStage
  txUrl?: string
  startedAt: number
}

export function readActiveEscrow(): ActiveEscrow | null {
  if (typeof window === "undefined") return null
  try {
    const raw = sessionStorage.getItem(KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw)
    return typeof parsed?.taskId === "string" ? (parsed as ActiveEscrow) : null
  } catch {
    return null
  }
}

export function setActiveEscrow(escrow: ActiveEscrow): void {
  if (typeof window === "undefined") return
  try {
    sessionStorage.setItem(KEY, JSON.stringify(escrow))
    window.dispatchEvent(new CustomEvent(EVENT))
  } catch {
    // Private mode, or storage disabled. The hire still works; it just has
    // no bar following it around.
  }
}

export function updateEscrowStage(stage: EscrowStage): void {
  const current = readActiveEscrow()
  if (current) setActiveEscrow({ ...current, stage })
}

export function clearActiveEscrow(): void {
  if (typeof window === "undefined") return
  try {
    sessionStorage.removeItem(KEY)
    window.dispatchEvent(new CustomEvent(EVENT))
  } catch {
    // Nothing to do: there is no state to be wrong about.
  }
}

/** Subscribe to changes, including those made by this same tab. */
export function onEscrowChange(handler: () => void): () => void {
  if (typeof window === "undefined") return () => {}
  window.addEventListener(EVENT, handler)
  // `storage` only fires in *other* tabs, so both are needed to cover both.
  window.addEventListener("storage", handler)
  return () => {
    window.removeEventListener(EVENT, handler)
    window.removeEventListener("storage", handler)
  }
}
