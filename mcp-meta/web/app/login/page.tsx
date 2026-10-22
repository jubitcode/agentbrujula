"use client"

/**
 * Sign in.
 *
 * With a wallet there is no way to tell a returning visitor from a new one
 * before they sign — the signature *is* the identity. So this page and
 * /register ask for the same gesture and differ in what they do with the
 * answer: this one expects to find an account, and says so plainly when it
 * does not.
 *
 * Someone who lands here by mistake is not sent back to the start. They have
 * already signed, which means there is already a session; /register picks
 * them up at the profile step rather than charging them a second signature
 * for choosing the wrong door.
 */

import { useCallback, useEffect, useState } from "react"
import Link from "next/link"
import { useRouter, useSearchParams } from "next/navigation"
import { connectWallet } from "@/lib/wallet"
import { getWalletAuthHeader } from "@/lib/wallet-auth"
import { getAccountState } from "@/lib/account"
import { safeReturnTo } from "@/lib/session"
import { useI18n } from "@/lib/i18n"
import { AuthFrame } from "@/components/auth-frame"
import { ErrorState, Loading } from "@/components/ui/states"

type View = "checking" | "ready" | "no-account"

export default function LoginPage() {
  const { t } = useI18n()
  const router = useRouter()
  const params = useSearchParams()

  const [view, setView] = useState<View>("checking")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  /** Where they were going before the wall stopped them. Validated. */
  const destination = safeReturnTo(params.get("volver"))
  const expired = params.get("motivo") === "vencida"

  /** Someone who is already in should not be asked to sign in again. */
  useEffect(() => {
    let cancelled = false
    getAccountState().then((state) => {
      if (cancelled) return
      if (state.kind === "registered") router.replace(destination)
      else if (state.kind === "unregistered") router.replace("/register")
      else setView("ready")
    })
    return () => {
      cancelled = true
    }
  }, [router, destination])

  const signIn = useCallback(async () => {
    setBusy(true)
    setError(null)
    try {
      await connectWallet()
      // The signature. This is what proves the wallet is theirs, and it is
      // what creates the session — whether or not an account exists yet.
      await getWalletAuthHeader()

      const state = await getAccountState()
      if (state.kind === "registered") {
        router.replace(destination)
        return
      }
      // Signed, but nobody by this wallet. Not an error: a wrong door.
      setView("no-account")
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e)
      const rejected = /reject|declin|denied|cancel/i.test(message)
      setError(rejected ? t("auth.signRejected") : `${t("auth.connectFailed")} ${message}`)
    } finally {
      setBusy(false)
    }
  }, [router, destination, t])

  return (
    <AuthFrame>
      {view === "checking" && <Loading title={t("login.checking")} />}

      {view === "ready" && (
        <>
          <div className="section-label" style={{ marginBottom: 8 }}>{t("auth.welcome")}</div>
          <h1 className="t-h2">{t("login.title")}</h1>
          <p className="t-small" style={{ margin: "0 0 10px" }}>{t("login.subtitle")}</p>

          {expired && (
            <p className="t-small" style={{ color: "var(--amber)", margin: "0 0 18px" }}>
              {t("login.expired")}
            </p>
          )}

          <button
            className="btn-primary"
            onClick={signIn}
            disabled={busy}
            style={{ width: "100%", marginTop: 14 }}
          >
            {busy ? (
              <>
                <span className="spinner" aria-hidden="true" />
                {t("auth.connecting")}
              </>
            ) : (
              t("login.button")
            )}
          </button>

          <p className="t-small" style={{ marginTop: 22, textAlign: "center" }}>
            {t("login.firstTime")}{" "}
            <Link href="/register" style={{ color: "var(--accent)" }}>
              {t("login.goRegister")}
            </Link>
          </p>
        </>
      )}

      {view === "no-account" && (
        <>
          <div className="section-label" style={{ marginBottom: 8 }}>{t("auth.welcome")}</div>
          <h1 className="t-h2">{t("login.notFound")}</h1>
          <p className="t-small" style={{ margin: "0 0 18px" }}>{t("login.notFoundHint")}</p>
          <button
            className="btn-primary"
            onClick={() => router.push("/register")}
            style={{ width: "100%" }}
          >
            {t("login.createAccount")} →
          </button>
        </>
      )}

      {error && (
        <div style={{ marginTop: 18 }}>
          <ErrorState title={error} />
        </div>
      )}
    </AuthFrame>
  )
}
