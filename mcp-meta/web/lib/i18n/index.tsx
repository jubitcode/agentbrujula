"use client"

/**
 * Brújula in two languages, from one application.
 *
 * There is no library here on purpose. `next-intl` is the standard for Next,
 * but its shape is `/[locale]/...` — a second copy of every route, which is
 * exactly what we are not doing. Used without routes it still wants a plugin,
 * a middleware and a request config: more machinery than the problem has.
 *
 * What the problem actually is: every screen in this app is a client
 * component, so a React context reaches all of the translatable text. That is
 * the whole design.
 *
 * Keys are flat and dotted (`home.title`) rather than nested. Flat means
 * `en` can be typed as `typeof es`, and TypeScript then refuses to compile a
 * dictionary that is missing a key or has invented one — which is the failure
 * this kind of file always has, and the only one worth spending types on.
 */

import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
  type ReactNode,
} from "react"
import { es } from "./es"
import { en } from "./en"
import { DEFAULT_LANG, LANG_COOKIE, type Lang } from "./lang"

// Re-exported so screens have one import to remember. The definitions live in
// ./lang because the server layout reads the cookie and cannot import from a
// "use client" module.
export { LANGS, LANG_COOKIE, DEFAULT_LANG, normaliseLang, type Lang } from "./lang"

/** Spanish is the source dictionary; English is checked against its keys. */
export type Dict = typeof es
export type Key = keyof Dict

const DICTS: Record<Lang, Dict> = { es, en }

type Vars = Record<string, string | number>

type I18n = {
  lang: Lang
  setLang: (l: Lang) => void
  /** A single string. Missing key → the key itself, loud in dev, never blank. */
  t: (key: Key, vars?: Vars) => string
  /** A list of strings, for bulleted content. */
  tl: (key: Key, vars?: Vars) => string[]
}

const I18nContext = createContext<I18n | null>(null)

function interpolate(text: string, vars?: Vars): string {
  if (!vars) return text
  return text.replace(/\{(\w+)\}/g, (match, name) =>
    name in vars ? String(vars[name]) : match,
  )
}

export function I18nProvider({
  initial,
  children,
}: {
  initial: Lang
  children: ReactNode
}) {
  const [lang, setLangState] = useState<Lang>(initial)

  const setLang = useCallback((next: Lang) => {
    setLangState(next)
    // A year, site-wide, and `SameSite=Lax` so it survives normal navigation
    // without being sent on cross-site requests. No Server Action and no
    // endpoint: the server only ever reads this.
    document.cookie = `${LANG_COOKIE}=${next}; path=/; max-age=31536000; SameSite=Lax`
    // The server set `lang` on the first render; keep it true after a change
    // without a reload, because screen readers and the browser's own
    // translation prompt both read it live.
    document.documentElement.lang = next
  }, [])

  const value = useMemo<I18n>(() => {
    const dict = DICTS[lang]
    const fallback = DICTS[DEFAULT_LANG]
    const lookup = (key: Key) => dict[key] ?? fallback[key]

    return {
      lang,
      setLang,
      t: (key, vars) => {
        const found = lookup(key)
        if (found === undefined) return key
        return interpolate(Array.isArray(found) ? found.join(" ") : found, vars)
      },
      tl: (key, vars) => {
        const found = lookup(key)
        if (found === undefined) return []
        const list = Array.isArray(found) ? found : [found]
        return list.map((item) => interpolate(item, vars))
      },
    }
  }, [lang, setLang])

  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>
}

/**
 * The hook every screen uses.
 *
 * Outside a provider it falls back to Spanish rather than throwing. Several
 * component tests render a single component with no wrapper, and a hook that
 * throws there would turn "this button is missing" into "cannot read context"
 * — a worse message for the same failure.
 */
export function useI18n(): I18n {
  const ctx = useContext(I18nContext)
  if (ctx) return ctx
  const dict = DICTS[DEFAULT_LANG]
  return {
    lang: DEFAULT_LANG,
    setLang: () => {},
    t: (key, vars) => {
      const found = dict[key]
      if (found === undefined) return key
      return interpolate(Array.isArray(found) ? found.join(" ") : found, vars)
    },
    tl: (key, vars) => {
      const found = dict[key]
      if (found === undefined) return []
      return (Array.isArray(found) ? found : [found]).map((i) =>
        interpolate(i, vars),
      )
    },
  }
}

/** Shorthand for the common case: `const t = useT()`. */
export function useT() {
  return useI18n().t
}
