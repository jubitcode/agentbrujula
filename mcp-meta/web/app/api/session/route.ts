/**
 * The session cookie.
 *
 * The browser already gets a JWT from the marketplace when someone signs the
 * wallet challenge (`lib/wallet-auth.ts`), and keeps it in `sessionStorage`
 * so it can send `Authorization: Bearer …` to the marketplace on port 5050.
 * That works for the marketplace and is useless to Next: a different origin's
 * `sessionStorage` is not something a Next route can read.
 *
 * So the same token is handed here once, and stored as an httpOnly cookie:
 *
 *   - `/api/pm` can read it on the server and know who is calling.
 *   - The app layout can check for a session before rendering.
 *   - Page scripts cannot read it, so an XSS cannot carry it off — which is
 *     the one thing `sessionStorage` cannot offer.
 *
 * The token is verified before it is stored. Accepting whatever the browser
 * posted would make the cookie a self-service pass.
 */

import { NextRequest, NextResponse } from "next/server"
import { SESSION_COOKIE, verifySession } from "@/lib/session"

/** Store a verified token. The browser calls this once per sign-in. */
export async function POST(request: NextRequest) {
  let body: { token?: unknown }
  try {
    body = await request.json()
  } catch {
    return NextResponse.json({ error: "el cuerpo no es JSON" }, { status: 400 })
  }

  const token = typeof body?.token === "string" ? body.token : ""
  const result = verifySession(token)

  if (!result.ok) {
    // The reason is named rather than flattened: "no_secret" is a
    // misconfigured server and "bad_signature" is a forged token, and
    // answering both with "unauthorized" hides an outage as an attack.
    const status = result.reason === "no_secret" ? 500 : 401
    return NextResponse.json({ error: result.reason }, { status })
  }

  const response = NextResponse.json({ address: result.session.address })
  response.cookies.set({
    name: SESSION_COOKIE,
    value: token,
    httpOnly: true,
    sameSite: "lax",
    // Only over TLS in production; in local development there is none.
    secure: process.env.NODE_ENV === "production",
    path: "/",
    // Expire with the token rather than on a round number, so the cookie and
    // the thing inside it never disagree about whether there is a session.
    expires: new Date(result.session.expiresAt * 1000),
  })
  return response
}

/** Who is signed in, if anyone. Used by the client to render the account. */
export async function GET(request: NextRequest) {
  const result = verifySession(request.cookies.get(SESSION_COOKIE)?.value)
  if (!result.ok) {
    return NextResponse.json({ address: null, reason: result.reason }, { status: 200 })
  }
  return NextResponse.json({ address: result.session.address })
}

/** Sign out. */
export async function DELETE() {
  const response = NextResponse.json({ ok: true })
  response.cookies.set({
    name: SESSION_COOKIE,
    value: "",
    httpOnly: true,
    sameSite: "lax",
    secure: process.env.NODE_ENV === "production",
    path: "/",
    maxAge: 0,
  })
  return response
}
