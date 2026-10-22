"use client"

/**
 * Loading, error and empty — one presentation, used everywhere.
 *
 * Before this there were eight loading states with eight different wordings
 * and six error states with six different shades of red. Worse, the screens
 * rendered them as independent conditions, so `/knowledge` showed
 * "Loading..." and "unauthorized: missing API key" over an empty 500px canvas
 * at the same time: three states at once, none of them true.
 *
 * `resolveState` is why that cannot happen here. It takes the raw flags and
 * returns exactly one state, so exclusivity is a property of the code rather
 * than something each screen has to remember.
 *
 * None of this touches why a request failed. The error still happens; it just
 * stops looking like a broken page.
 */

import type { ReactNode } from "react"
import { useI18n } from "@/lib/i18n"

export type ViewState = "loading" | "error" | "empty" | "ready"

/**
 * The one place that decides what a screen is showing.
 *
 * Order matters and is deliberate: an error outranks a pending request,
 * because a screen that failed is not still loading — that exact confusion
 * is what `/knowledge` was displaying.
 */
export function resolveState({
  loading,
  error,
  empty,
}: {
  loading: boolean
  error: unknown
  empty: boolean
}): ViewState {
  if (error) return "error"
  if (loading) return "loading"
  if (empty) return "empty"
  return "ready"
}

export function Loading({ title, hint }: { title?: string; hint?: string }) {
  const { t } = useI18n()
  return (
    <div className="state" role="status" aria-live="polite">
      <div style={{ display: "flex", alignItems: "center", justifyContent: "center", gap: 10, marginBottom: hint ? 6 : 0 }}>
        <span className="spinner" aria-hidden="true" />
        <span className="state-title" style={{ margin: 0 }}>{title ?? t("common.loading")}</span>
      </div>
      {hint && <p className="state-hint">{hint}</p>}
    </div>
  )
}

/**
 * An error a person can read, with the machine's own words kept underneath
 * rather than thrown away — a developer needs `unauthorized: missing API key`,
 * and a visitor needs to know the graph is not broken.
 */
export function ErrorState({
  title,
  hint,
  detail,
  onRetry,
  retryLabel,
}: {
  title: string
  hint?: string
  detail?: string | null
  onRetry?: () => void
  retryLabel?: string
}) {
  const { t } = useI18n()
  return (
    <div className="state state-error" role="alert">
      <p className="state-title">{title}</p>
      {hint && <p className="state-hint">{hint}</p>}
      {detail && <div className="state-detail">{detail}</div>}
      {onRetry && (
        <div className="state-actions">
          <button className="btn-ghost btn-sm btn-inline" onClick={onRetry}>
            {retryLabel ?? t("common.retry")}
          </button>
        </div>
      )}
    </div>
  )
}

export function EmptyState({
  title,
  hint,
  children,
}: {
  title: string
  hint?: string
  children?: ReactNode
}) {
  return (
    <div className="state">
      <p className="state-title">{title}</p>
      {hint && <p className="state-hint">{hint}</p>}
      {children && <div className="state-actions">{children}</div>}
    </div>
  )
}
