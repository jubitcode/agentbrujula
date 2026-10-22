"use client";

import { useEffect, useState } from "react";
import { useI18n, type Key } from "@/lib/i18n";
import { EmptyState, ErrorState, Loading, resolveState } from "@/components/ui/states";

const MARKETPLACE_API =
  process.env.NEXT_PUBLIC_MARKETPLACE_URL || "http://localhost:5050";

type EscrowRow = {
  task_id: string;
  agent_handle: string;
  escrow_tx: string;
  status: string;
  settled: boolean;
  refunded: boolean;
  deadline_unix: number;
  created_at: number;
  refund_tx_hash: string;
  tx_url: string;
};

type EscrowList = {
  escrows: EscrowRow[];
  count: number;
  escrow_contract: string;
  contract_url: string;
  network: string;
};

/** What the contract says, fetched per row after the list renders. */
type ChainState = {
  status: "found" | "not_found" | string;
  escrow_status?: string;
  amount?: string;
  client?: string;
  agent?: string;
  deadline?: number;
};

/**
 * What our own database believes happened. Not authoritative.
 *
 * Returns a dictionary key when it knows the state, and the raw server string
 * when it does not — a status like `awaiting_manual_reconciliation` has no
 * translation and showing it raw is better than inventing one.
 */
function localBelief(row: EscrowRow): { key?: Key; raw?: string } {
  if (row.settled) return { key: "escrows.state.settled" };
  if (row.refunded) return { key: "escrows.state.refunded" };
  if (!row.status) return { key: "escrows.state.pending" };
  return { raw: row.status };
}

/**
 * How a row ended up — **as the contract says**, not as our database hopes.
 *
 * The chain is authoritative here and the local record is not, because the two
 * can and do disagree. This page is the public claim about where money went;
 * stating "refunded" because a row says so, while the contract still holds the
 * funds as `Created`, would be asserting a payment that never happened.
 *
 * When the database disagrees with the chain, that disagreement is shown
 * rather than resolved silently — a stale local row is a reconciliation bug
 * worth seeing, and hiding it is how it survives.
 */
type Outcome = {
  /** The state as a dictionary key, when we recognise it. */
  key?: Key;
  /** The state as the server or contract spelled it, when we do not. */
  raw?: string;
  tone: string;
  detail?: Key;
  authoritative: boolean;
  disagrees: boolean;
  local: { key?: Key; raw?: string };
};

function outcome(row: EscrowRow, chain: ChainState | undefined): Outcome {
  const onChain = chain?.escrow_status;
  const local = localBelief(row);

  if (onChain) {
    const fromChain: Record<string, { key: Key; tone: string; detail: Key }> = {
      Settled: { key: "escrows.state.settled", tone: "good", detail: "escrows.detail.settled" },
      Released: { key: "escrows.state.released", tone: "good", detail: "escrows.detail.released" },
      Refunded: { key: "escrows.state.refunded", tone: "warn", detail: "escrows.detail.refunded" },
      Created: { key: "escrows.state.locked", tone: "live", detail: "escrows.detail.locked" },
    };
    const verdict = fromChain[onChain];
    if (!verdict) {
      return { raw: onChain, tone: "idle", authoritative: true, disagrees: false, local };
    }
    // "Locked" against a local row claiming it is finished is the disagreement
    // that matters: money the server thinks it moved is still on the chain.
    const samePending = local.key === "escrows.state.pending" && onChain === "Created";
    const disagrees = local.key !== verdict.key && !samePending;
    return { ...verdict, authoritative: true, disagrees, local };
  }

  // No entry on the chain. Could be an escrow that never existed, or one whose
  // TTL expired and was evicted — either way we cannot confirm an outcome, so
  // we do not state one as fact.
  return { ...local, tone: "idle", detail: undefined, authoritative: false, disagrees: false, local };
}

const TONE: Record<string, { fg: string; bg: string; border: string }> = {
  good: { fg: "var(--green)", bg: "rgba(34,197,94,.10)", border: "rgba(34,197,94,.25)" },
  warn: { fg: "var(--amber)", bg: "rgba(245,158,11,.10)", border: "rgba(245,158,11,.25)" },
  live: { fg: "var(--accent)", bg: "rgba(59,130,246,.10)", border: "rgba(59,130,246,.25)" },
  idle: { fg: "var(--dim)", bg: "transparent", border: "var(--border2)" },
};

function shortId(id: string, head = 10) {
  return id.length > head + 6 ? `${id.slice(0, head)}…${id.slice(-4)}` : id;
}

/**
 * A Stellar transaction hash is 64 hex characters, always.
 *
 * Rows can hold placeholders left by test runs ("tx", "h"). Linking one to an
 * explorer offers a dead link as proof, which is worse than offering nothing.
 */
function isTxHash(value: string): boolean {
  return /^[0-9a-fA-F]{64}$/.test(value);
}

export default function EscrowsPage() {
  const { t } = useI18n();
  const [data, setData] = useState<EscrowList | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [chain, setChain] = useState<Record<string, ChainState>>({});

  useEffect(() => {
    let cancelled = false;

    fetch(`${MARKETPLACE_API}/api/escrows?limit=25`)
      .then((r) => (r.ok ? r.json() : Promise.reject(new Error(`HTTP ${r.status}`))))
      .then((list: EscrowList) => {
        if (cancelled) return;
        setData(list);

        // Read each entry from the contract, one at a time. The list is already
        // on screen by now; these fill in as they land. Sequential on purpose —
        // a burst of simulate calls is slower overall than a queue, and the
        // first call in a cold process pays for the connection.
        (async () => {
          for (const row of list.escrows) {
            if (cancelled) return;
            try {
              const res = await fetch(
                `${MARKETPLACE_API}/api/escrow/${encodeURIComponent(row.task_id)}`,
              );
              if (!res.ok) continue;
              const state: ChainState = await res.json();
              if (!cancelled) setChain((prev) => ({ ...prev, [row.task_id]: state }));
            } catch {
              // A row that cannot be read keeps its server-side status. The
              // page says which rows are chain-confirmed, so a gap is visible
              // rather than papered over.
            }
          }
        })();
      })
      .catch((e) => !cancelled && setError(e.message));

    return () => {
      cancelled = true;
    };
  }, []);

  const state = resolveState({
    loading: !data && !error,
    error,
    empty: !!data && data.escrows.length === 0,
  });

  return (
    <div className="page">
      <section style={{ paddingTop: 12, paddingBottom: 20 }}>
        <div className="section-label">{`// ${t("escrows.kicker")}`}</div>
        <h1 className="t-hero">{t("escrows.title")}</h1>
        <p className="t-intro">{t("escrows.intro")}</p>
      </section>

      <section
        className="card"
        style={{ marginBottom: 24, background: "rgba(59,130,246,.06)", borderColor: "rgba(59,130,246,.2)" }}
      >
        <div className="t-small t-wrap" style={{ color: "var(--accent)", margin: 0 }}>
          <strong style={{ fontWeight: 600 }}>{t("escrows.whoTitle")}</strong>{" "}
          {t("escrows.whoBody")}
        </div>
        {data?.contract_url && (
          <a
            href={data.contract_url}
            target="_blank"
            rel="noopener noreferrer"
            className="mono-id"
            style={{
              display: "inline-block",
              marginTop: 10,
              fontFamily: "var(--mono)",
              fontSize: 11,
              color: "var(--accent)",
            }}
          >
            {data.escrow_contract} ↗
          </a>
        )}
      </section>

      {/* One state at a time. These were three independent conditions, so a
          failed request rendered an error above a list that was also still
          "loading escrows…". */}
      {state === "loading" && <Loading title={t("escrows.loading")} />}

      {state === "error" && (
        <ErrorState title={t("escrows.error")} hint={t("escrows.errorHint")} detail={error} />
      )}

      {state === "empty" && (
        <EmptyState title={t("escrows.empty")} hint={t("escrows.emptyHint")} />
      )}

      <div style={{ display: "grid", gap: 10 }}>
        {state === "ready" && data?.escrows.map((row) => {
          const rowChain = chain[row.task_id];
          const result = outcome(row, rowChain);
          const tone = TONE[result.tone];
          const confirmed = rowChain?.status === "found";
          const label = result.key ? t(result.key) : result.raw ?? "—";

          return (
            <div key={row.task_id} className="card">
              <div
                style={{
                  display: "flex",
                  justifyContent: "space-between",
                  alignItems: "baseline",
                  gap: 12,
                  flexWrap: "wrap",
                  marginBottom: 10,
                }}
              >
                <div style={{ fontFamily: "var(--mono)", fontSize: 13, color: "var(--text)" }}>
                  {row.agent_handle || "—"}
                </div>
                <span
                  style={{
                    fontFamily: "var(--mono)",
                    fontSize: 10,
                    letterSpacing: ".5px",
                    textTransform: "uppercase",
                    color: tone.fg,
                    background: tone.bg,
                    border: `1px solid ${tone.border}`,
                    borderRadius: 999,
                    padding: "3px 10px",
                  }}
                >
                  {label}
                </span>
              </div>

              {result.detail && (
                <div className="t-small" style={{ margin: "0 0 10px" }}>{t(result.detail)}</div>
              )}

              <div
                style={{
                  display: "flex",
                  gap: 20,
                  flexWrap: "wrap",
                  fontFamily: "var(--mono)",
                  fontSize: 11,
                  color: "var(--dim)",
                  fontVariantNumeric: "tabular-nums",
                }}
              >
                <span>
                  {t("escrows.task")} <span style={{ color: "var(--muted)" }}>{shortId(row.task_id)}</span>
                </span>
                {rowChain?.amount && (
                  <span>
                    {t("escrows.amount")} <span style={{ color: "var(--text)" }}>{rowChain.amount} USDC</span>
                  </span>
                )}
                <span>
                  {confirmed ? (
                    <span style={{ color: "var(--green)" }}>✓ {t("escrows.confirmed")}</span>
                  ) : rowChain?.status === "not_found" ? (
                    // Soroban evicts persistent entries once their TTL runs out,
                    // so an old escrow stops being readable even though its
                    // transaction is permanent. Say that, rather than implying
                    // the payment never happened.
                    <span>{t("escrows.notOnChain")}</span>
                  ) : (
                    <span>{t("escrows.checking")}</span>
                  )}
                </span>
              </div>

              {/* Four lines of mono explaining a reconciliation bug sat in the
                  middle of the row, in the same weight as the data. The
                  sentence that matters to a person is kept; the reasoning
                  behind it moves one click away. */}
              {result.disagrees && (
                <details
                  className="disclosure"
                  style={{
                    marginTop: 10,
                    padding: "10px 12px",
                    borderRadius: "var(--r)",
                    border: "1px solid rgba(245,158,11,.3)",
                    background: "rgba(245,158,11,.08)",
                  }}
                >
                  <summary>
                    <span className="t-small" style={{ color: "var(--amber)", margin: 0, fontWeight: 600 }}>
                      {t("escrows.mismatch")}
                    </span>
                  </summary>
                  <p className="t-small t-wrap" style={{ margin: "10px 0 0", color: "var(--amber)" }}>
                    {t("escrows.mismatchDetail", {
                      chain: label,
                      local: result.local.key ? t(result.local.key) : result.local.raw ?? "—",
                    })}
                  </p>
                </details>
              )}

              <div style={{ display: "flex", gap: 14, marginTop: 10, flexWrap: "wrap" }}>
                {row.tx_url && (
                  <a
                    href={row.tx_url}
                    target="_blank"
                    rel="noopener noreferrer"
                    style={{ fontFamily: "var(--mono)", fontSize: 11, color: "var(--accent)" }}
                  >
                    {t("escrows.escrowTx")} ↗
                  </a>
                )}
                {isTxHash(row.refund_tx_hash) && (
                  <a
                    href={`https://stellar.expert/explorer/${data.network === "public" ? "public" : "testnet"}/tx/${row.refund_tx_hash}`}
                    target="_blank"
                    rel="noopener noreferrer"
                    style={{ fontFamily: "var(--mono)", fontSize: 11, color: "var(--green)" }}
                  >
                    {t("escrows.refundTx")} ↗
                  </a>
                )}
              </div>
            </div>
          );
        })}
      </div>

      {state === "ready" && data && data.count > 0 && (
        <p
          style={{
            fontFamily: "var(--mono)",
            fontSize: 11,
            color: "var(--dim)",
            marginTop: 20,
            lineHeight: 1.6,
          }}
        >
          {t(data.count === 1 ? "escrows.count" : "escrows.countPlural", {
            n: data.count,
            network: data.network,
          })}
        </p>
      )}
    </div>
  );
}
