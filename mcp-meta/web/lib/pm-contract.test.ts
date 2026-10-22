/**
 * The simulated contract, and the one property that must never slip:
 * everything it produces is marked `demo: true`.
 *
 * Brújula's escrow is real on testnet. This is a demonstration of the same
 * cycle for an agent that has no on-chain price. The danger is not that the
 * simulation breaks — it is that six months from now something reads this
 * state and renders it as a settlement. The flag is what stops that, so it
 * gets a test of its own.
 */

import {
  DEMO_PRICE_USDC,
  advance,
  clearContract,
  openContract,
  readContract,
} from "./pm-contract"

const KEY = "brujula_demo_pm_contract_v1"

describe("el contrato simulado del PM", () => {
  beforeEach(() => {
    sessionStorage.clear()
  })

  test("nace marcado como demostración", () => {
    const c = openContract("pm-planner")
    expect(c.demo).toBe(true)
    expect(readContract()?.demo).toBe(true)
  })

  /** Anything not carrying the flag is not ours and is not trusted. */
  test("rechaza lo que no esté marcado como demostración", () => {
    sessionStorage.setItem(
      KEY,
      JSON.stringify({ id: "x", stage: "released", amount: "999", agent: "a", events: [] }),
    )
    expect(readContract()).toBeNull()

    sessionStorage.setItem(KEY, JSON.stringify({ demo: "sí", stage: "released" }))
    expect(readContract()).toBeNull()
  })

  test("ignora un valor corrupto en vez de romper la pantalla", () => {
    sessionStorage.setItem(KEY, "{no es json")
    expect(readContract()).toBeNull()
  })

  /** The id must not read like something you could look up on a chain. */
  test("el identificador se anuncia como demo", () => {
    expect(openContract("pm-planner").id.startsWith("demo-")).toBe(true)
  })

  test("usa el precio de demostración", () => {
    expect(openContract("pm-planner").amount).toBe(DEMO_PRICE_USDC)
  })

  test("recorre el ciclo y va dejando registro", () => {
    openContract("pm-planner")
    advance("working", "Trabajando", "El agente empezó.")
    advance("delivered", "Entregado", "El plan está listo.")
    const c = advance("released", "Aprobado", "El pago se liberaría.")

    expect(c?.stage).toBe("released")
    // The deposit plus the three steps, in order.
    expect(c?.events).toHaveLength(4)
    expect(c?.events.map((e) => e.title)).toEqual([
      "Depósito simulado",
      "Trabajando",
      "Entregado",
      "Aprobado",
    ])
    expect(c?.demo).toBe(true)
  })

  test("avanzar sin contrato no crea uno", () => {
    expect(advance("released", "x", "y")).toBeNull()
    expect(readContract()).toBeNull()
  })

  test("se puede cerrar y empezar de nuevo", () => {
    openContract("pm-planner")
    clearContract()
    expect(readContract()).toBeNull()
  })
})
