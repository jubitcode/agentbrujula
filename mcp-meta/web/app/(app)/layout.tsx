/**
 * Everything behind the login.
 *
 * Two layers guard this, and they do different jobs. `middleware.ts` does a
 * cheap existence check on the cookie and knows the URL, so it is what sends
 * someone to `/login?volver=…`. This one runs in Node, verifies the
 * signature properly, and knows the thing the middleware cannot cheaply ask:
 * whether this person ever finished signing up.
 *
 * That third state is why the old flow bounced people. Someone who signed
 * the challenge has a session, but a session is not an account — the profile
 * comes after. Sent to the app they met a screen with no name on it; sent to
 * sign-in they were already signed in. They belong at `/register`, on the
 * step they stopped at.
 *
 * Neither layer is a security boundary. Both are conveniences that keep
 * people out of screens that would not work for them. The boundary is in
 * each resource: `/api/pm` verifies the same token before spending anything,
 * and the marketplace checks it again on its own protected endpoints.
 */

import { cookies } from "next/headers"
import { redirect } from "next/navigation"
import { SESSION_COOKIE, verifySession } from "@/lib/session"
import { AppShell } from "@/components/app-shell"

const MARKETPLACE_API =
  process.env.NEXT_PUBLIC_MARKETPLACE_URL || "http://localhost:5050"

/**
 * Has this wallet finished signing up?
 *
 * Asked of the marketplace, which owns the answer — rather than mirrored
 * into a second cookie that could disagree with it.
 *
 * It fails **open**: when the marketplace cannot be reached we render the
 * app rather than pushing someone into sign-up they may have completed
 * months ago. Being wrong in that direction shows a screen with a missing
 * name; being wrong in the other direction tells a registered person they
 * do not exist, and nothing they can do from there will fix it.
 */
async function hasProfile(token: string): Promise<boolean> {
  try {
    const res = await fetch(`${MARKETPLACE_API}/api/profile`, {
      headers: { Authorization: `Bearer ${token}` },
      cache: "no-store",
    })
    if (!res.ok) return true
    const body = await res.json()
    return body?.registered !== false
  } catch {
    return true
  }
}

export default async function AppLayout({ children }: { children: React.ReactNode }) {
  const token = (await cookies()).get(SESSION_COOKIE)?.value
  const session = verifySession(token)

  if (!session.ok) {
    // `expired` is told apart so the sign-in page can say "your session
    // ended" instead of "sign up" — different sentences for someone who was
    // working a minute ago.
    redirect(session.reason === "expired" ? "/login?motivo=vencida" : "/login")
  }

  if (!(await hasProfile(token as string))) {
    redirect("/register")
  }

  return <AppShell address={session.session.address}>{children}</AppShell>
}
