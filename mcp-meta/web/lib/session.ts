/**
 * The session token, verified on the server.
 *
 * The marketplace issues a JWT when someone signs a wallet challenge
 * (`agent/src/research_agent/session_tokens.py`): HS256, claims `sub` (the
 * Stellar address), `iat`, `exp` and `jti`. This verifies that token inside
 * Next, so a route can know *who* is calling before doing anything expensive.
 *
 * Why verify here rather than asking the marketplace: `/api/pm` does not talk
 * to the marketplace at all — it goes to mcp-meta. Adding a round trip to a
 * third service to answer "is this person real" would make every plan slower
 * and give the PM a new way to break. The secret is already shared (both
 * processes read `SESSION_TOKEN_SECRET` from the same `.env`), so an HMAC
 * check is enough, and it is twenty lines of `node:crypto` rather than a
 * dependency.
 *
 * Security notes, because this file is the lock:
 *
 *   - The algorithm is **fixed** at HS256 and the header's `alg` is ignored.
 *     Trusting it is how `{"alg":"none"}` forgeries get in.
 *   - The signature comparison is constant-time.
 *   - A token with no `sub` is rejected: the whole point is knowing who is
 *     asking, and the per-wallet limit has nothing to key on without it.
 */

import { createHmac, timingSafeEqual } from "node:crypto"

/** Same cookie name the session route sets. */
export const SESSION_COOKIE = "brujula_session"

export type Session = {
  /** The Stellar address that signed the challenge. */
  address: string
  /** Expiry, seconds since the epoch. */
  expiresAt: number
}

/**
 * Why a token was refused.
 *
 * Kept separate from the message shown to a person: "no_session" means
 * "sign in", and "expired" means "sign in again", which are different
 * sentences even though both end in a 401.
 */
export type SessionFailure =
  | "no_session"
  | "malformed"
  | "bad_signature"
  | "expired"
  | "no_subject"
  | "no_secret"

export type SessionResult =
  | { ok: true; session: Session }
  | { ok: false; reason: SessionFailure }

function base64urlToBuffer(value: string): Buffer {
  // JWT uses base64url without padding; Buffer wants the padding back.
  const padded = value.replace(/-/g, "+").replace(/_/g, "/")
  return Buffer.from(padded.padEnd(Math.ceil(padded.length / 4) * 4, "="), "base64")
}

/**
 * The shared secret.
 *
 * Read on every call rather than at module load so a missing variable is a
 * clean 401 with a named reason instead of a crash at import time — and so
 * the dev server does not have to restart after the variable is added.
 */
function secret(): string | null {
  const value = process.env.SESSION_TOKEN_SECRET?.trim()
  return value && value.length >= 32 ? value : null
}

/**
 * Verify a token and return who it belongs to.
 *
 * Never throws: every failure is a named reason, because a route that has to
 * wrap this in try/catch will eventually forget to.
 */
export function verifySession(token: string | undefined | null): SessionResult {
  if (!token) return { ok: false, reason: "no_session" }

  const key = secret()
  if (!key) return { ok: false, reason: "no_secret" }

  const parts = token.split(".")
  if (parts.length !== 3) return { ok: false, reason: "malformed" }
  const [headerB64, payloadB64, signatureB64] = parts

  // HS256, fixed. The header is not consulted on purpose.
  const expected = createHmac("sha256", key)
    .update(`${headerB64}.${payloadB64}`)
    .digest()
  const given = base64urlToBuffer(signatureB64)

  // `timingSafeEqual` throws on a length mismatch, which is itself a signal
  // worth answering plainly rather than leaking as an exception.
  if (given.length !== expected.length) return { ok: false, reason: "bad_signature" }
  if (!timingSafeEqual(given, expected)) return { ok: false, reason: "bad_signature" }

  let payload: { sub?: unknown; exp?: unknown }
  try {
    payload = JSON.parse(base64urlToBuffer(payloadB64).toString("utf8"))
  } catch {
    return { ok: false, reason: "malformed" }
  }

  const exp = typeof payload.exp === "number" ? payload.exp : 0
  // Checked after the signature: an expired token whose signature is forged
  // should read as forged, not as expired.
  if (!exp || Date.now() / 1000 >= exp) return { ok: false, reason: "expired" }

  const sub = typeof payload.sub === "string" ? payload.sub.trim() : ""
  if (!sub) return { ok: false, reason: "no_subject" }

  return { ok: true, session: { address: sub, expiresAt: exp } }
}

/**
 * Where to send someone back to after they sign in.
 *
 * The caller gets this from a query parameter, which means it comes from
 * whoever wrote the link — including a stranger. An unchecked value here is
 * the classic open redirect: `/login?volver=https://sitio-falso` turns our
 * sign-in page into a springboard that lends it our domain's credibility.
 *
 * So only same-origin paths survive, and "same-origin path" is narrower
 * than "starts with a slash":
 *
 *   `//evil.com`   is a protocol-relative URL — the browser reads it as
 *                  `https://evil.com`.
 *   `/\evil.com`   several browsers normalise the backslash to a slash and
 *                  treat it the same way.
 *   `https://…`    an absolute URL, ours or not.
 *
 * Anything that is not plainly one of our own paths falls back to `fallback`.
 */
export function safeReturnTo(value: string | undefined | null, fallback = "/agentes"): string {
  if (!value) return fallback

  let decoded = value
  try {
    decoded = decodeURIComponent(value)
  } catch {
    // Malformed percent-encoding: not something we are going to navigate to.
    return fallback
  }

  const path = decoded.trim()
  if (!path.startsWith("/")) return fallback
  // Both of these are read as "another host" by at least one browser.
  if (path.startsWith("//") || path.startsWith("/\\")) return fallback
  // A control character can be used to smuggle one of the above past a
  // naive check; nothing legitimate here contains one.
  if (/[\u0000-\u001f\u007f]/.test(path)) return fallback

  return path
}
