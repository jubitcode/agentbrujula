"use client"

/**
 * Create an account.
 *
 * Role, wallet, profile — then a review. The shape comes from the login
 * prototype; what it does underneath does not, because the prototype
 * simulated everything.
 *
 * Two things this page has to get right, and neither is the happy path:
 *
 *   Someone who **already has an account** must not be made to fill in a
 *   form they filled in before. The wallet tells us, but only after they
 *   sign — so the check happens there, and they go straight in.
 *
 *   Someone who **signed and then left** comes back to the profile step,
 *   not to the beginning. They already paid the signature; charging it
 *   twice because our state was not written down is our problem, not
 *   theirs. That state has a name now: `unregistered`.
 */

import { useCallback, useEffect, useState } from "react"
import { useRouter } from "next/navigation"
import Link from "next/link"
import { connectWallet } from "@/lib/wallet"
import { getWalletAuthHeader } from "@/lib/wallet-auth"
import { getAccountState, saveProfile } from "@/lib/account"
import { useI18n } from "@/lib/i18n"
import { AuthFrame } from "@/components/auth-frame"
import { ErrorState, Loading } from "@/components/ui/states"

type Step = "checking" | "role" | "wallet" | "profile" | "review" | "already"

/** Where someone lands once they are in. */
const HOME = "/agentes"

export default function RegisterPage() {
  const { t } = useI18n()
  const router = useRouter()

  const [step, setStep] = useState<Step>("checking")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [resumed, setResumed] = useState(false)

  const [address, setAddress] = useState("")
  const [name, setName] = useState("")
  const [email, setEmail] = useState("")

  /** Pick up wherever this person actually is. */
  useEffect(() => {
    let cancelled = false
    getAccountState().then((state) => {
      if (cancelled) return
      if (state.kind === "anonymous") {
        setStep("role")
        return
      }
      setAddress(state.address)
      if (state.kind === "registered") {
        setName(state.profile.display_name)
        setStep("already")
      } else {
        // Signed, no profile: resume rather than restart.
        setResumed(true)
        setStep("profile")
      }
    })
    return () => {
      cancelled = true
    }
  }, [])

  const connect = useCallback(async () => {
    setBusy(true)
    setError(null)
    try {
      const { publicKey } = await connectWallet()
      await getWalletAuthHeader()
      setAddress(publicKey)

      // The wallet may already be registered — this is the door for new
      // accounts, but nothing stops a returning person from walking in.
      const state = await getAccountState()
      if (state.kind === "registered") {
        setName(state.profile.display_name)
        setStep("already")
        return
      }
      setStep("profile")
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e)
      const rejected = /reject|declin|denied|cancel/i.test(message)
      setError(rejected ? t("auth.signRejected") : `${t("auth.connectFailed")} ${message}`)
    } finally {
      setBusy(false)
    }
  }, [t])

  function submitProfile(event: React.FormEvent) {
    event.preventDefault()
    setError(null)
    if (!name.trim()) {
      setError(t("auth.nameRequired"))
      return
    }
    if (email.trim() && !/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(email.trim())) {
      setError(t("auth.emailInvalid"))
      return
    }
    setStep("review")
  }

  async function confirm() {
    setBusy(true)
    setError(null)
    try {
      await saveProfile({ display_name: name.trim(), email: email.trim() })
      router.replace(HOME)
    } catch (e) {
      setError(`${t("auth.saveFailed")} ${e instanceof Error ? e.message : String(e)}`)
      setBusy(false)
    }
  }

  const shortAddress = address ? `${address.slice(0, 6)}…${address.slice(-6)}` : ""

  return (
    <AuthFrame>
      {step === "checking" && <Loading />}

      {step !== "checking" && step !== "already" && (
        <>
          <div className="section-label" style={{ marginBottom: 8 }}>{t("auth.welcome")}</div>
          <h1 className="t-h2">{t("register.title")}</h1>
          <p className="t-small" style={{ margin: "0 0 26px" }}>{t("register.subtitle")}</p>
        </>
      )}

      {step === "role" && (
        <div>
          <p className="step-label">{t("auth.step1")}</p>
          <div className="role-grid">
            <button className="role-option" onClick={() => setStep("wallet")}>
              <span className="role-icon" aria-hidden="true">↗</span>
              <span>
                <strong>{t("auth.roleClient")}</strong>
                <small>{t("auth.roleClientHint")}</small>
              </span>
            </button>
            {/* Visible and disabled: it says where the product is going
                without pretending it is already there. */}
            <button className="role-option" disabled>
              <span className="role-icon" aria-hidden="true">✧</span>
              <span>
                <strong>{t("auth.roleProvider")}</strong>
                <small>{t("auth.roleProviderHint")}</small>
              </span>
              <span className="badge-soon">{t("auth.soon")}</span>
            </button>
          </div>
        </div>
      )}

      {step === "wallet" && (
        <div>
          <button className="back-action" onClick={() => setStep("role")}>
            ← {t("auth.changeRole")}
          </button>
          <p className="step-label">{t("auth.step2")}</p>
          <h2 className="t-h3" style={{ fontSize: 18 }}>{t("auth.connectTitle")}</h2>
          <p className="t-small" style={{ margin: "0 0 18px" }}>{t("auth.connectSubtitle")}</p>
          <button className="btn-primary" onClick={connect} disabled={busy} style={{ width: "100%" }}>
            {busy ? (
              <>
                <span className="spinner" aria-hidden="true" />
                {t("auth.connecting")}
              </>
            ) : (
              t("auth.connectButton")
            )}
          </button>
        </div>
      )}

      {step === "profile" && (
        <form onSubmit={submitProfile}>
          {/* No "change wallet" when resuming: they did not come from that
              step this time, and offering to undo something they did in a
              previous visit is a false promise. */}
          {!resumed && (
            <button type="button" className="back-action" onClick={() => setStep("wallet")}>
              ← {t("auth.changeWallet")}
            </button>
          )}
          {resumed && (
            <p className="t-small" style={{ color: "var(--accent)", margin: "0 0 14px" }}>
              {t("register.resume")}
            </p>
          )}
          <p className="step-label">{t("auth.step3")}</p>
          <h2 className="t-h3" style={{ fontSize: 18 }}>{t("auth.profileTitle")}</h2>
          <p className="t-small" style={{ margin: "0 0 18px" }}>{t("auth.profileSubtitle")}</p>

          <label className="field-label" htmlFor="name">{t("auth.name")}</label>
          <input
            id="name"
            className="input"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={t("auth.namePlaceholder")}
            maxLength={80}
            autoComplete="name"
            style={{ marginBottom: 14 }}
          />

          <label className="field-label" htmlFor="email">{t("auth.email")}</label>
          <input
            id="email"
            className="input"
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            placeholder={t("auth.emailPlaceholder")}
            maxLength={254}
            autoComplete="email"
            style={{ marginBottom: 18 }}
          />

          <button className="btn-primary" type="submit" style={{ width: "100%" }}>
            {t("auth.continue")}
          </button>
        </form>
      )}

      {step === "review" && (
        <div>
          <button className="back-action" onClick={() => setStep("profile")}>
            ← {t("auth.reviewEdit")}
          </button>
          <p className="step-label">{t("auth.reviewLabel")}</p>
          <h2 className="t-h3" style={{ fontSize: 18 }}>{t("auth.reviewTitle")}</h2>
          <p className="t-small" style={{ margin: "0 0 4px" }}>{t("auth.reviewSubtitle")}</p>

          <div className="review-card">
            <div className="review-row">
              <span>{t("auth.name")}</span>
              <strong>{name}</strong>
            </div>
            {email && (
              <div className="review-row">
                <span>{t("auth.email")}</span>
                <strong>{email}</strong>
              </div>
            )}
            <div className="review-row">
              <span>{t("auth.role")}</span>
              <strong>{t("auth.roleClientLabel")}</strong>
            </div>
            <div className="review-row">
              <span>{t("auth.wallet")}</span>
              <strong className="mono-id">{shortAddress}</strong>
            </div>
          </div>

          <button className="btn-primary" onClick={confirm} disabled={busy} style={{ width: "100%" }}>
            {busy ? t("auth.saving") : t("auth.confirm")}
          </button>
        </div>
      )}

      {step === "already" && (
        <div>
          <div className="section-label" style={{ marginBottom: 8 }}>{t("auth.welcome")}</div>
          <h1 className="t-h2">{t("register.alreadyRegistered")}</h1>
          <p className="t-small" style={{ margin: "0 0 4px" }}>{t("register.alreadyHint")}</p>
          <div className="review-card">
            {name && (
              <div className="review-row">
                <span>{t("auth.name")}</span>
                <strong>{name}</strong>
              </div>
            )}
            <div className="review-row">
              <span>{t("auth.wallet")}</span>
              <strong className="mono-id">{shortAddress}</strong>
            </div>
          </div>
          <button className="btn-primary" onClick={() => router.replace(HOME)} style={{ width: "100%" }}>
            {t("auth.goToApp")}
          </button>
        </div>
      )}

      {step !== "checking" && step !== "already" && (
        <p className="t-small" style={{ marginTop: 22, textAlign: "center" }}>
          {t("register.haveAccount")}{" "}
          <Link href="/login" style={{ color: "var(--accent)" }}>
            {t("register.goLogin")}
          </Link>
        </p>
      )}

      {error && (
        <div style={{ marginTop: 18 }}>
          <ErrorState title={error} />
        </div>
      )}
    </AuthFrame>
  )
}
