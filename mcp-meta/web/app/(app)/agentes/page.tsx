"use client"

/**
 * The catalogue. What a person sees first, and the only screen that has to
 * answer "what can I do here".
 *
 * It used to be a landing page: hero, five numbered steps, three contract
 * addresses. All true, none of it a thing you could use — the only link out of
 * it went to /hire, so everything else the project had built was reachable
 * only by typing a URL.
 *
 * The agents are read from mcp-meta's agent card, which is what the registry
 * actually serves. Nothing here is invented: the mockup this follows showed
 * four agents with prices, star ratings and "1.284 contratos", and three of
 * those four do not exist. Showing a number we cannot stand behind is worse
 * than showing none.
 */

import { useEffect, useMemo, useState } from "react"
import Link from "next/link"
import type { AgentCard } from "@/lib/types"
import { getAgentCard } from "@/lib/api"
import { useI18n, type Key } from "@/lib/i18n"
import { EmptyState, ErrorState, Loading, resolveState } from "@/components/ui/states"
import { DEMO_PRICE_USDC } from "@/lib/pm-contract"

/** The one agent with a screen of its own. Everything else runs through /hire. */
const FEATURED = "pm-planner"

/**
 * Skills left over from test runs and demos. They are in the registry and they
 * are not products; a catalogue that lists `test-tool-1792540296` is telling
 * the visitor something false about what this is.
 */
const JUNK = /^(test-|demo-|tool-registry|httpcli)/

/**
 * Tag groups.
 *
 * The filter row showed one chip per tag: 25 of them, two full rows on
 * desktop and a wall on a phone, with `adr` and `decision` sitting next to
 * each other as if they were different things.
 *
 * A word on where these come from, because it matters: there is no category
 * in the data. Not in the contract, not in the manifests, not in any
 * endpoint — only loose `tags`, on-chain `capabilities` that do not match
 * them, and `model_tier`. So this grouping is a presentation decision made
 * here, over tags that are real. If it should be data, it belongs in the
 * manifests; until then the tags keep working as search terms.
 */
const GROUPS: { key: Key; tags: string[] }[] = [
  { key: "home.group.planning", tags: ["planificación", "planificacion", "producto", "proyecto", "pm", "planning"] },
  { key: "home.group.research", tags: ["research", "web", "trends", "news", "monitoring", "competitive", "intelligence", "business"] },
  { key: "home.group.writing", tags: ["writing", "content", "blog", "summarize"] },
  { key: "home.group.technical", tags: ["architecture", "debate", "adr", "decision", "engineering"] },
  { key: "home.group.knowledge", tags: ["knowledge", "memory", "search", "learn"] },
]

/** Where a skill goes when it is picked. Only the PM has its own path today. */
function destination(id: string) {
  if (id === FEATURED) return "/pm"
  if (id === "adr-debate") return "/debate"
  if (id.startsWith("knowledge-")) return "/knowledge"
  return "/hire"
}

export default function HomePage() {
  const { t, tl } = useI18n()
  const [card, setCard] = useState<AgentCard | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [query, setQuery] = useState("")
  const [group, setGroup] = useState<Key | null>(null)

  useEffect(() => {
    let cancelled = false
    getAgentCard()
      .then((c) => !cancelled && setCard(c))
      .catch((e) => !cancelled && setError(e instanceof Error ? e.message : String(e)))
      .finally(() => !cancelled && setLoading(false))
    return () => {
      cancelled = true
    }
  }, [])

  const skills = useMemo(
    () =>
      (card?.skills ?? [])
        .filter((s) => s.id && !JUNK.test(s.id))
        // The agent card returns whatever order the registry walked, which
        // changes between loads: the catalogue appeared to reshuffle itself.
        // Sorted by name so the same list is the same list.
        .sort((a, b) => (a.name || a.id).localeCompare(b.name || b.id, "es")),
    [card],
  )

  /** Only the groups that actually match something in the registry today. */
  const groups = useMemo(() => {
    const present = new Set<string>()
    for (const s of skills) for (const tag of s.tags ?? []) present.add(tag.toLowerCase())
    return GROUPS.filter((g) => g.tags.some((tag) => present.has(tag)))
  }, [skills])

  const shown = useMemo(() => {
    const q = query.trim().toLowerCase()
    const active = group ? GROUPS.find((g) => g.key === group) : null
    return skills.filter((s) => {
      const tags = (s.tags ?? []).map((x) => x.toLowerCase())
      if (active && !active.tags.some((tag) => tags.includes(tag))) return false
      if (!q) return true
      // The individual tags stay searchable even though they are no longer
      // each a chip — that was the point of grouping rather than hiding.
      //
      // The group names are searchable too: offering "Investigación" as a
      // filter and then returning nothing for the same word typed into the
      // box reads as the search being broken.
      const groupNames = GROUPS.filter((g) => g.tags.some((tag) => tags.includes(tag)))
        .map((g) => t(g.key).toLowerCase())
      return [s.name, s.description, ...tags, ...groupNames]
        .join(" ")
        .toLowerCase()
        .includes(q)
    })
  }, [skills, query, group, t])

  const featured = shown.find((s) => s.id === FEATURED)
  const rest = shown.filter((s) => s.id !== FEATURED)
  const state = resolveState({ loading, error, empty: shown.length === 0 })

  return (
    <div className="page">
      <section style={{ paddingTop: 36, paddingBottom: 24 }}>
        <div className="section-label">{`// ${t("home.kicker")}`}</div>
        <h1 className="t-hero">
          {t("home.titleA")}
          <br />
          {t("home.titleB")}
        </h1>
        <p className="t-intro">{t("home.intro")}</p>
      </section>

      <section style={{ marginBottom: 24 }}>
        <input
          className="input"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={t("home.search")}
          aria-label={t("home.searchLabel")}
          style={{ marginBottom: 12 }}
        />
        <div style={{ display: "flex", flexWrap: "wrap", gap: 8 }}>
          <button className="chip" aria-pressed={group === null} onClick={() => setGroup(null)}>
            {t("home.all")}
          </button>
          {groups.map((g) => (
            <button
              key={g.key}
              className="chip"
              aria-pressed={group === g.key}
              onClick={() => setGroup(group === g.key ? null : g.key)}
            >
              {t(g.key)}
            </button>
          ))}
        </div>
      </section>

      {/* Exactly one of these four. `resolveState` is what guarantees it. */}
      {state === "loading" && <Loading title={t("home.loading")} />}

      {state === "error" && (
        <ErrorState title={t("home.error")} hint={t("home.errorHint")} detail={error} />
      )}

      {state === "empty" && (
        <EmptyState title={t("home.empty")} hint={t("home.emptyHint")} />
      )}

      {state === "ready" && (
        <>
          {featured && <FeaturedCard skill={featured} t={t} tl={tl} />}

          {rest.length > 0 && (
            <section style={{ paddingBottom: 60 }}>
              <div className="section-label" style={{ marginBottom: 14 }}>
                {`// ${t("home.also")}`}
              </div>
              <div className="grid-auto">
                {rest.map((s) => (
                  <Link key={s.id} href={destination(s.id)} style={{ textDecoration: "none", color: "inherit" }}>
                    <div className="card" style={{ height: "100%", display: "flex", flexDirection: "column" }}>
                      <div className="t-h3">{s.name}</div>
                      <div className="t-small t-wrap" style={{ flex: 1 }}>{s.description}</div>
                      <div style={{ display: "flex", flexWrap: "wrap", gap: 6, marginTop: 12 }}>
                        {(s.tags ?? []).slice(0, 3).map((tag) => (
                          <span key={tag} className="tag">{tag}</span>
                        ))}
                      </div>
                    </div>
                  </Link>
                ))}
              </div>
            </section>
          )}
        </>
      )}
    </div>
  )
}

function FeaturedCard({
  skill,
  t,
  tl,
}: {
  skill: { id: string; name: string; description: string; examples?: string[] }
  t: (k: Key) => string
  tl: (k: Key) => string[]
}) {
  return (
    <Link href="/pm" style={{ textDecoration: "none", color: "inherit" }}>
      <section
        className="card"
        style={{
          marginBottom: 32,
          background: "rgba(59,130,246,.06)",
          borderColor: "rgba(59,130,246,.25)",
        }}
      >
        <div style={{ display: "flex", alignItems: "baseline", gap: 10, marginBottom: 10 }}>
          <span style={{ fontFamily: "var(--mono)", fontSize: 11, color: "var(--accent)", textTransform: "uppercase", letterSpacing: ".06em" }}>
            {t("home.startHere")}
          </span>
        </div>
        <h2 className="t-h2">{skill.name}</h2>
        <p className="t-intro t-wrap" style={{ margin: "0 0 18px" }}>{skill.description}</p>

        <div className="grid-auto" style={{ gap: 10, marginBottom: 20 }}>
          {tl("home.featureList").map((d) => (
            <div key={d} className="t-small" style={{ display: "flex", gap: 7 }}>
              <span style={{ color: "var(--accent)" }} aria-hidden="true">·</span>
              {d}
            </div>
          ))}
        </div>

        {/* Real examples, from the manifest. The old home shipped one that
            mapped to a capability no agent had, so the most obvious thing to
            click answered "nobody available for this task right now". */}
        {skill.examples && skill.examples.length > 0 && (
          <div style={{ marginBottom: 20 }}>
            <div className="field-label">{t("home.examples")}</div>
            {skill.examples.slice(0, 2).map((e) => (
              <div key={e} className="t-small t-wrap" style={{ color: "var(--dim)", fontStyle: "italic", lineHeight: 1.7 }}>
                “{e}”
              </div>
            ))}
          </div>
        )}

        <div className="card-foot" style={{ borderTopColor: "rgba(59,130,246,.2)" }}>
          {/* Priced like the rest of the catalogue, and labelled for what it
              is: the other agents take their rate from the registry on
              chain, and the PM has no entry there. */}
          <span className="price">
            {DEMO_PRICE_USDC} <small>USDC · {t("contract.priceNote")}</small>
          </span>
          <span className="btn-primary btn-sm">{t("home.cta")} →</span>
        </div>
      </section>
    </Link>
  )
}
