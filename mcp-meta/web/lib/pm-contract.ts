/**
 * The PM's contract — **simulated**, and deliberately kept apart from the
 * real one.
 *
 * Brújula's escrow is not a mock: `/hire` locks real USDC in a Soroban
 * contract on testnet, and `lib/active-escrow.ts` tracks that. This file
 * tracks something else entirely — a demonstration of the same cycle for the
 * PM, which has no on-chain price and never touches the chain.
 *
 * They are separate files on purpose. One mock sitting next to the real
 * thing, sharing its types, is how a demo ends up being presented as a
 * settlement six months later. Everything here says `demo` in its name, and
 * every screen that reads it is required to say so on screen too.
 *
 * What is simulated: the money, the deposit, the release. What is NOT
 * simulated: the plan. The agent really runs, really charges our model
 * budget, and really writes what you read.
 */

/** The price shown for the PM. A demonstration figure, not an on-chain rate.
 *
 * The other agents price themselves from `rate_stroops` in the registry;
 * the PM has no entry there, which is precisely why this is a demo. */
export const DEMO_PRICE_USDC = "2.00"

/**
 * Where the simulated contract is.
 *
 * `delivered` is the interesting one: the plan exists and the money has not
 * moved. That is the whole point of escrow, and the step a demo most needs
 * to show.
 */
export type DemoStage =
  | "none"
  | "deposited"
  | "working"
  | "delivered"
  | "released"
  | "disputed"

export type DemoContract = {
  /** Always true. Read it before showing anything as settled. */
  demo: true
  id: string
  stage: DemoStage
  amount: string
  agent: string
  startedAt: number
  /** Appended as the cycle advances, for the activity list. */
  events: { at: number; title: string; detail: string }[]
}

const KEY = "brujula_demo_pm_contract_v1"
const EVENT = "brujula:demo-contract-changed"

export function readContract(): DemoContract | null {
  if (typeof window === "undefined") return null
  try {
    const raw = sessionStorage.getItem(KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw)
    return parsed?.demo === true ? (parsed as DemoContract) : null
  } catch {
    return null
  }
}

function write(contract: DemoContract | null) {
  if (typeof window === "undefined") return
  try {
    if (contract) sessionStorage.setItem(KEY, JSON.stringify(contract))
    else sessionStorage.removeItem(KEY)
    window.dispatchEvent(new CustomEvent(EVENT))
  } catch {
    // Storage disabled. The demo still runs in memory for this render.
  }
}

/** Open a simulated contract. Called when the deposit animation finishes. */
export function openContract(agent: string): DemoContract {
  const contract: DemoContract = {
    demo: true,
    // Shaped like a task id so the screens can render it the same way,
    // but random and local: it corresponds to nothing on any chain.
    id: `demo-${Math.random().toString(16).slice(2, 10)}`,
    stage: "deposited",
    amount: DEMO_PRICE_USDC,
    agent,
    startedAt: Date.now(),
    events: [
      {
        at: Date.now(),
        title: "Depósito simulado",
        detail: `${DEMO_PRICE_USDC} USDC quedarían bloqueados en el contrato.`,
      },
    ],
  }
  write(contract)
  return contract
}

export function advance(stage: DemoStage, title: string, detail: string): DemoContract | null {
  const current = readContract()
  if (!current) return null
  const next: DemoContract = {
    ...current,
    stage,
    events: [...current.events, { at: Date.now(), title, detail }],
  }
  write(next)
  return next
}

export function clearContract(): void {
  write(null)
}

/** Subscribe to changes, including those made by this same tab. */
export function onContractChange(handler: () => void): () => void {
  if (typeof window === "undefined") return () => {}
  window.addEventListener(EVENT, handler)
  window.addEventListener("storage", handler)
  return () => {
    window.removeEventListener(EVENT, handler)
    window.removeEventListener("storage", handler)
  }
}
