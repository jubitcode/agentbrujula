/**
 * The signed-in person: session state and profile.
 *
 * Two different questions, deliberately kept apart:
 *
 *   - **Is there a session?** Answered by Next, from the httpOnly cookie
 *     (`/api/session`). Cheap, no wallet prompt, no marketplace round trip.
 *   - **Who is this person?** Answered by the marketplace (`/api/profile`),
 *     which needs the wallet-auth header.
 *
 * A wallet can have a session and no profile — that is exactly the state
 * someone is in halfway through signing up — so `registered` is its own flag
 * rather than something inferred from an empty name.
 */

export type Profile = {
  wallet_address: string
  display_name: string
  email: string
  role: string
  /** False when the wallet authenticated but never completed the form. */
  registered: boolean
  created_at?: number
}

/**
 * The address of whoever holds the session cookie, or null.
 *
 * Reads Next rather than `sessionStorage` on purpose: the cookie is what the
 * server actually honours, so asking it is asking the thing that decides.
 */
export async function getSessionAddress(): Promise<string | null> {
  try {
    const res = await fetch("/api/session", { cache: "no-store" })
    if (!res.ok) return null
    const body = await res.json()
    return typeof body?.address === "string" ? body.address : null
  } catch {
    return null
  }
}

/**
 * The profile for the signed-in wallet.
 *
 * Asked of our own `/api/profile`, which carries the session cookie and adds
 * the token server-side. Going to the marketplace directly would need the
 * JWT from `sessionStorage` — gone the moment the tab closes — and would ask
 * someone with a perfectly good session to sign again to read their own name.
 */
export async function getProfile(): Promise<Profile> {
  const res = await fetch("/api/profile", { cache: "no-store" })
  if (!res.ok) {
    throw new Error(await describeFailure(res))
  }
  return res.json()
}

/**
 * Create or update the profile. The address comes from the token, not here.
 *
 * POST rather than PUT: the marketplace's CORS allows GET, POST and OPTIONS
 * only, so a PUT is rejected at the preflight and surfaces as "Failed to
 * fetch" — an error that looks like the network and is really a verb.
 */
export async function saveProfile(input: {
  display_name: string
  email: string
}): Promise<Profile> {
  const res = await fetch("/api/profile", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  })
  if (!res.ok) {
    throw new Error(await describeFailure(res))
  }
  return res.json()
}

/**
 * Sign out of everything.
 *
 * Three things said "you are signed in" and this cleared two of them. The
 * third, `wallet_public_key` in localStorage, survived — and the landing
 * read *that* one as proof of identity, so signing out bounced you to the
 * app, which had no cookie, which bounced you to sign-up. Not a stray
 * redirect: a loop, because the way back out read the same stale key.
 *
 * `disconnect()` has always existed to clear it (`lib/wallet.ts:125`) and
 * had no caller anywhere in the app. It does now.
 *
 * Every step is attempted even if an earlier one throws: a half sign-out
 * that *looks* complete is how someone ends up believing they logged out.
 */
export async function signOut(): Promise<void> {
  try {
    await fetch("/api/session", { method: "DELETE" })
  } catch {
    // The cookie may outlive this, but the local state below still goes.
  }

  try {
    const { clearSessionToken } = await import("./wallet-auth")
    clearSessionToken()
  } catch {
    // Storage disabled or unavailable; nothing to clear.
  }

  try {
    const { disconnect } = await import("./wallet")
    await disconnect()
  } catch {
    // The wallet kit can refuse; the key is removed either way below.
  }

  // Belt and braces: `disconnect()` also tears down the wallet kit, and if
  // that throws the key must still go. This is the one that mattered.
  try {
    localStorage.removeItem("wallet_public_key")
  } catch {
    // Private mode. Nothing stored means nothing stale.
  }
}

/** The server's own words when it has them, rather than a generic failure. */
async function describeFailure(res: Response): Promise<string> {
  try {
    const body = await res.json()
    const detail = body?.detail ?? body?.error
    if (typeof detail === "string" && detail) return detail
  } catch {
    // Not JSON; fall through to the status.
  }
  return `HTTP ${res.status}`
}

/**
 * The three states a visitor can be in.
 *
 * Writing these down is most of the fix. The flow kept bouncing people
 * because only two were modelled — in or out — and there is a third:
 * someone who signed the challenge (so they have a session) and never
 * finished the form (so they have no profile). That state is normal; it is
 * what abandoning sign-up halfway looks like. Unnamed, it had nowhere to go.
 */
export type AccountState =
  /** No session. Belongs at /login. */
  | { kind: "anonymous" }
  /** Signed, unregistered. Belongs at /register, on the profile step. */
  | { kind: "unregistered"; address: string }
  /** Done. Belongs in the app. */
  | { kind: "registered"; address: string; profile: Profile }

/**
 * Work out which one, asking the cookie first and the marketplace second.
 *
 * The order matters for cost: the cookie answer is local and free, and most
 * visitors are anonymous, so they never trigger a wallet-authenticated call.
 */
export async function getAccountState(): Promise<AccountState> {
  const address = await getSessionAddress()
  if (!address) return { kind: "anonymous" }

  try {
    const profile = await getProfile()
    return profile.registered
      ? { kind: "registered", address, profile }
      : { kind: "unregistered", address }
  } catch {
    // A cookie the marketplace does not honour, or a marketplace that is
    // not answering. Either way we cannot claim this person is registered,
    // and treating them as unregistered would send them to a form they may
    // have already filled. Anonymous is the honest answer: sign in again.
    return { kind: "anonymous" }
  }
}
