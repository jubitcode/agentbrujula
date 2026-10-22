"use client"

/**
 * ES / EN, in the header.
 *
 * A two-state segmented control rather than a dropdown: with two options a
 * select costs a click to show the user what they already know, and the
 * current language is worth seeing without opening anything.
 */

import { LANGS, useI18n, type Lang } from "@/lib/i18n"

const NAMES: Record<Lang, string> = { es: "ES", en: "EN" }
const FULL: Record<Lang, string> = { es: "Español", en: "English" }

export function LangSwitch() {
  const { lang, setLang, t } = useI18n()

  return (
    <div
      role="group"
      aria-label={t("common.language")}
      style={{
        display: "flex",
        border: "1px solid var(--border2)",
        borderRadius: "var(--radius-pill)",
        overflow: "hidden",
        flexShrink: 0,
      }}
    >
      {LANGS.map((l) => {
        const active = l === lang
        return (
          <button
            key={l}
            type="button"
            onClick={() => setLang(l)}
            aria-pressed={active}
            // The short label is what people read; the full language name is
            // what a screen reader announces, because "EN" on its own is a
            // guess for anyone who cannot see which one is highlighted.
            aria-label={FULL[l]}
            title={FULL[l]}
            style={{
              fontFamily: "var(--mono)",
              fontSize: 11,
              letterSpacing: ".04em",
              padding: "5px 10px",
              border: "none",
              cursor: "pointer",
              background: active ? "var(--surface2)" : "transparent",
              color: active ? "var(--text)" : "var(--dim)",
              transition: "color .15s, background .15s",
            }}
          >
            {NAMES[l]}
          </button>
        )
      })}
    </div>
  )
}
