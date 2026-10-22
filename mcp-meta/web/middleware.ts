/**
 * The convenience redirect, and only that.
 *
 * It answers one question — "is there a session cookie at all?" — and it
 * does not verify it. That is deliberate on two counts:
 *
 *   Middleware runs on every request, so it is the wrong place for an HMAC
 *   check, and the edge runtime has no `node:crypto` to do one with.
 *
 *   More importantly, a redirect is **not** a security boundary. Anyone can
 *   skip this by calling the API directly. The real checks live where the
 *   cost is: `app/(app)/layout.tsx` verifies the signature before rendering,
 *   and `app/api/pm/route.ts` verifies it before spending money. This exists
 *   so a signed-out visitor sees a sign-in page instead of a screen that
 *   cannot load.
 *
 * What it buys us is the one thing the layout cannot do: it knows which URL
 * was asked for, so it can carry it in `?volver=` and put the person back
 * where they were going.
 */

import { NextRequest, NextResponse } from "next/server"

const SESSION_COOKIE = "brujula_session"

export function middleware(request: NextRequest) {
  if (request.cookies.get(SESSION_COOKIE)?.value) {
    return NextResponse.next()
  }

  const target = new URL("/login", request.url)
  const { pathname, search } = request.nextUrl
  // Only a path, never an absolute URL: `safeReturnTo` refuses anything
  // else on the way back, and there is no reason to hand it one.
  target.searchParams.set("volver", `${pathname}${search}`)
  return NextResponse.redirect(target)
}

/**
 * Only the application's own screens.
 *
 * `/login` and `/register` are excluded, and that exclusion is what keeps
 * this from eating its own exit: a visitor with no session sent to a
 * sign-in page that is itself behind the wall would loop forever. The same
 * goes for `/api` — the routes that *create* the session cannot require one.
 */
export const config = {
  matcher: [
    "/agentes/:path*",
    "/pm/:path*",
    "/escrows/:path*",
    "/verify/:path*",
    "/hire/:path*",
    "/explore/:path*",
    "/debate/:path*",
    "/deploy/:path*",
    "/knowledge/:path*",
  ],
}
