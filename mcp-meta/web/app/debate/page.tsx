"use client";

import { useState } from "react";
import Markdown from "react-markdown";
import { InputBar } from "@/components/input-bar";
import {
  hireAgent,
  buildPayment,
  submitPayment,
  prepareWallet,
  streamExecution,
  getResult,
  type DebateResult,
  type PaymentInfo,
  type ExecutionEvent,
} from "@/lib/marketplace";
import { connectWallet, signTransaction } from "@/lib/wallet";
import { getWalletAuthHeader } from "@/lib/wallet-auth";
import { HORIZON_URL } from "@/lib/stellar-env";

function stripCodeFences(text: string): string {
  // Remove ```markdown ... ``` wrappers that LLMs add
  let cleaned = text.trim();
  if (cleaned.startsWith("```")) {
    // Remove opening fence (```markdown or ```)
    cleaned = cleaned.replace(/^```\w*\n?/, "");
  }
  if (cleaned.endsWith("```")) {
    cleaned = cleaned.slice(0, -3);
  }
  return cleaned.trim();
}

const AGENT_COLORS: Record<string, { border: string; bg: string }> = {
  advocate: { border: "rgba(59,130,246,0.3)", bg: "rgba(59,130,246,0.08)" },
  skeptic: { border: "rgba(245,158,11,0.3)", bg: "rgba(245,158,11,0.08)" },
};

const AGENT_LABELS: Record<string, string> = {
  advocate: "Advocate",
  skeptic: "Skeptic",
};

const EXAMPLES = [
  "PostgreSQL or MongoDB for user profiles? Structured data, complex relations, team knows SQL, 10k users.",
  "Kubernetes or Docker Compose for staging? 2-person team, 4 services, AWS, plan to scale in 18 months.",
  "Build custom auth or use Auth0? Team of 3, launching in 6 weeks, B2B SaaS.",
];

export default function DebatePage() {
  const [question, setQuestion] = useState("");
  const [loading, setLoading] = useState(false);
  const [currentStep, setCurrentStep] = useState("");
  const [result, setResult] = useState<any | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [activeTab, setActiveTab] = useState<"debate" | "adr">("debate");
  const [paymentInfo, setPaymentInfo] = useState<PaymentInfo | null>(null);
  const [walletKey, setWalletKey] = useState<string | null>(null);

  // Hire adr-debate agent — gets 402 with pricing
  const runDebate = async (q: string) => {
    setQuestion(q);
    setLoading(true);
    setError(null);
    setResult(null);
    setPaymentInfo(null);
    setCurrentStep("Requesting pricing...");
    setActiveTab("debate");

    try {
      const res = await hireAgent("adr-debate", q, 100);
      if (res.status === 402 && res.paymentInfo) {
        setPaymentInfo(res.paymentInfo);
        setCurrentStep("");
        setLoading(false);
        return;
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to request debate");
    } finally {
      if (!paymentInfo) {
        setLoading(false);
        setCurrentStep("");
      }
    }
  };

  // Pay and execute — same flow as single agent hire
  const payAndExecute = async () => {
    if (!paymentInfo) return;
    setLoading(true);
    setError(null);

    try {
      // Connect wallet
      setCurrentStep("Connecting wallet...");
      let key = walletKey;
      if (!key) {
        const wallet = await connectWallet();
        key = wallet.publicKey;
        setWalletKey(key);
      }

      // Prepare wallet (trustline + USDC)
      setCurrentStep("Preparing wallet...");
      const status = await prepareWallet(key);
      if (!status.has_trustline && status.trustline_xdr) {
        setCurrentStep("Signing trustline...");
        const signedTrustline = await signTransaction(status.trustline_xdr);
        const formData = new URLSearchParams({ tx: signedTrustline });
        const horizonRes = await fetch(`${HORIZON_URL}/transactions`, {
          method: "POST", body: formData,
        });
        if (!horizonRes.ok) throw new Error(`Trustline failed: ${(await horizonRes.text()).slice(0, 200)}`);
        await new Promise((r) => setTimeout(r, 3000));
      }

      // Build payment via FastAPI (not Horizon directly)
      setCurrentStep("Building payment...");
      const amount = paymentInfo.budget_ceiling || paymentInfo.estimated_cost || paymentInfo.price || "0.25";
      const tx = await buildPayment("adr-debate", amount, key);

      // Sign with Freighter
      setCurrentStep("Sign in your wallet...");
      const signedXdr = await signTransaction(tx.unsigned_xdr);

      // Submit via FastAPI (handles Soroban RPC + confirmation polling)
      setCurrentStep("Submitting payment...");
      const hire = await submitPayment(signedXdr, "adr-debate", question, 100, tx.task_id);

      // Execute via SSE stream
      setCurrentStep("Running debate...");
      let walletAuth: string | undefined;
      try { walletAuth = await getWalletAuthHeader(); } catch { /* skip */ }

      await new Promise<void>((resolve, reject) => {
        streamExecution(
          hire.task_id,
          (evt) => {
            if (evt.type === "log" || evt.type === "tool_call") {
              setCurrentStep(evt.message || "Processing...");
            }
          },
          async () => {
            try {
              setCurrentStep("Settling...");
              const res = await getResult(hire.task_id, walletAuth);
              const output = res.output as string || "";
              // Parse debate output (JSON with adr, transcript, decision, cost)
              let debateData: any = {};
              try {
                debateData = JSON.parse(output);
              } catch {
                debateData = { adr: output };
              }
              // Merge settlement info from the result response
              debateData._settlement = res.settlement;
              debateData._settlement_status = res.settlement_status;
              debateData._escrow_tx = res.escrow_tx;
              debateData._compliant = res.compliant;
              setResult(debateData);
              setPaymentInfo(null);
              resolve();
            } catch (err) {
              reject(err);
            }
          },
          walletAuth,
        );
      });
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      if (msg.includes("cancel") || msg.includes("reject") || msg.includes("User")) {
        setError("Wallet signature cancelled. No payment was made.");
      } else {
        setError(msg);
      }
    } finally {
      setLoading(false);
      setCurrentStep("");
    }
  };

  return (
    <div className="max-w-4xl mx-auto">
      <h1 className="text-xl font-medium mb-2" style={{ color: "var(--text)" }}>Architecture Debate</h1>
      <p className="text-sm mb-6" style={{ color: "var(--dim)" }}>
        Four independent agents debate your architecture decision. Each has isolated reasoning — they only see what the other said, not how they thought.
      </p>

      <div className="mb-6">
        <InputBar
          onSubmit={runDebate}
          placeholder="Describe your architecture decision..."
        />
      </div>

      <div className="flex gap-2 mb-8 flex-wrap">
        {EXAMPLES.map((ex) => (
          <button
            key={ex}
            onClick={() => runDebate(ex)}
            disabled={loading}
            className="text-xs rounded-full px-3 py-1 border transition-colors disabled:opacity-50"
            style={{ color: "var(--dim)", borderColor: "var(--border)" }}
          >
            {ex.slice(0, 50)}...
          </button>
        ))}
      </div>

      {/* Payment required */}
      {paymentInfo && !loading && !result && (
        <div className="rounded-xl p-6 mb-6 border" style={{ background: "var(--surface)", borderColor: "var(--border)" }}>
          <h2 className="text-sm font-medium mb-4" style={{ color: "var(--muted)" }}>Payment Required</h2>
          <div className="space-y-3 mb-6">
            <div className="flex justify-between">
              <span className="text-sm" style={{ color: "var(--muted)" }}>Estimated cost</span>
              <span className="text-lg font-semibold">
                {paymentInfo.estimated_cost || paymentInfo.price} USDC
              </span>
            </div>
            {paymentInfo.budget_ceiling && (
              <div className="flex justify-between">
                <span className="text-sm" style={{ color: "var(--muted)" }}>Budget lock</span>
                <span className="text-sm font-mono">{paymentInfo.budget_ceiling} USDC</span>
              </div>
            )}
            <div className="text-xs" style={{ color: "var(--dim)" }}>
              Network: {paymentInfo.network} · Escrow: {paymentInfo.payTo.slice(0, 8)}...{paymentInfo.payTo.slice(-4)}
            </div>
          </div>
          <div className="rounded-lg p-3 text-xs mb-4" style={{ background: "rgba(59,130,246,0.08)", borderColor: "rgba(59,130,246,0.3)", color: "var(--accent)" }}>
            USDC locked in escrow. Refunded if execution fails.
          </div>
          <div className="flex gap-3">
            <button
              onClick={() => { setPaymentInfo(null); setQuestion(""); }}
              className="px-4 py-2 border rounded-lg text-sm"
              style={{ borderColor: "var(--border)", color: "var(--muted)" }}
            >
              Cancel
            </button>
            <button
              onClick={payAndExecute}
              className="px-6 py-2 rounded-lg text-sm font-medium"
              style={{ background: "var(--text)", color: "var(--bg)" }}
            >
              {walletKey ? "Pay and run debate" : "Connect wallet and pay"}
            </button>
          </div>
        </div>
      )}

      {/* Loading state */}
      {loading && (
        <div className="rounded-xl p-6 border" style={{ background: "var(--surface)", borderColor: "var(--border)" }}>
          <div className="flex items-center gap-3">
            <span className="animate-pulse text-lg" style={{ color: "var(--accent)" }}>●</span>
            <span style={{ color: "var(--muted)" }}>{currentStep || "Agents are debating..."}</span>
          </div>
          <p className="text-xs mt-2" style={{ color: "var(--dim)" }}>
            This takes 30-60 seconds — 4 agents making 6+ LLM calls
          </p>
        </div>
      )}

      {/* Error */}
      {error && (
        <div className="rounded-xl p-4 text-sm" style={{ background: "rgba(239,68,68,0.08)", borderColor: "rgba(239,68,68,0.2)", color: "#ef4444" }}>
          {error}
        </div>
      )}

      {/* Result */}
      {result && (
        <div>
          {/* Stats bar */}
          <div className="flex items-center gap-4 mb-4 text-xs" style={{ color: "var(--dim)" }}>
            {result.rounds && <><span>{result.rounds} rounds</span><span>·</span></>}
            {result.concession_detected != null && (
              <><span>{result.concession_detected ? "Concession reached" : "Max rounds"}</span><span>·</span></>
            )}
            {result.cost?.total_tokens_in != null && (
              <span>{(result.cost.total_tokens_in || 0) + (result.cost.total_tokens_out || 0)} tokens</span>
            )}
            {result.cost?.estimated_cost_usd != null && (
              <><span>·</span><span>${result.cost.estimated_cost_usd.toFixed(4)}</span></>
            )}
          </div>

          {/* Tabs */}
          <div className="flex gap-1 mb-4">
            <button
              onClick={() => setActiveTab("debate")}
              className="px-4 py-2 text-sm rounded-lg transition-colors"
              style={
                activeTab === "debate"
                  ? { background: "var(--text)", color: "var(--bg)" }
                  : { background: "var(--surface2)", color: "var(--muted)" }
              }
            >
              Debate
            </button>
            <button
              onClick={() => setActiveTab("adr")}
              className="px-4 py-2 text-sm rounded-lg transition-colors"
              style={
                activeTab === "adr"
                  ? { background: "var(--text)", color: "var(--bg)" }
                  : { background: "var(--surface2)", color: "var(--muted)" }
              }
            >
              ADR Document
            </button>
          </div>

          {/* Debate transcript */}
          {activeTab === "debate" && (
            <div className="space-y-4">
              {(result.transcript || []).length > 0 ? result.transcript.map((turn: any, i: number) => {
                const agentStyle = AGENT_COLORS[turn.agent];
                return (
                  <div
                    key={i}
                    className="border rounded-xl p-4"
                    style={
                      agentStyle
                        ? { borderColor: agentStyle.border, background: agentStyle.bg }
                        : { borderColor: "var(--border)", background: "var(--surface)" }
                    }
                  >
                    <div className="flex items-center gap-2 mb-2">
                      <span className="text-xs font-semibold uppercase tracking-wide" style={{ color: "var(--muted)" }}>
                        {AGENT_LABELS[turn.agent] || turn.agent}
                      </span>
                      <span className="text-xs" style={{ color: "var(--dim)" }}>Round {turn.round}</span>
                    </div>
                    <div className="prose prose-sm max-w-none break-words" style={{ color: "var(--text)" }}>
                      <Markdown>{stripCodeFences(turn.text || turn.content || "")}</Markdown>
                    </div>
                  </div>
                );
              }) : (
                /* No structured transcript — show the full output as markdown */
                <div className="border rounded-xl p-6" style={{ borderColor: "var(--border)", background: "var(--surface)" }}>
                  <div className="prose prose-sm max-w-none break-words" style={{ color: "var(--text)" }}>
                    <Markdown>{stripCodeFences(result.adr || "")}</Markdown>
                  </div>
                </div>
              )}

              {/* Decision (if available) */}
              {result.decision && (
                <div className="border-2 rounded-xl p-4" style={{ borderColor: "rgba(34,197,94,0.3)", background: "rgba(34,197,94,0.08)" }}>
                  <div className="flex items-center gap-2 mb-2">
                    <span className="text-xs font-semibold uppercase tracking-wide" style={{ color: "var(--green)" }}>
                      Decision
                    </span>
                  </div>
                  <div className="prose prose-sm max-w-none break-words" style={{ color: "var(--text)" }}>
                    <Markdown>{stripCodeFences(result.decision)}</Markdown>
                  </div>
                </div>
              )}
            </div>
          )}

          {/* ADR document */}
          {activeTab === "adr" && (
            <div className="rounded-xl p-8 overflow-hidden border" style={{ background: "var(--surface)", borderColor: "var(--border)" }}>
              <div className="prose prose-sm max-w-none
                prose-headings:font-semibold
                prose-h1:text-xl prose-h1:pb-3 prose-h1:mb-4
                prose-h2:text-base prose-h2:mt-6 prose-h2:mb-2
                prose-p:text-sm prose-p:leading-relaxed
                prose-ul:my-2 prose-ol:my-2
                break-words
              " style={{ color: "var(--text)" }}>
                <Markdown>{stripCodeFences(result.adr)}</Markdown>
              </div>
              <div className="mt-6 pt-4 flex gap-3" style={{ borderTop: "1px solid var(--border)" }}>
                <button
                  onClick={() => {
                    navigator.clipboard.writeText(result.adr);
                  }}
                  className="text-xs border rounded-lg px-4 py-2 transition-colors"
                  style={{ color: "var(--muted)", borderColor: "var(--border)" }}
                >
                  Copy markdown
                </button>
                <button
                  onClick={() => {
                    const blob = new Blob([result.adr], { type: "text/markdown" });
                    const url = URL.createObjectURL(blob);
                    const a = document.createElement("a");
                    a.href = url;
                    a.download = `adr-${Date.now()}.md`;
                    a.click();
                  }}
                  className="text-xs border rounded-lg px-4 py-2 transition-colors"
                  style={{ color: "var(--muted)", borderColor: "var(--border)" }}
                >
                  Download .md
                </button>
              </div>
            </div>
          )}

          {/* Settlement info */}
          <div className="mt-6 pt-4 space-y-2" style={{ borderTop: "1px solid var(--border)" }}>
            {result._settlement && (
              <div className="rounded-xl p-4 border" style={{ background: "var(--surface)", borderColor: "var(--border)" }}>
                <div className="text-xs mb-2" style={{ color: "var(--dim)" }}>Settlement</div>
                <div className="text-sm space-y-1">
                  <div className="flex justify-between">
                    <span style={{ color: "var(--muted)" }}>Actual cost</span>
                    <span className="font-mono">{result._settlement.actual_cost_usdc} USDC</span>
                  </div>
                  <div className="text-xs font-mono" style={{ color: "var(--dim)" }}>
                    TX: {result._settlement.settle_tx?.slice(0, 16)}...
                  </div>
                </div>
              </div>
            )}
            <div className="text-xs flex items-center gap-2" style={{ color: "var(--dim)" }}>
              <span>Status: {String(result._settlement_status || "pending")}</span>
              {result._compliant != null && (
                <span>Compliance: {result._compliant ? "PASS" : "FAIL"}</span>
              )}
              {result._escrow_tx && (
                <span>Escrow: {result._escrow_tx.slice(0, 12)}...</span>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
