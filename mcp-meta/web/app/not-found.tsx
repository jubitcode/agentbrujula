"use client"

/**
 * The 404.
 *
 * There wasn't one, so a mistyped address got Next's built-in page: the word
 * "404", "This page could not be found", in English regardless of the
 * language, and no way back other than the browser's back button. `/explore`
 * links to `/task/new?q=…`, a route that does not exist, so this was reachable
 * by clicking rather than only by mistyping.
 *
 * It used to inherit a header from the root layout. Since the layout stopped
 * carrying chrome — there are three frames now and this page is in none of
 * them — it supplies its own, which is most of what "a way back" means.
 */

import Link from "next/link"
import { useI18n } from "@/lib/i18n"
import { BrujulaWordmark } from "@/components/logo/BrujulaWordmark"

export default function NotFound() {
  const { t } = useI18n()

  return (
    <>
      {/* Its own header. This page renders outside both frames — it is not
          inside the app's sidebar and not inside the landing — so with the
          root layout carrying no chrome it would otherwise start flush
          against the left edge with no way back to anywhere. */}
      <header className="app-topbar">
        <Link href="/" style={{ textDecoration: "none", color: "inherit" }}>
          <BrujulaWordmark height={24} />
        </Link>
      </header>

      <div className="app-content" style={{ paddingTop: 36, paddingBottom: 80 }}>
        <div className="section-label">{`// ${t("notFound.kicker")}`}</div>
        <h1 className="t-h2">{t("notFound.title")}</h1>
        <p className="t-intro" style={{ marginBottom: 28 }}>{t("notFound.intro")}</p>

        <div style={{ display: "flex", gap: 12, flexWrap: "wrap" }}>
          {/* Home is the public landing now, and the catalogue moved to
              /agentes behind the login. Someone with no session who clicks
              the second one lands on /entrar, which is the right place. */}
          <Link href="/" className="btn-primary btn-inline" style={{ textDecoration: "none" }}>
            {t("notFound.home")}
          </Link>
          <Link href="/agentes" className="btn-ghost btn-inline" style={{ textDecoration: "none" }}>
            {t("notFound.agents")}
          </Link>
        </div>
      </div>
    </>
  )
}
