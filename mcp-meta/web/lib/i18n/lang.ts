/**
 * The language itself — no React, no "use client".
 *
 * This is split out because the root layout is a server component and has to
 * read the cookie there: a module marked "use client" exports client
 * functions, and the server cannot call one. Everything here is plain data
 * and a pure function, so both sides can import it.
 */

export type Lang = "es" | "en"

export const LANGS: Lang[] = ["es", "en"]

/**
 * Where the choice lives.
 *
 * A cookie rather than localStorage because the server has to know the
 * language to emit `<html lang>` in the first byte. Read from localStorage,
 * the language is only known after hydration, and the page shows one
 * language for an instant before swapping — a visible flicker, and this
 * iteration exists to remove those.
 */
export const LANG_COOKIE = "lang"
export const DEFAULT_LANG: Lang = "es"

/** Anything that is not a language we have falls back to Spanish. */
export function normaliseLang(value: string | undefined | null): Lang {
  return value === "en" ? "en" : DEFAULT_LANG
}
