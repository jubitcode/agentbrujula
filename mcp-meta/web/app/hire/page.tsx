"use client";

import { useState, useRef, useCallback, useEffect } from "react";
import { useRouter } from "next/navigation";
import Markdown from "react-markdown";
import { DebateResultView } from "@/components/debate-result";
import {
  discoverAgents,
  hireAgent,
  buildPayment,
  submitPayment,
  prepareWallet,
  streamExecution,
  getResult,
  type Agent,
  type PaymentInfo,
  type UnsignedPayment,
  type ExecutionEvent,
} from "@/lib/marketplace";
import { connectWallet, signTransaction, assertWalletOnExpectedNetwork, getSavedWallet } from "@/lib/wallet";
import { getWalletAuthHeader } from "@/lib/wallet-auth";
import { HORIZON_URL } from "@/lib/stellar-env";

type Step = "input" | "discovering" | "agents" | "hiring" | "payment" | "network_mismatch" | "building_tx" | "signing" | "submitting" | "executing" | "settling" | "done" | "error";

const EXPECTED_NETWORK = (process.env.NEXT_PUBLIC_STELLAR_NETWORK || "TESTNET").toUpperCase() === "PUBLIC" ? "Mainnet" : "Testnet";

const CAPABILITY_LIST = ["research", "writing", "trends", "summarize"] as const;
type Capability = typeof CAPABILITY_LIST[number];

const EXAMPLE_PROMPTS = [
  "Research the x402 protocol and where it fits Stellar",
  "Write a one-paragraph intro about Brújula",
  "Summarize the Soroban escrow contract in 5 bullet points",
  "What is trending in AI infrastructure right now?",
];

// Map a free-form task to a probable capability using keyword cues. Used to
// pre-fill the capability filter so the user does not have to categorise
// their request manually. Returns null when nothing matches; in that case the
// discover call goes out with all four capabilities and lets the registry
// decide.
function detectCapability(task: string): Capability | null {
  const t = task.toLowerCase();
  if (/\b(write|compose|draft|blog|article|post|intro|paragraph|copy)\b/.test(t)) return "writing";
  if (/\b(summari[sz]e|summary|tldr|tl;dr|shorten|condense|brief)\b/.test(t)) return "summarize";
  if (/\b(trend|trending|popular|viral|momentum|what.s.hot)\b/.test(t)) return "trends";
  if (/\b(research|investigate|find.out|look.up|study|explain|deep.dive)\b/.test(t)) return "research";
  return null;
}

const STEPPER_STEPS = ["Describe", "Pick", "Pay", "Watch", "Done"] as const;

/**
 * Translate a raw execution log message (emitted by the FastAPI backend) into
 * something a non-technical user can read. The backend strings are kept
 * intentionally technical for server-side logs; only the chrome rewrites
 * them. New backend phrases get a best-effort prettifier (drop "...", capital
 * letter, strip hashes) so the user never sees raw jargon.
 *
 * Exported so the unit test can pin every translation deterministically.
 */
export function humaniseEventMessage(message: string, type: string): string {
  const m = (message || "").trim();
  if (!m) return "Working…";

  // Exact replacements for known FastAPI strings.
  if (/^sending to a2a$/i.test(m)) return "Reaching out to the agent…";
  if (/^a2a task:\s*[a-f0-9]+$/i.test(m)) return "Agent picked it up";
  if (/^execution complete$/i.test(m)) return "The agent finished";
  if (/^started$/i.test(m)) return "Started";
  if (/^settling$/i.test(m)) return "Finishing up";
  if (/^complete$/i.test(m)) return "Done";

  // Tool-call traces like "search invoked" → "Used: search".
  const toolMatch = m.match(/^(\w[\w-]*)\s+invoked$/i);
  if (toolMatch) return `Used: ${toolMatch[1]}`;

  // [tool_call] search(...) → Used: search
  const bracketTool = m.match(/^\[tool_call\]\s*(\w[\w-]*)/i);
  if (bracketTool) return `Used: ${bracketTool[1]}`;

  // Fallback: strip any long hex hash so we never leak a raw task ID, and
  // drop the trailing "..." (we render the dot ourselves).
  let cleaned = m.replace(/\b[a-f0-9]{16,}\b/gi, "").replace(/\.\.\.$/, "").trim();
  if (!cleaned) cleaned = type === "complete" ? "Done" : "Working";
  // Title-case the first character so events read like UI copy.
  return cleaned.charAt(0).toUpperCase() + cleaned.slice(1);
}

/**
 * Strip machine-named labels from the LLM's rendered markdown. The agent
 * occasionally outputs headings like "## Hebbian Knowledge Graph" because
 * the upstream prompt references the memory store by its engineering name.
 * General users do not know what Hebbian is, so we rewrite those mentions
 * to plain English. The semantic chip ABOVE the result explains the
 * benefit; this helper just cleans up the body.
 *
 * Exported for direct unit testing — no React or DOM needed.
 */
export function humaniseResultMarkdown(md: string): string {
  if (!md) return "";

  let out = md;

  // 1. Headings of any level: # / ## / ### / etc. (with optional bold/italic).
  out = out.replace(
    /^(#{1,6}\s+)\*{0,2}\s*Hebbian\s+Knowledge\s+Graph\*{0,2}\s*:?$/gim,
    "$1What we already knew",
  );

  // 2. Bold-or-italic-prefixed inline label that the LLM sometimes uses as a
  //    section header instead of a real markdown heading.
  out = out.replace(
    /\*{1,2}\s*Hebbian\s+Knowledge\s+Graph\s*\*{1,2}\s*:?/gi,
    "**What we already knew:**",
  );

  // 3. Any other Hebbian mentions get rewritten to "memory" so the body
  //    text reads cleanly to a non-technical audience.
  out = out.replace(/\bHebbian\b/g, "memory");

  return out;
}

function stepperIndex(step: Step): number {
  if (step === "input") return 0;
  if (step === "discovering" || step === "agents") return 1;
  if (
    step === "hiring" || step === "payment" || step === "network_mismatch" ||
    step === "building_tx" || step === "signing" || step === "submitting"
  ) return 2;
  if (step === "executing" || step === "settling") return 3;
  if (step === "done") return 4;
  return 0;
}

export default function HirePage() {
  const router = useRouter();
  const [mounted, setMounted] = useState(false);
  const [gateChecked, setGateChecked] = useState(false);

  // Wallet gate. /hire is private — without a saved wallet we send the
  // visitor back to the landing where the Connect CTA lives. The check
  // runs only once on mount; downstream useEffects pick up the wallet
  // from the same source after this passes.
  useEffect(() => {
    setMounted(true);
    const saved = getSavedWallet();
    if (!saved) {
      router.replace("/");
      return;
    }
    setGateChecked(true);
  }, [router]);

  const [step, setStep] = useState<Step>("input");
  const [task, setTask] = useState("");
  const [selectedCaps, setSelectedCaps] = useState<Set<string>>(new Set());
  // Budget is hidden from the main form by design: the actual price varies
  // per agent and is shown on the payment screen. We keep this as a high
  // ceiling so the discover filter never trims real agents accidentally.
  const [budget, setBudget] = useState("1.0");
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [walletKey, setWalletKey] = useState<string | null>(null);

  // Welcome banner state — sessionStorage scoped so it shows once after the
  // wallet connects and stays dismissed for the rest of the session.
  const [showWelcome, setShowWelcome] = useState<boolean>(false);
  useEffect(() => {
    if (typeof window === "undefined") return;
    const dismissed = window.sessionStorage.getItem("welcome_dismissed");
    if (!dismissed && gateChecked) setShowWelcome(true);
  }, [gateChecked]);

  const dismissWelcome = useCallback(() => {
    if (typeof window !== "undefined") {
      window.sessionStorage.setItem("welcome_dismissed", "1");
    }
    setShowWelcome(false);
  }, []);

  // Auto-dismiss after 8 seconds. Long enough to read, short enough to not
  // linger forever. The timer is canceled if the user dismisses manually or
  // navigates away, so we never double-update sessionStorage.
  useEffect(() => {
    if (!showWelcome) return;
    const t = window.setTimeout(dismissWelcome, 8000);
    return () => window.clearTimeout(t);
  }, [showWelcome, dismissWelcome]);
  const [agents, setAgents] = useState<Agent[]>([]);
  const [selectedAgent, setSelectedAgent] = useState<Agent | null>(null);
  const [paymentInfo, setPaymentInfo] = useState<PaymentInfo | null>(null);
  const [unsignedTx, setUnsignedTx] = useState<UnsignedPayment | null>(null);
  const [taskId, setTaskId] = useState<string | null>(null);
  const [events, setEvents] = useState<ExecutionEvent[]>([]);
  const [result, setResult] = useState<Record<string, unknown> | null>(null);
  const [error, setError] = useState<string | null>(null);
  const eventsRef = useRef<HTMLDivElement>(null);

  const addEvent = useCallback((evt: ExecutionEvent) => {
    setEvents((prev) => [...prev, evt]);
    setTimeout(() => eventsRef.current?.scrollTo(0, eventsRef.current.scrollHeight), 50);
  }, []);

  const caps = Array.from(selectedCaps);

  // Connect wallet (used internally by payment flows)
  const handleConnect = async () => {
    try {
      const { publicKey } = await connectWallet();
      setWalletKey(publicKey);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  // Compute the effective capability list. The user can override via the
  // Advanced panel; if they have not, we use the auto-detected one; if no
  // detection matched we send all four so the registry returns its best
  // overall matches.
  const detectedCap = detectCapability(task);
  const effectiveCaps: Capability[] = selectedCaps.size > 0
    ? Array.from(selectedCaps) as Capability[]
    : detectedCap
      ? [detectedCap]
      : [...CAPABILITY_LIST];

  // Discover agents
  const handleDiscover = async () => {
    setStep("discovering");
    setError(null);
    try {
      const res = await discoverAgents(task, effectiveCaps, parseFloat(budget));
      // Filter out debate/coordinator agents — those belong in the Debate tab
      const filtered = res.agents.filter((a) =>
        !a.capabilities.some((c) => ["debate", "adr", "architecture"].includes(c.toLowerCase()))
      );
      setAgents(filtered);
      setStep("agents");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      setStep("error");
    }
  };

  // Select agent → get 402 with price
  const handleHireSingle = async (agent: Agent) => {
    setSelectedAgent(agent);
    setStep("hiring");
    try {
      const res = await hireAgent(agent.handle, task, parseFloat(budget));
      if (res.status === 402 && res.paymentInfo) {
        setPaymentInfo(res.paymentInfo);
        setStep("payment");
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      setStep("error");
    }
  };

  // Team hire — same x402 flow as single agent
  // Step 1: Call hire-team → get 402 with price → show payment step
  // Build → Sign (Freighter) → Submit → Execute (all automatic)
  const handlePayAndExecute = async () => {
    if (!selectedAgent) return;

    try {
      // Connect wallet if not connected
      let key = walletKey;
      if (!key) {
        const wallet = await connectWallet();
        key = wallet.publicKey;
        setWalletKey(key);
      }

      // Confirm Freighter is on the same network as the app before doing
      // anything else. If not, surface a dedicated UI step so the user does
      // not lose their form state when switching networks in the extension.
      try {
        await assertWalletOnExpectedNetwork();
      } catch (netErr) {
        const msg = netErr instanceof Error ? netErr.message : String(netErr);
        if (msg.toLowerCase().includes("network")) {
          setError(msg);
          setStep("network_mismatch");
          return;
        }
        throw netErr;
      }

      // Prepare wallet: check trustline + balance
      setStep("building_tx");
      const status = await prepareWallet(key);
      // wallet prepared

      // If no trustline, sign the trustline tx with Freighter and submit via Horizon
      if (!status.has_trustline && status.trustline_xdr) {
        setStep("signing");
        const signedTrustline = await signTransaction(status.trustline_xdr);
        // Submit to Horizon directly
        const formData = new URLSearchParams({ tx: signedTrustline });
        const horizonRes = await fetch(`${HORIZON_URL}/transactions`, {
          method: "POST",
          body: formData,
        });
        if (!horizonRes.ok) {
          const err = await horizonRes.text();
          throw new Error(`Trustline tx failed: ${err.slice(0, 200)}`);
        }
        await new Promise((r) => setTimeout(r, 3000));
        // Re-prepare to get minted USDC
        await prepareWallet(key); // re-prepare after trustline
      }

      // Build payment
      setStep("building_tx");
      const amount = paymentInfo?.budget_ceiling || paymentInfo?.price || "0.10";
      const tx = await buildPayment(selectedAgent.handle, amount, key);
      setUnsignedTx(tx);

      // Sign via Freighter
      setStep("signing");
      const signedXdr = await signTransaction(tx.unsigned_xdr);

      // Submit to Stellar
      setStep("submitting");
      const hire = await submitPayment(signedXdr, selectedAgent.handle, task, parseFloat(budget), tx.task_id);
      setTaskId(hire.task_id);

      // Execute
      setStep("executing");
      setEvents([]);
      let walletAuth: string | undefined;
      try {
        walletAuth = await getWalletAuthHeader();
      } catch {
        addEvent({ type: "log", timestamp: new Date().toLocaleTimeString(), message: "Wallet auth skipped" });
      }

      streamExecution(
        hire.task_id,
        addEvent,
        async () => {
          setStep("settling");
          try {
            const res = await getResult(hire.task_id, walletAuth);
            setResult(res);
            setStep("done");
          } catch {
            setStep("done");
          }
        },
        walletAuth,
      );
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      if (msg.includes("cancel") || msg.includes("reject") || msg.includes("User")) {
        setError("Wallet signature cancelled. No payment was made.");
      } else if (
        msg.includes("Contract, #10") ||
        msg.includes("balance is not within the allowed range") ||
        msg.includes("Build payment failed: 500") && msg.includes("transfer")
      ) {
        setError(
          "Your wallet does not have enough USDC on Stellar testnet. Reload the page so we can mint test USDC for you, then try again."
        );
      } else {
        setError(msg);
      }
      setStep("error");
    }
  };

  const reset = () => {
    setStep("input");
    setAgents([]);
    setSelectedAgent(null);
    setPaymentInfo(null);
    setUnsignedTx(null);
    setTaskId(null);
    setEvents([]);
    setResult(null);
    setError(null);
  };

  // Until the gate has run, show a neutral placeholder. Rendering the form
  // before the gate check would either flash hire content to an
  // unauthenticated visitor (then redirect) or worse, run side-effects on
  // mount with no wallet. Better to wait.
  if (!mounted || !gateChecked) {
    return (
      <div className="max-w-3xl mx-auto py-8 text-center" style={{ color: "var(--dim)" }}>
        Loading…
      </div>
    );
  }

  return (
    <div style={{ maxWidth: step === "done" ? 1100 : 720, margin: "0 auto", padding: "0 24px" }}>
      {showWelcome && (
        <div
          data-testid="welcome-banner"
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            gap: 12,
            marginTop: 24,
            padding: "12px 16px",
            background: "rgba(34, 197, 94, 0.08)",
            border: "1px solid rgba(34, 197, 94, 0.25)",
            borderRadius: "var(--radius-md)",
            color: "var(--green)",
            fontSize: 13,
          }}
        >
          <span style={{ display: "flex", alignItems: "center", gap: 10 }}>
            <span
              style={{
                width: 8,
                height: 8,
                borderRadius: "50%",
                background: "var(--green)",
              }}
            />
            Welcome back. Your wallet is connected — you can hire anyone here without a second sign-in.
          </span>
          <button
            type="button"
            aria-label="Dismiss welcome"
            onClick={dismissWelcome}
            style={{
              background: "transparent",
              border: "none",
              color: "var(--green)",
              cursor: "pointer",
              fontSize: 16,
              lineHeight: 1,
              padding: 4,
            }}
          >
            ×
          </button>
        </div>
      )}

      {/* Hero — emotive opening. Shown for every step so the user always
          has a sense of place. The stepper underneath shows where they are. */}
      <section style={{ paddingTop: 56, paddingBottom: 24 }}>
        <div
          className="hero-anim"
          style={{
            fontSize: 11,
            fontFamily: "var(--mono)",
            fontWeight: 500,
            textTransform: "uppercase",
            letterSpacing: 1.2,
            color: "var(--accent)",
            marginBottom: 16,
          }}
        >
          ✱ pay only when the work ships
        </div>
        <h1
          className="hero-anim hero-anim-delay-1"
          style={{
            fontSize: 52,
            fontWeight: 700,
            letterSpacing: -2,
            color: "var(--text)",
            margin: 0,
            lineHeight: 1.05,
          }}
        >
          What do you
        </h1>
        <h1
          className="hero-anim hero-anim-delay-2 text-gradient"
          style={{
            fontSize: 52,
            fontWeight: 700,
            letterSpacing: -2,
            margin: 0,
            marginBottom: 18,
            lineHeight: 1.05,
            width: "fit-content",
          }}
        >
          need done?
        </h1>
        <p
          className="hero-anim hero-anim-delay-3"
          style={{
            fontSize: 16,
            color: "var(--muted)",
            maxWidth: 520,
            lineHeight: 1.6,
            marginBottom: 32,
          }}
        >
          An AI agent will pick it up. You see the result first, then decide if
          you pay.
        </p>

        {/* Stepper — visible on desktop only. Mobile prioritises content. */}
        <div
          className="hidden md:flex"
          aria-label="Progress"
          style={{
            alignItems: "center",
            gap: 8,
            marginBottom: 0,
            paddingTop: 8,
            borderTop: "1px solid var(--border)",
            paddingBottom: 4,
          }}
        >
          {STEPPER_STEPS.map((label, i) => {
            const current = stepperIndex(step);
            const isPast = i < current;
            const isCurrent = i === current;
            return (
              <div key={label} style={{ display: "flex", alignItems: "center", gap: 8 }}>
                <span
                  style={{
                    width: 6,
                    height: 6,
                    borderRadius: "50%",
                    background: isCurrent
                      ? "var(--accent)"
                      : isPast
                        ? "var(--green)"
                        : "var(--border2)",
                  }}
                />
                <span
                  style={{
                    fontSize: 11,
                    fontFamily: "var(--mono)",
                    color: isCurrent
                      ? "var(--text)"
                      : isPast
                        ? "var(--muted)"
                        : "var(--dim)",
                    fontWeight: isCurrent ? 600 : 400,
                  }}
                >
                  {label}
                </span>
                {i < STEPPER_STEPS.length - 1 && (
                  <span style={{ width: 24, height: 1, background: "var(--border)", marginLeft: 4 }} />
                )}
              </div>
            );
          })}
        </div>
      </section>

      {/* Input form */}
      {step === "input" && (
        <section style={{ paddingBottom: 64 }}>
          <textarea
            value={task}
            onChange={(e) => setTask(e.target.value)}
            placeholder="Type a task in your own words. Be specific about the format you want back."
            rows={5}
            aria-label="Task description"
            style={{
              width: "100%",
              minHeight: 140,
              border: "1px solid var(--border)",
              background: "var(--surface)",
              color: "var(--text)",
              fontSize: 15,
              lineHeight: 1.6,
              padding: 16,
              borderRadius: "var(--radius-md)",
              outline: "none",
              resize: "vertical",
              fontFamily: "var(--sans)",
            }}
          />

          {/* Example chips. Click fills the textarea. */}
          <div style={{ marginTop: 16 }}>
            <div
              style={{
                fontSize: 12,
                color: "var(--dim)",
                marginBottom: 10,
                fontFamily: "var(--mono)",
              }}
            >
              or click to start:
            </div>
            <div style={{ display: "flex", flexWrap: "wrap", gap: 8 }}>
              {EXAMPLE_PROMPTS.map((p) => (
                <button
                  key={p}
                  type="button"
                  onClick={() => setTask(p)}
                  style={{
                    fontSize: 12,
                    padding: "8px 14px",
                    borderRadius: "var(--radius-pill)",
                    border: "1px solid var(--border2)",
                    background: "var(--surface)",
                    color: "var(--muted)",
                    cursor: "pointer",
                    textAlign: "left",
                    transition: "color 0.15s, border-color 0.15s",
                  }}
                  onMouseEnter={(e) => {
                    e.currentTarget.style.color = "var(--text)";
                    e.currentTarget.style.borderColor = "var(--dim)";
                  }}
                  onMouseLeave={(e) => {
                    e.currentTarget.style.color = "var(--muted)";
                    e.currentTarget.style.borderColor = "var(--border2)";
                  }}
                >
                  {p}
                </button>
              ))}
            </div>
          </div>

          {/* Auto-detected capability chip. Only appears when something matched
              AND the user has not overridden via Advanced. */}
          {detectedCap && selectedCaps.size === 0 && (
            <div
              role="status"
              style={{
                marginTop: 20,
                display: "inline-flex",
                alignItems: "center",
                gap: 8,
                padding: "8px 14px",
                background: "rgba(59, 130, 246, 0.08)",
                border: "1px solid rgba(59, 130, 246, 0.25)",
                borderRadius: "var(--radius-pill)",
                fontSize: 12,
                fontFamily: "var(--mono)",
                color: "var(--accent)",
              }}
            >
              <span aria-hidden="true">🪄</span>
              Detected: {detectedCap} task
            </div>
          )}

          {/* Primary CTA + Advanced disclosure */}
          <div style={{ display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap", marginTop: 28 }}>
            <button
              onClick={handleDiscover}
              disabled={task.trim().length < 10}
              className="btn-primary"
              style={{ fontSize: 14, padding: "12px 28px" }}
            >
              Find an agent →
            </button>
            <button
              type="button"
              onClick={() => setShowAdvanced((s) => !s)}
              aria-expanded={showAdvanced}
              style={{
                fontSize: 13,
                background: "transparent",
                border: "none",
                color: "var(--muted)",
                cursor: "pointer",
                padding: "12px 8px",
              }}
            >
              {showAdvanced ? "Hide" : "Advanced options"} {showAdvanced ? "▲" : "▼"}
            </button>
          </div>

          {/* Advanced panel — only visible if the user opens it. Houses the
              capability override (the auto-detect handles the default case)
              and the budget cap (default 1.0 USDC is plenty for the demo
              pricing of 0.10–0.25 USDC). */}
          {showAdvanced && (
            <div
              style={{
                marginTop: 16,
                padding: 20,
                border: "1px solid var(--border)",
                borderRadius: "var(--radius-md)",
                background: "var(--surface)",
              }}
            >
              <div style={{ marginBottom: 16 }}>
                <div className="field-label" style={{ marginBottom: 10 }}>Filter by skill</div>
                <div style={{ display: "flex", flexWrap: "wrap", gap: 8 }}>
                  {CAPABILITY_LIST.map((cap) => {
                    const active = selectedCaps.has(cap);
                    return (
                      <button
                        key={cap}
                        onClick={() => setSelectedCaps((prev) => {
                          const next = new Set(prev);
                          next.has(cap) ? next.delete(cap) : next.add(cap);
                          return next;
                        })}
                        style={{
                          fontSize: 12,
                          padding: "6px 14px",
                          borderRadius: "var(--radius-pill)",
                          border: `1px solid ${active ? "var(--accent)" : "var(--border2)"}`,
                          background: active ? "var(--accent)" : "transparent",
                          color: active ? "white" : "var(--muted)",
                          cursor: "pointer",
                          transition: "all 0.15s",
                        }}
                      >
                        {cap}
                      </button>
                    );
                  })}
                </div>
                <p style={{ fontSize: 11, color: "var(--dim)", marginTop: 8 }}>
                  Leave empty to let the system pick based on the task text.
                </p>
              </div>

              <div>
                <label htmlFor="budget" className="field-label">Max budget (USDC)</label>
                <input
                  id="budget"
                  type="number"
                  value={budget}
                  step="0.01"
                  min="0.01"
                  onChange={(e) => setBudget(e.target.value)}
                  style={{
                    width: 120,
                    marginTop: 6,
                    padding: "8px 12px",
                    borderRadius: "var(--radius-sm)",
                    border: "1px solid var(--border2)",
                    background: "var(--bg)",
                    color: "var(--text)",
                    fontSize: 14,
                    fontFamily: "var(--mono)",
                    outline: "none",
                  }}
                />
                <p style={{ fontSize: 11, color: "var(--dim)", marginTop: 6 }}>
                  Hard ceiling on what you can spend. Unused budget refunds automatically.
                </p>
              </div>
            </div>
          )}
        </section>
      )}

      {/* Discovering */}
      {step === "discovering" && (
        <div className="text-sm py-8 text-center" style={{ color: "var(--dim)" }}>Querying on-chain registry...</div>
      )}

      {/* Agent list — rich cards. The first card is highlighted as "Best
          match" so users have a clear default but can still pick anyone. */}
      {step === "agents" && (
        <section>
          <h2
            style={{
              fontSize: 22,
              fontWeight: 700,
              letterSpacing: -0.5,
              color: "var(--text)",
              marginBottom: 6,
            }}
          >
            Pick someone for the job
          </h2>
          <p style={{ fontSize: 13, color: "var(--muted)", marginBottom: 24 }}>
            {agents.length === 0
              ? "Nobody available for this task right now."
              : `${agents.length} agent${agents.length === 1 ? "" : "s"} ready to take it.`}
          </p>

          {agents.length === 0 ? (
            <div style={{ textAlign: "center", padding: 48, color: "var(--dim)" }}>
              <button onClick={reset} style={{
                background: "transparent", border: "none", color: "var(--accent)",
                cursor: "pointer", fontSize: 14, textDecoration: "underline",
              }}>
                Edit task and try again
              </button>
            </div>
          ) : (
            <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
              {agents.map((agent, i) => {
                const isBestMatch = i === 0;
                return (
                  <button
                    key={agent.handle}
                    onClick={() => handleHireSingle(agent)}
                    style={{
                      position: "relative",
                      width: "100%",
                      textAlign: "left",
                      border: `1px solid ${isBestMatch ? "rgba(59, 130, 246, 0.4)" : "var(--border)"}`,
                      borderRadius: "var(--radius-md)",
                      background: isBestMatch ? "rgba(59, 130, 246, 0.04)" : "var(--surface)",
                      padding: 20,
                      cursor: "pointer",
                      transition: "border-color 0.15s, transform 0.15s",
                    }}
                    onMouseEnter={(e) => {
                      e.currentTarget.style.transform = "translateY(-1px)";
                      e.currentTarget.style.borderColor = "var(--accent)";
                    }}
                    onMouseLeave={(e) => {
                      e.currentTarget.style.transform = "translateY(0)";
                      e.currentTarget.style.borderColor = isBestMatch
                        ? "rgba(59, 130, 246, 0.4)"
                        : "var(--border)";
                    }}
                  >
                    {isBestMatch && (
                      <span
                        style={{
                          position: "absolute",
                          top: -10,
                          left: 16,
                          fontSize: 10,
                          fontFamily: "var(--mono)",
                          textTransform: "uppercase",
                          letterSpacing: 1,
                          background: "var(--accent)",
                          color: "white",
                          padding: "3px 10px",
                          borderRadius: "var(--radius-pill)",
                        }}
                      >
                        ⭐ Best match
                      </span>
                    )}

                    <div style={{ display: "flex", justifyContent: "space-between", alignItems: "baseline", marginBottom: 12 }}>
                      <div>
                        <div
                          style={{
                            fontSize: 16,
                            fontWeight: 600,
                            color: "var(--text)",
                            fontFamily: "var(--mono)",
                            marginBottom: 4,
                          }}
                        >
                          {agent.handle}
                        </div>
                        <div style={{ fontSize: 11, color: "var(--dim)", fontFamily: "var(--mono)" }}>
                          {agent.tier} · REP {agent.rep_score}
                          {agent.hebbian_confidence != null && (
                            <> · {(agent.hebbian_confidence * 100).toFixed(0)}% match</>
                          )}
                        </div>
                      </div>
                      <div
                        style={{
                          fontSize: 20,
                          fontWeight: 600,
                          fontFamily: "var(--mono)",
                          color: "var(--text)",
                        }}
                      >
                        {agent.rate_xlm} <span style={{ fontSize: 12, color: "var(--muted)" }}>USDC</span>
                      </div>
                    </div>

                    <div style={{ display: "flex", flexWrap: "wrap", gap: 6, marginBottom: 16 }}>
                      {agent.capabilities.map((c) => (
                        <span
                          key={c}
                          style={{
                            fontSize: 10,
                            padding: "3px 10px",
                            borderRadius: "var(--radius-pill)",
                            background: "var(--surface2)",
                            color: "var(--muted)",
                            fontFamily: "var(--mono)",
                          }}
                        >
                          {c}
                        </span>
                      ))}
                    </div>

                    <div
                      style={{
                        fontSize: 12,
                        color: "var(--accent)",
                        display: "flex",
                        alignItems: "center",
                        gap: 4,
                      }}
                    >
                      Hire {agent.handle} →
                    </div>
                  </button>
                );
              })}

              <button
                onClick={reset}
                type="button"
                style={{
                  background: "transparent",
                  border: "none",
                  color: "var(--muted)",
                  fontSize: 12,
                  cursor: "pointer",
                  padding: "8px 0",
                  textAlign: "center",
                  marginTop: 4,
                }}
              >
                ◀ Back to task
              </button>
            </div>
          )}
        </section>
      )}

      {/* Hiring */}
      {step === "hiring" && (
        <div className="text-sm py-8 text-center" style={{ color: "var(--dim)" }}>
          {`Hiring ${selectedAgent?.handle}...`}
        </div>
      )}

      {/* Payment info — agent handle is the headline; the price is the
          biggest number on the screen; cancel is a link, not a button. */}
      {step === "payment" && (
        <section style={{
          border: "1px solid var(--border)",
          borderRadius: "var(--radius-md)",
          padding: 28,
          background: "var(--surface)",
        }}>
          {selectedAgent && (
            <div style={{ fontSize: 12, fontFamily: "var(--mono)", color: "var(--dim)", marginBottom: 6 }}>
              Paying {selectedAgent.handle}
            </div>
          )}
          <h2 style={{
            fontSize: 20,
            fontWeight: 600,
            color: "var(--text)",
            marginBottom: 24,
          }}>
            Confirm payment
          </h2>

          {paymentInfo && (
            <div style={{ marginBottom: 24 }}>
              {paymentInfo.estimated_cost ? (
                <>
                  <div style={{ display: "flex", alignItems: "baseline", justifyContent: "space-between", marginBottom: 6 }}>
                    <span style={{ fontSize: 13, color: "var(--muted)" }}>You'll pay up to</span>
                    <span style={{ fontSize: 28, fontWeight: 700, fontFamily: "var(--mono)" }}>
                      {paymentInfo.budget_ceiling} <span style={{ fontSize: 14, color: "var(--muted)" }}>USDC</span>
                    </span>
                  </div>
                  <div style={{ display: "flex", justifyContent: "space-between", fontSize: 12, color: "var(--dim)" }}>
                    <span>Estimated actual cost</span>
                    <span style={{ fontFamily: "var(--mono)" }}>{paymentInfo.estimated_cost} USDC</span>
                  </div>
                  {paymentInfo.rate_breakdown && paymentInfo.rate_breakdown.length > 0 && (
                    <details style={{ marginTop: 14 }}>
                      <summary style={{ fontSize: 11, color: "var(--dim)", cursor: "pointer" }}>
                        Show rate breakdown
                      </summary>
                      <div style={{ fontSize: 11, color: "var(--dim)", marginTop: 8, paddingLeft: 12 }}>
                        {paymentInfo.rate_breakdown.map((r, i) => (
                          <div key={i}>{r.tool}: {r.rate} USDC/call</div>
                        ))}
                      </div>
                    </details>
                  )}
                </>
              ) : (
                <div style={{ display: "flex", alignItems: "baseline", justifyContent: "space-between" }}>
                  <span style={{ fontSize: 13, color: "var(--muted)" }}>Price</span>
                  <span style={{ fontSize: 28, fontWeight: 700, fontFamily: "var(--mono)" }}>
                    {paymentInfo.price} <span style={{ fontSize: 14, color: "var(--muted)" }}>USDC</span>
                  </span>
                </div>
              )}
              <div style={{ fontSize: 11, fontFamily: "var(--mono)", color: "var(--dim)", marginTop: 14 }}>
                Network {paymentInfo.network} · Escrow {paymentInfo.payTo.slice(0, 8)}…{paymentInfo.payTo.slice(-4)}
              </div>
            </div>
          )}

          <div style={{
            background: "rgba(59,130,246,0.08)",
            border: "1px solid rgba(59,130,246,0.2)",
            borderRadius: "var(--radius-sm)",
            padding: 12,
            fontSize: 12,
            color: "var(--accent)",
            marginBottom: 24,
            lineHeight: 1.5,
          }}>
            USDC sits in escrow. The agent gets paid only after the work is verified. Unused budget refunds back to you.
          </div>

          <div style={{ display: "flex", alignItems: "center", gap: 16 }}>
            <button
              onClick={handlePayAndExecute}
              className="btn-primary"
              style={{ fontSize: 14, padding: "12px 28px" }}
            >
              {walletKey ? "Pay and execute →" : "Connect wallet and pay →"}
            </button>
            <button
              onClick={reset}
              type="button"
              style={{
                background: "transparent",
                border: "none",
                color: "var(--muted)",
                fontSize: 13,
                cursor: "pointer",
                textDecoration: "underline",
                padding: 0,
              }}
            >
              Cancel
            </button>
          </div>
        </section>
      )}

      {/* Network mismatch — keeps form state, just prompts user to switch */}
      {step === "network_mismatch" && (
        <div
          className="border rounded-xl p-6"
          style={{ borderColor: "var(--amber, #f59e0b)" }}
        >
          <h2 className="text-sm font-medium mb-3" style={{ color: "var(--amber, #f59e0b)" }}>
            Wrong network selected in your wallet
          </h2>
          <p className="text-sm mb-4" style={{ color: "var(--muted)" }}>
            Brújula runs on Stellar {EXPECTED_NETWORK}. Your wallet is on the
            other network, so it will not let you sign this payment.
          </p>

          <ol
            className="text-sm mb-5"
            style={{ color: "var(--text)", paddingLeft: 20, lineHeight: 1.8 }}
          >
            <li>Open the Freighter extension in your browser.</li>
            <li>Click the network dropdown at the top of the popup.</li>
            <li>
              Choose <strong>{EXPECTED_NETWORK}</strong>.
            </li>
            <li>Come back here and click the button below.</li>
          </ol>

          <div className="flex gap-3">
            <button
              onClick={reset}
              className="px-4 py-2 border rounded-lg text-sm"
              style={{ borderColor: "var(--border)", color: "var(--muted)" }}
            >
              Cancel
            </button>
            <button
              onClick={() => {
                setError(null);
                setStep("payment");
                handlePayAndExecute();
              }}
              className="px-6 py-2 rounded-lg text-sm font-medium"
              style={{ background: "var(--text)", color: "var(--bg)" }}
            >
              I switched. Try again.
            </button>
          </div>
        </div>
      )}

      {/* Progress states */}
      {step === "building_tx" && (
        <div className="text-sm py-8 text-center" style={{ color: "var(--dim)" }}>Building Soroban transaction...</div>
      )}
      {step === "signing" && (
        <div className="text-sm py-8 text-center" style={{ color: "var(--dim)" }}>
          Check your wallet. Approve the transaction in Freighter.
          {unsignedTx && (
            <div className="mt-2 text-xs">
              {unsignedTx.amount_usdc} USDC → {unsignedTx.destination_label || "Escrow"}
            </div>
          )}
        </div>
      )}
      {step === "submitting" && (
        <div className="text-sm py-8 text-center" style={{ color: "var(--dim)" }}>Submitting to Stellar...</div>
      )}

      {/* Execution stream — each backend event is translated for end users.
          We keep timestamps because users like seeing things move, but the
          jargon (A2A, hashes, "Execution complete") is rewritten. */}
      {(step === "executing" || step === "settling") && (
        <section>
          <div
            style={{
              fontSize: 14,
              color: "var(--text)",
              fontWeight: 500,
              marginBottom: 4,
              display: "flex",
              alignItems: "center",
              gap: 10,
            }}
          >
            <span
              style={{
                width: 8,
                height: 8,
                borderRadius: "50%",
                background: "var(--accent)",
                animation: "pulse-dot 1.4s ease-in-out infinite",
              }}
            />
            {step === "executing" ? "The agent is working" : "Finishing up"}
          </div>
          <p style={{ fontSize: 12, color: "var(--muted)", marginBottom: 14 }}>
            {step === "executing"
              ? "It usually takes a few seconds."
              : "Releasing the payment now."}
          </p>
          <div
            ref={eventsRef}
            style={{
              borderRadius: "var(--radius-md)",
              border: "1px solid var(--border)",
              background: "var(--surface)",
              padding: 16,
              maxHeight: 320,
              overflowY: "auto",
            }}
          >
            {events.map((evt, i) => {
              const friendly = humaniseEventMessage(evt.message, evt.type);
              const color = evt.type === "error" ? "#f87171"
                : evt.type === "complete" ? "var(--green)"
                : evt.type === "tool_call" ? "var(--accent)"
                : "var(--text)";
              return (
                <div
                  key={i}
                  style={{
                    display: "flex",
                    alignItems: "baseline",
                    gap: 12,
                    padding: "6px 0",
                    fontSize: 13,
                    color,
                    fontFamily: "var(--sans)",
                  }}
                >
                  <span style={{
                    fontSize: 10,
                    fontFamily: "var(--mono)",
                    color: "var(--dim)",
                    flexShrink: 0,
                    minWidth: 56,
                  }}>
                    {evt.timestamp}
                  </span>
                  <span>{friendly}</span>
                </div>
              );
            })}
          </div>
        </section>
      )}

      {/* Done */}
      {step === "done" && (
        <div className="space-y-4">
          <div className="font-medium" style={{ color: "var(--green)" }}>Task complete</div>

          {result && (
            <>
              {/* Memory hint — show when the agent reused prior research. The
                  raw result includes prior_knowledge_used > 0 and/or a
                  finding of type "prior_knowledge". We surface the benefit
                  in plain language: faster + cheaper because the agent
                  recognised the question from a past run. */}
              {(() => {
                const raw = (result as any).output;
                let priorUsed = 0;
                let findingsCount = 0;
                if (typeof raw === "string") {
                  try {
                    const parsed = JSON.parse(raw);
                    priorUsed = parsed?.prior_knowledge_used ?? 0;
                    findingsCount = Array.isArray(parsed?.findings)
                      ? parsed.findings.filter(
                          (f: any) => f?.type === "prior_knowledge",
                        ).length
                      : 0;
                  } catch {
                    /* not JSON, no hint */
                  }
                }
                const usedMemory = priorUsed > 0 || findingsCount > 0;
                if (!usedMemory) return null;
                return (
                  <div
                    role="note"
                    style={{
                      display: "flex",
                      alignItems: "flex-start",
                      gap: 12,
                      padding: "14px 16px",
                      borderRadius: "var(--radius-md)",
                      background: "rgba(59,130,246,0.06)",
                      border: "1px solid rgba(59,130,246,0.22)",
                    }}
                  >
                    <span aria-hidden="true" style={{ fontSize: 22, lineHeight: 1 }}>💭</span>
                    <div>
                      <div style={{ fontSize: 13, fontWeight: 600, color: "var(--text)", marginBottom: 2 }}>
                        The agent recognised this from a past run
                      </div>
                      <div style={{ fontSize: 12, color: "var(--muted)", lineHeight: 1.5 }}>
                        Some of this answer came from memory built up by your previous
                        searches. That part was instant and didn't cost extra. The more
                        you use Brújula, the smarter and cheaper each task gets.
                      </div>
                    </div>
                  </div>
                );
              })()}

              {/* Output — auto-detects debate vs regular. We pre-process the
                  markdown to strip the "Hebbian Knowledge Graph" label the
                  LLM occasionally produces. */}
              {(result as any).output && (
                <DebateResultView output={humaniseResultMarkdown(String((result as any).output))} />
              )}

              {/* Team results — per subtask */}
              {(result as any).results && typeof (result as any).results === "object" && (
                <div className="space-y-3">
                  {Object.entries((result as any).results as Record<string, string>).map(([subtaskId, output]) => {
                    const st = ((result as any).sub_tasks || []).find((s: any) => s.id === subtaskId);
                    return (
                      <div key={subtaskId} className="border rounded-xl p-6 overflow-hidden" style={{ background: "var(--surface)", borderColor: "var(--border)" }}>
                        <div className="text-xs mb-3 font-mono" style={{ color: "var(--dim)" }}>
                          [{st?.agent_handle || subtaskId}] {st?.action?.slice(0, 80)}
                        </div>
                        <div className="prose prose-sm max-w-none prose-headings:font-semibold prose-h2:text-base prose-h2:mt-6 prose-h2:mb-2 prose-h3:text-sm prose-h3:mt-4 prose-h3:mb-1 prose-p:text-sm prose-p:leading-relaxed prose-ul:my-2 prose-ol:my-2 break-words" style={{ color: "var(--text)" }}>
                          <Markdown>{String(output)}</Markdown>
                        </div>
                      </div>
                    );
                  })}
                </div>
              )}

              {(result as any).settlement && (
                <div className="border rounded-xl p-4" style={{ background: "var(--surface)", borderColor: "var(--border)" }}>
                  <div className="text-xs mb-2" style={{ color: "var(--dim)" }}>Settlement</div>
                  <div className="text-sm space-y-1">
                    <div className="flex justify-between">
                      <span style={{ color: "var(--muted)" }}>Actual cost</span>
                      <span className="font-mono">
                        {(result as any).settlement.actual_cost_usdc || (result as any).settlement.total_cost_usdc} USDC
                      </span>
                    </div>
                    {(result as any).settlement.settle_tx && (
                      <div className="text-xs font-mono" style={{ color: "var(--dim)" }}>
                        {/* settle_tx can be a string, an object with .hash, or missing.
                            Coerce defensively so a stringly-typed backend response
                            does not crash the result panel. */}
                        TX: {String(
                          (result as any).settlement.settle_tx?.hash ||
                          (result as any).settlement.settle_tx
                        ).slice(0, 16)}…
                      </div>
                    )}
                  </div>
                </div>
              )}

              <div className="text-xs" style={{ color: "var(--dim)" }}>
                Status: {String((result as any).settlement_status || "pending")}
                {(result as any).compliant != null && (
                  <span className="ml-2">Compliance: {(result as any).compliant ? "PASS" : "FAIL"}</span>
                )}
              </div>
            </>
          )}

          <button onClick={reset} className="px-6 py-2 rounded-lg text-sm font-medium" style={{ background: "var(--text)", color: "var(--bg)" }}>
            New Task
          </button>
        </div>
      )}

      {/* Error */}
      {step === "error" && (
        <div className="text-center py-8">
          <div className="text-sm mb-4" style={{ color: "#ef4444" }}>{error}</div>
          <button onClick={reset} className="px-4 py-2 rounded-lg text-sm" style={{ background: "var(--text)", color: "var(--bg)" }}>
            Try Again
          </button>
        </div>
      )}
    </div>
  );
}
