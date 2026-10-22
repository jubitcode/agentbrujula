/**
 * `/entrar` is gone, split into `/login` and `/register`.
 *
 * This stays as a redirect because the address was shipped: it is in the
 * landing's CTA history, in screenshots, and possibly in someone's tab.
 * Sign-in is the safer of the two destinations — a returning visitor gets
 * what they expected, and a new one is one link away from the right door.
 */

import { redirect } from "next/navigation"

export default function EntrarPage() {
  redirect("/login")
}
