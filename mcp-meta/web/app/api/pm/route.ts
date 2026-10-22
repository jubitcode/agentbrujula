/**
 * The PM's door into mcp-meta: the only place the API key lives, and the lock
 * on the most expensive thing this product does.
 *
 * Until now this route asked for nothing. A plain POST ran a whole plan
 * against the server's PAID key — no account, no payment, no limit. And
 * because mcp-meta buckets requests by API key, every visitor shared one
 * bucket of ~500/hour with burst 1, so one abuser did not just spend money:
 * they left everyone else without a PM.
 *
 * Two things changed, and both are server-side because an interface gate is
 * a suggestion:
 *
 *   1. A verified session is required. The token is the JWT the marketplace
 *      issues for a signed wallet challenge, carried in an httpOnly cookie.
 *   2. The rate limit is per wallet, not global. Creating a Stellar account
 *      costs reserve, so a wallet is a real cost to acquire — which is what
 *      makes it worth limiting against.
 *
 * `/v1/execute` answers 401 without an `X-API-Key` whenever `AUTH_ENABLED` is
 * on, which it is by default in both `start.sh` and `docker-compose.yml`. The
 * browser cannot supply that key: anything it holds is readable by anyone who
 * opens devtools, and `NEXT_PUBLIC_` variables are compiled into the bundle.
 *
 * So the call is made here, server-side. The browser posts to a same-origin
 * route that has no secret in it, and this handler adds the key on its way
 * out. Same-origin also means no CORS, which is the other thing that was
 * quietly breaking requests from the page.
 *
 * It is a proxy and nothing else. No validation of the brief, no shaping of
 * the plan, no retries — those belong to the pod, which is the only place that
 * knows what a plan is.
 */

import { NextRequest, NextResponse } from "next/server"
import { SESSION_COOKIE, verifySession } from "@/lib/session"

/**
 * Where mcp-meta is. `MCP_META_URL` is server-only on purpose, so the address
 * of an internal service is not published to every visitor.
 */
const MCP = (process.env.MCP_META_URL || "http://127.0.0.1:9876").replace(/\/$/, "")
/**
 * The paid key when there is one, the free key otherwise.
 *
 * Not about billing: it is the rate limit. The free tier is 50 requests an
 * hour (`RATE_LIMIT_FREE=0.014`, a token bucket), and one plan plus one
 * revision plus the catalogue loading is already a handful. A person trying
 * the PM three times would hit "rate limit exceeded" and read it as the
 * product being broken.
 *
 * The PM is this product calling its own agent, not an anonymous visitor
 * calling a public API, so it uses the key that says so. The limit still
 * applies — 500/hr — it is just the right one.
 */
const KEY = process.env.AUTH_PAID_KEY || process.env.AUTH_FREE_KEY || ""

/** Only these two. An open proxy to /v1/execute would let anyone run any pod. */
const ALLOWED = new Set(["plan", "revise"])

/**
 * Generous, and deliberately so: a plan is one long generation, measured at
 * about ninety seconds on a free model, and the pod retries once when the
 * model answers badly. Cutting this short produces the worst failure mode —
 * the work completes on the server and the page says it failed.
 */
const TIMEOUT_MS = 300_000

/**
 * Plans per wallet per hour.
 *
 * A plan is roughly forty seconds of generation, so this is not a throughput
 * knob — it is the ceiling on what one person can spend of ours. Generous
 * enough that nobody doing real work meets it, low enough that a script is
 * not worth writing.
 */
const PLANS_PER_HOUR = Number(process.env.PM_PLANS_PER_HOUR || "12")
const WINDOW_MS = 60 * 60 * 1000

/**
 * Recent calls per wallet.
 *
 * In memory on purpose. It resets when the server restarts, which is the
 * honest trade: a persistent counter means a table, a migration and a write
 * on the hot path, and this exists to stop casual abuse rather than a
 * determined adversary — who is already paying Stellar reserve per wallet.
 */
const recent = new Map<string, number[]>()

function rateLimit(address: string): { allowed: boolean; retryAfterSeconds: number } {
  const now = Date.now()
  const cutoff = now - WINDOW_MS
  const hits = (recent.get(address) ?? []).filter((t) => t > cutoff)

  if (hits.length >= PLANS_PER_HOUR) {
    recent.set(address, hits)
    // When the oldest call in the window ages out, there is room again.
    const retryAfterSeconds = Math.max(1, Math.ceil((hits[0] + WINDOW_MS - now) / 1000))
    return { allowed: false, retryAfterSeconds }
  }

  hits.push(now)
  recent.set(address, hits)

  // Keep the map from growing without bound on a long-lived server: drop
  // wallets whose window has emptied. Cheap, and only on a successful call.
  if (recent.size > 5000) {
    for (const [key, times] of recent) {
      if (!times.some((t) => t > cutoff)) recent.delete(key)
    }
  }

  return { allowed: true, retryAfterSeconds: 0 }
}

export async function POST(request: NextRequest) {
  // The lock. Before reading the body, before touching mcp-meta: an
  // unauthenticated caller should cost us a signature check and nothing else.
  const session = verifySession(request.cookies.get(SESSION_COOKIE)?.value)
  if (!session.ok) {
    if (session.reason === "no_secret") {
      return NextResponse.json(
        { error: "la sesión no está configurada en el servidor" },
        { status: 500 },
      )
    }
    return NextResponse.json(
      {
        error:
          session.reason === "expired"
            ? "tu sesión venció — volvé a entrar"
            : "necesitás una cuenta para usar el PM",
        reason: session.reason,
      },
      { status: 401 },
    )
  }

  const limit = rateLimit(session.session.address)
  if (!limit.allowed) {
    const minutes = Math.ceil(limit.retryAfterSeconds / 60)
    return NextResponse.json(
      {
        error: `llegaste al límite de ${PLANS_PER_HOUR} planes por hora. Probá de nuevo en ${minutes} minuto${minutes === 1 ? "" : "s"}.`,
        retry_after_seconds: limit.retryAfterSeconds,
      },
      { status: 429, headers: { "Retry-After": String(limit.retryAfterSeconds) } },
    )
  }

  let body: { op?: string; params?: Record<string, unknown> }
  try {
    body = await request.json()
  } catch {
    return NextResponse.json({ error: "el cuerpo no es JSON" }, { status: 400 })
  }

  const op = String(body?.op || "")
  if (!ALLOWED.has(op)) {
    return NextResponse.json(
      { error: `operación desconocida: ${op || "(ninguna)"}` },
      { status: 400 },
    )
  }

  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), TIMEOUT_MS)

  try {
    const upstream = await fetch(`${MCP}/v1/execute`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        ...(KEY ? { "X-API-Key": KEY } : {}),
      },
      body: JSON.stringify({
        tool: `pm-planner.${op}`,
        arguments: body?.params ?? {},
        timeout: 280,
      }),
      signal: controller.signal,
    })

    const payload = await upstream.json().catch(() => null)

    if (!upstream.ok) {
      // mcp-meta's own words, not a generic failure. A 401 means the key is
      // wrong or missing here; a 404 means the manifest was never loaded,
      // which needs a restart rather than a retry. Flattening both into
      // "no se pudo" throws away the only sentence that says what to do.
      const detail =
        (payload && (payload.error || payload.message)) ||
        `mcp-meta respondió ${upstream.status}`
      return NextResponse.json({ error: String(detail) }, { status: upstream.status })
    }

    // Tool-level failures arrive as HTTP 200 with `error` in the body — that
    // is how mcp-meta reports them, and how /knowledge reads them. Passed
    // through as they are so the page can show the pod's own message.
    return NextResponse.json(payload ?? {})
  } catch (error) {
    const aborted = error instanceof Error && error.name === "AbortError"
    return NextResponse.json(
      {
        error: aborted
          ? `el agente tardó más de ${TIMEOUT_MS / 1000} segundos y se canceló la espera`
          : `no se pudo hablar con mcp-meta: ${
              error instanceof Error ? error.message : String(error)
            }`,
      },
      { status: aborted ? 504 : 502 },
    )
  } finally {
    clearTimeout(timer)
  }
}
