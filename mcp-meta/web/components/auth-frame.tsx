"use client"

/**
 * The two-panel frame shared by /login and /register.
 *
 * Both doors look the same on the left — what Brújula is, and the sentence
 * about never asking for a secret phrase, which is the one that decides
 * whether someone connects a wallet to a site they just met. Only the right
 * panel differs, so only the right panel is a prop.
 */

import Link from "next/link"
import type { ReactNode } from "react"
import { BrujulaWordmark } from "@/components/logo/BrujulaWordmark"
import { LangSwitch } from "@/components/lang-switch"
import { useI18n } from "@/lib/i18n"

export function AuthFrame({ children }: { children: ReactNode }) {
  const { t } = useI18n()

  return (
    <div className="auth-shell">
      <section className="auth-intro">
        <Link href="/" style={{ textDecoration: "none", color: "inherit" }}>
          <BrujulaWordmark height={26} />
        </Link>
        <div>
          <h1>
            {t("auth.heroA")}
            <br />
            {t("auth.heroB")}
            <br />
            {t("auth.heroC")}
          </h1>
          <p className="t-intro" style={{ marginTop: 18 }}>{t("auth.heroIntro")}</p>
        </div>
        <div className="auth-trust">
          <span aria-hidden="true" style={{ color: "var(--green)" }}>⛊</span>
          <span>{t("auth.trust")}</span>
        </div>
      </section>

      <section className="auth-panel">
        {/* Inside the content column, not pinned to the panel's corner.
            With the panel twice as wide as the intro, a switch floating at
            the far edge reads as belonging to nothing. */}
        <div className="auth-panel-inner">
          <div style={{ display: "flex", justifyContent: "flex-end", marginBottom: 22 }}>
            <LangSwitch />
          </div>
          {children}
        </div>
      </section>
    </div>
  )
}
