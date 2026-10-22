/**
 * The profile, proxied through Next so the cookie is enough.
 *
 * Before this, the browser read the profile straight from the marketplace
 * with `Authorization: Bearer …`, taking the token from `sessionStorage`.
 * That works until the tab is closed — `sessionStorage` dies with it, while
 * the session cookie lives for an hour. Someone coming back inside that hour
 * had a perfectly valid session and was asked to sign again to find out
 * their own name.
 *
 * Worse, the failure was silent in the wrong direction: unable to read the
 * profile, the app concluded "no account" and offered sign-up to someone who
 * already had one.
 *
 * So the token comes from the httpOnly cookie, on the server, and the
 * browser just asks same-origin. Same shape as `/api/pm`: the credential
 * never reaches the page, and there is no CORS in the way.
 */

import { NextRequest, NextResponse } from "next/server"
import { SESSION_COOKIE, verifySession } from "@/lib/session"

const MARKETPLACE =
  (process.env.NEXT_PUBLIC_MARKETPLACE_URL || "http://localhost:5050").replace(/\/$/, "")

/** The session, or the response to send instead. */
function requireSession(request: NextRequest) {
  const result = verifySession(request.cookies.get(SESSION_COOKIE)?.value)
  if (result.ok) return { token: request.cookies.get(SESSION_COOKIE)!.value }
  return {
    error: NextResponse.json(
      { error: "necesitás una sesión", reason: result.reason },
      { status: result.reason === "no_secret" ? 500 : 401 },
    ),
  }
}

async function forward(token: string, init?: RequestInit) {
  const res = await fetch(`${MARKETPLACE}/api/profile`, {
    ...init,
    headers: { ...(init?.headers ?? {}), Authorization: `Bearer ${token}` },
    cache: "no-store",
  })
  const body = await res.json().catch(() => null)
  return NextResponse.json(body ?? {}, { status: res.status })
}

export async function GET(request: NextRequest) {
  const session = requireSession(request)
  if ("error" in session) return session.error
  try {
    return await forward(session.token)
  } catch (err) {
    return NextResponse.json(
      { error: `no se pudo leer el perfil: ${err instanceof Error ? err.message : String(err)}` },
      { status: 502 },
    )
  }
}

export async function POST(request: NextRequest) {
  const session = requireSession(request)
  if ("error" in session) return session.error

  let body: unknown
  try {
    body = await request.json()
  } catch {
    return NextResponse.json({ error: "el cuerpo no es JSON" }, { status: 400 })
  }

  try {
    return await forward(session.token, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    })
  } catch (err) {
    return NextResponse.json(
      { error: `no se pudo guardar el perfil: ${err instanceof Error ? err.message : String(err)}` },
      { status: 502 },
    )
  }
}
