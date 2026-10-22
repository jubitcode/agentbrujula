"use client"

/**
 * The application frame: sidebar, topbar, content.
 *
 * Layout from the marketplace prototype — a fixed 248px rail, the account
 * block pinned to its bottom, a 68px topbar with breadcrumb and avatar. The
 * palette is this project's, not the prototype's: the prototype is lime on
 * paper and Brújula is dark.
 *
 * Sections that do not exist yet are shown and disabled, marked "Pronto",
 * exactly as the prototype does. It is more honest than hiding them and it
 * says where the product is going.
 */

import Link from "next/link"
import { usePathname, useRouter } from "next/navigation"
import { useEffect, useState, type ReactNode } from "react"
import { BrujulaWordmark } from "@/components/logo/BrujulaWordmark"
import { LangSwitch } from "@/components/lang-switch"
import { EscrowBar } from "@/components/escrow-bar"
import { getProfile, signOut } from "@/lib/account"
import { prepareWallet } from "@/lib/marketplace"
import { useI18n, type Key } from "@/lib/i18n"

/** Built sections. Each one is a real screen. */
const SECTIONS: { href: string; key: Key; icon: string }[] = [
  { href: "/agentes", key: "nav.agents", icon: "▦" },
  { href: "/pm", key: "nav.pm", icon: "◈" },
  { href: "/escrows", key: "nav.escrows", icon: "▤" },
  { href: "/verify", key: "nav.verify", icon: "⛊" },
  { href: "/hire", key: "nav.hire", icon: "↗" },
]

/** Named in the prototype's menu, not built. Shown so the shape is legible. */
const COMING: Key[] = ["nav.myAgents", "nav.wallet", "nav.help"]

const EXPLORER =
  (process.env.NEXT_PUBLIC_STELLAR_NETWORK || "TESTNET").toUpperCase() === "PUBLIC"
    ? "https://stellar.expert/explorer/public"
    : "https://stellar.expert/explorer/testnet"

const NETWORK =
  (process.env.NEXT_PUBLIC_STELLAR_NETWORK || "TESTNET").toUpperCase() === "PUBLIC"
    ? "Mainnet"
    : "Testnet"

export function AppShell({ address, children }: { address: string; children: ReactNode }) {
  const { t } = useI18n()
  const pathname = usePathname()
  const router = useRouter()
  const [name, setName] = useState("")
  // null = not asked yet, "" = the lookup failed, else the figure.
  const [balance, setBalance] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  // The profile is for the avatar and the greeting only, so a failure here
  // must not take the application down with it: the session is already
  // established and every screen works without a display name.
  useEffect(() => {
    let cancelled = false
    getProfile()
      .then((p) => !cancelled && setName(p.display_name))
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [])

  // The balance used to live in the header's dropdown, which no longer
  // exists. It is read once per mount rather than per dropdown-open, because
  // here it is always on screen.
  useEffect(() => {
    if (!address) return
    let cancelled = false
    prepareWallet(address)
      .then((r) => !cancelled && setBalance(r.usdc_balance_display ?? "0"))
      .catch(() => !cancelled && setBalance(""))
    return () => {
      cancelled = true
    }
  }, [address])

  async function copyAddress() {
    try {
      await navigator.clipboard.writeText(address)
    } catch {
      // Some browsers refuse outside a user gesture. The feedback still
      // flashes so the click does not feel dead; the address is selectable.
    }
    setCopied(true)
    window.setTimeout(() => setCopied(false), 1500)
  }

  const current = SECTIONS.find((s) => s.href === pathname)
  const short = address ? `${address.slice(0, 4)}…${address.slice(-4)}` : ""
  const initial = (name || address || "?").trim().charAt(0).toUpperCase()

  async function leave() {
    await signOut()
    // The landing, and it stays there now: it no longer reads localStorage
    // to decide you are signed in, and sign-out no longer leaves that key
    // behind. Between the two, the loop has nothing left to feed on.
    router.push("/")
  }

  return (
    <div className="app-shell">
      <aside className="app-sidebar">
        <Link href="/agentes" style={{ textDecoration: "none", color: "inherit", padding: "0 10px" }}>
          <BrujulaWordmark height={24} />
        </Link>

        <div>
          <div className="side-label">{t("nav.platform")}</div>
          <nav className="side-nav" aria-label={t("nav.label")}>
            {SECTIONS.map((s) => (
              <Link key={s.href} href={s.href} aria-current={pathname === s.href ? "page" : undefined}>
                <span className="side-ico" aria-hidden="true">{s.icon}</span>
                {t(s.key)}
              </Link>
            ))}
          </nav>
        </div>

        <div>
          <div className="side-label">{t("nav.account")}</div>
          <nav className="side-nav">
            {COMING.map((key) => (
              <button key={key} disabled>
                <span className="side-ico" aria-hidden="true">◇</span>
                {t(key)}
                <span className="badge-soon">{t("auth.soon")}</span>
              </button>
            ))}
          </nav>
        </div>

        <div className="side-foot">
          <div className="side-wallet">
            <small>{t("nav.networkLabel", { network: NETWORK })}</small>

            <div className="side-wallet-row">
              <span style={{ color: "var(--muted)" }}>{t("header.balance")}</span>
              <span style={{ color: "var(--green)" }} aria-hidden="true">●</span>
            </div>
            <div style={{ marginTop: 4, marginBottom: 10 }}>
              {balance === null && (
                <span className="t-small" style={{ color: "var(--dim)" }}>{t("header.balanceLoading")}</span>
              )}
              {balance === "" && (
                <span className="t-small" style={{ color: "var(--dim)" }}>{t("header.balanceUnavailable")}</span>
              )}
              {balance && (
                <span style={{ display: "flex", alignItems: "baseline", gap: 5 }}>
                  <strong style={{ fontFamily: "var(--mono)", fontSize: 17 }}>{balance}</strong>
                  <span className="t-small" style={{ color: "var(--muted)" }}>USDC</span>
                </span>
              )}
            </div>

            <button
              type="button"
              onClick={copyAddress}
              aria-label={t("header.copyAddress")}
              title={t("header.copyAddress")}
              style={{
                display: "flex", alignItems: "center", gap: 7, width: "100%",
                background: "transparent", border: 0, padding: 0,
                cursor: "pointer", textAlign: "left",
                color: copied ? "var(--green)" : "var(--muted)",
              }}
            >
              <span className="mono-id" style={{ fontSize: 11 }}>{short}</span>
              <span style={{ marginLeft: "auto", fontSize: 10, flexShrink: 0 }}>
                {copied ? t("common.copied") : t("common.copy")}
              </span>
            </button>

            <a
              href={`${EXPLORER}/account/${address}`}
              target="_blank"
              rel="noreferrer noopener"
              className="t-small"
              style={{ display: "block", marginTop: 9, color: "var(--accent)", textDecoration: "none" }}
            >
              {t("header.explorer")} ↗
            </a>
          </div>
          <button className="btn-ghost btn-sm" onClick={leave}>
            ← {t("auth.signOut")}
          </button>
        </div>
      </aside>

      <main className="app-main">
        <header className="app-topbar">
          <div className="breadcrumb">
            {t("nav.platform")}
            <span className="sep">/</span>
            {current ? t(current.key) : t("nav.agents")}
          </div>
          <div style={{ display: "flex", alignItems: "center", gap: 14 }}>
            <LangSwitch />
            <span className="status-pill">● {NETWORK}</span>
            <span className="avatar" title={name || short}>{initial}</span>
          </div>
        </header>

        {/* On a phone the sidebar is hidden, so the sections come back as the
            horizontal row they were before this redesign. */}
        <MobileNav sections={SECTIONS} pathname={pathname} />

        <div className="app-content">{children}</div>

        {/* Follows you across screens while a contract is open. */}
        <EscrowBar />
      </main>
    </div>
  )
}

function MobileNav({
  sections,
  pathname,
}: {
  sections: { href: string; key: Key }[]
  pathname: string
}) {
  const { t } = useI18n()
  return (
    <nav className="mobile-nav" aria-label={t("nav.label")}>
      {sections.map((s) => (
        <Link key={s.href} href={s.href} aria-current={pathname === s.href ? "page" : undefined}>
          {t(s.key)}
        </Link>
      ))}
    </nav>
  )
}
