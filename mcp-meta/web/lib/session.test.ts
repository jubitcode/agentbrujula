/**
 * The session verifier is the lock on the PM, so it gets tested as a lock:
 * not "does it let the right token in" but "what does it refuse".
 *
 * The tokens here are minted with the same algorithm the marketplace uses
 * (`agent/src/research_agent/session_tokens.py`: HS256 over base64url) rather
 * than mocked, so a change on either side that breaks the pairing fails here.
 */

import { createHmac } from "node:crypto"

const SECRET = "test-session-secret-at-least-32-characters-long"

function b64url(input: string | Buffer): string {
  return Buffer.from(input).toString("base64url")
}

/** Mint a token the way the Python side does. */
function mint(
  payload: Record<string, unknown>,
  { secret = SECRET, signature }: { secret?: string; signature?: string } = {},
): string {
  const header = b64url(JSON.stringify({ alg: "HS256", typ: "JWT" }))
  const body = b64url(JSON.stringify(payload))
  const sig =
    signature ?? createHmac("sha256", secret).update(`${header}.${body}`).digest("base64url")
  return `${header}.${body}.${sig}`
}

const inAnHour = () => Math.floor(Date.now() / 1000) + 3600
const anHourAgo = () => Math.floor(Date.now() / 1000) - 3600

const WALLET = "GTESTWALLET234567ABCDEFGHIJKLMNOPQRSTUVWXYZ234567ABC"

describe("verifySession", () => {
  let verifySession: typeof import("./session").verifySession

  beforeEach(() => {
    jest.resetModules()
    process.env.SESSION_TOKEN_SECRET = SECRET
    verifySession = require("./session").verifySession
  })

  test("accepts a token signed with the shared secret", () => {
    const result = verifySession(mint({ sub: WALLET, exp: inAnHour() }))
    expect(result.ok).toBe(true)
    if (result.ok) expect(result.session.address).toBe(WALLET)
  })

  test("refuses a missing token", () => {
    expect(verifySession(undefined)).toEqual({ ok: false, reason: "no_session" })
    expect(verifySession("")).toEqual({ ok: false, reason: "no_session" })
  })

  test("refuses anything that is not three parts", () => {
    expect(verifySession("not-a-jwt")).toEqual({ ok: false, reason: "malformed" })
    expect(verifySession("only.two")).toEqual({ ok: false, reason: "malformed" })
  })

  /** The point of the whole file: a payload nobody signed must not get in. */
  test("refuses a tampered payload", () => {
    const good = mint({ sub: WALLET, exp: inAnHour() })
    const [header, , signature] = good.split(".")
    const forged = b64url(JSON.stringify({ sub: "GATTACKER", exp: inAnHour() }))
    expect(verifySession(`${header}.${forged}.${signature}`)).toEqual({
      ok: false,
      reason: "bad_signature",
    })
  })

  test("refuses a token signed with a different secret", () => {
    const other = mint({ sub: WALLET, exp: inAnHour() }, { secret: "a-completely-different-secret-32-chars" })
    expect(verifySession(other)).toEqual({ ok: false, reason: "bad_signature" })
  })

  test("refuses a signature of the wrong length rather than throwing", () => {
    // `timingSafeEqual` throws when the buffers differ in length; that must
    // surface as a refusal, not as a 500.
    const short = mint({ sub: WALLET, exp: inAnHour() }, { signature: b64url("short") })
    expect(() => verifySession(short)).not.toThrow()
    expect(verifySession(short)).toEqual({ ok: false, reason: "bad_signature" })
  })

  /**
   * The classic JWT forgery: claim there is no algorithm and send no
   * signature. The header is ignored and HS256 is fixed, so this is just a
   * bad signature.
   */
  test("ignores the header's alg claim", () => {
    const header = b64url(JSON.stringify({ alg: "none", typ: "JWT" }))
    const body = b64url(JSON.stringify({ sub: WALLET, exp: inAnHour() }))
    expect(verifySession(`${header}.${body}.`)).toEqual({ ok: false, reason: "bad_signature" })
  })

  test("refuses an expired token", () => {
    expect(verifySession(mint({ sub: WALLET, exp: anHourAgo() }))).toEqual({
      ok: false,
      reason: "expired",
    })
  })

  test("refuses a token with no expiry", () => {
    expect(verifySession(mint({ sub: WALLET }))).toEqual({ ok: false, reason: "expired" })
  })

  /** Without a subject there is nobody to rate-limit, so it is not a session. */
  test("refuses a token with no subject", () => {
    expect(verifySession(mint({ exp: inAnHour() }))).toEqual({ ok: false, reason: "no_subject" })
    expect(verifySession(mint({ sub: "   ", exp: inAnHour() }))).toEqual({
      ok: false,
      reason: "no_subject",
    })
  })

  /**
   * A server with no secret must refuse everything rather than accept
   * anything — and say which it is, so a misconfiguration does not read as
   * an attack.
   */
  test("refuses everything when the secret is missing or too short", () => {
    jest.resetModules()
    delete process.env.SESSION_TOKEN_SECRET
    const { verifySession: noSecret } = require("./session")
    expect(noSecret(mint({ sub: WALLET, exp: inAnHour() }))).toEqual({
      ok: false,
      reason: "no_secret",
    })

    jest.resetModules()
    process.env.SESSION_TOKEN_SECRET = "too-short"
    const { verifySession: shortSecret } = require("./session")
    expect(shortSecret(mint({ sub: WALLET, exp: inAnHour() }))).toEqual({
      ok: false,
      reason: "no_secret",
    })
  })

  test("an expired token with a forged signature reads as forged", () => {
    const [header, body] = mint({ sub: WALLET, exp: anHourAgo() }).split(".")
    expect(verifySession(`${header}.${body}.${b64url("wrong-signature-entirely")}`)).toEqual({
      ok: false,
      reason: "bad_signature",
    })
  })
})

/**
 * `safeReturnTo` decides where someone lands after signing in, from a value
 * that arrives in the URL — which is to say, from anyone who can write a
 * link. These are the shapes that turn a sign-in page into someone else's
 * front door.
 */
describe("safeReturnTo", () => {
  let safeReturnTo: typeof import("./session").safeReturnTo

  beforeEach(() => {
    jest.resetModules()
    safeReturnTo = require("./session").safeReturnTo
  })

  test("keeps our own paths", () => {
    expect(safeReturnTo("/agentes")).toBe("/agentes")
    expect(safeReturnTo("/pm")).toBe("/pm")
    expect(safeReturnTo("/escrows?x=1")).toBe("/escrows?x=1")
  })

  test("falls back when there is nothing to return to", () => {
    expect(safeReturnTo(undefined)).toBe("/agentes")
    expect(safeReturnTo(null)).toBe("/agentes")
    expect(safeReturnTo("")).toBe("/agentes")
    expect(safeReturnTo("", "/pm")).toBe("/pm")
  })

  test("refuses absolute URLs", () => {
    expect(safeReturnTo("https://sitio-falso.com")).toBe("/agentes")
    expect(safeReturnTo("http://sitio-falso.com")).toBe("/agentes")
    // Even our own, spelled absolutely: there is no reason to need it.
    expect(safeReturnTo("https://agentbrujula.com/pm")).toBe("/agentes")
  })

  /** The one that looks like a path and is not. */
  test("refuses protocol-relative URLs", () => {
    expect(safeReturnTo("//sitio-falso.com")).toBe("/agentes")
    expect(safeReturnTo("//sitio-falso.com/pm")).toBe("/agentes")
  })

  /** Normalised to a slash by several browsers. */
  test("refuses the backslash variant", () => {
    expect(safeReturnTo("/\\sitio-falso.com")).toBe("/agentes")
  })

  test("refuses a path carrying control characters", () => {
    expect(safeReturnTo("/agentes\n")).toBe("/agentes")
    expect(safeReturnTo("/\t/sitio-falso.com")).toBe("/agentes")
  })

  /** Encoding is not a way around any of the above. */
  test("decodes before deciding", () => {
    expect(safeReturnTo(encodeURIComponent("//sitio-falso.com"))).toBe("/agentes")
    expect(safeReturnTo("%2F%2Fsitio-falso.com")).toBe("/agentes")
    expect(safeReturnTo("%2Fagentes")).toBe("/agentes")
  })

  test("refuses malformed encoding rather than guessing", () => {
    expect(safeReturnTo("/%E0%A4%A")).toBe("/agentes")
  })
})
