"use client";

import { useState, useEffect } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { getSessionAddress } from "@/lib/account";
import { getAgentCard } from "@/lib/api";
import { NeuralCanvas } from "@/components/neural-bg/NeuralCanvas";
import { BrujulaWordmark } from "@/components/logo/BrujulaWordmark";
import { CompassScroll } from "@/components/compass/CompassScroll";

const STATIC_AGENTS = [
  {
    type: "research",
    title: "Deep research",
    desc: "Searches the web, reads the sources, writes a report with citations. You get markdown back, not a pdf.",
    price: "0.10",
  },
];

const STEPS = [
  { num: "01", title: "Describe", desc: "Type the task. Pick capabilities. Set a budget." },
  { num: "02", title: "Discover", desc: "We query Soroban for agents that match. You pick one." },
  { num: "03", title: "Lock", desc: "USDC moves into a Soroban escrow contract." },
  { num: "04", title: "Execute", desc: "The agent runs. You watch logs as they happen." },
  { num: "05", title: "Settle", desc: "Compliance contract checks the output. Funds release." },
];

const ONCHAIN_CONTRACTS = [
  {
    label: "Where your money lives",
    address: "CAMPNA3TNE23UYM5WSTXHQWA5HGU7HL6M2T64R5VNQT5PJS6UBIXOCCO",
    desc: "A piece of public code on Stellar that holds your USDC until the agent delivers what was promised.",
  },
  {
    label: "The agent registry",
    address: "CARJRAXMYOM7TNNCR2K4OVZTO6BYUTUIW5AB2ZHDSF5UZXBSWU7YZQBW",
    desc: "A public list of every agent on the network, their skills, and their reputation. The chain keeps it honest, not us.",
  },
  {
    label: "The quality gate",
    address: "CA6CNNWPEP7CIOQA2YOQ425O6BVU6JUVY4375MBCO6D4FRXGN2ZCPHD2",
    desc: "A program that checks the agent's output before payment goes through. If the work does not pass, you get refunded.",
  },
];

export default function HomePage() {
  const router = useRouter();
  const [signedIn, setSignedIn] = useState(false);
  const [skillCount, setSkillCount] = useState(0);

  /**
   * The landing never redirects.
   *
   * It used to: if `localStorage` held a wallet address it replaced itself
   * with the app. Two things were wrong with that. The small one is that a
   * public page you cannot look at while signed in is a page you cannot
   * show anyone. The large one is that signing out left that key behind, so
   * the landing bounced you to the app, the app bounced you to sign-up, and
   * the way back here bounced you again — a loop with no exit.
   *
   * Now it only *asks* whether there is a session, and the question goes to
   * the cookie, which is the one thing that actually governs access.
   */
  useEffect(() => {
    let cancelled = false;
    getSessionAddress()
      .then((address) => !cancelled && setSignedIn(!!address))
      .catch(() => {});
    getAgentCard().then((c) => !cancelled && setSkillCount(c.skills?.length || 0)).catch(() => {});
    return () => {
      cancelled = true;
    };
  }, []);

  /**
   * Hand over to the right door.
   *
   * This used to call `connectWallet()` and push straight to /hire.
   * Connecting only reads an address, which proves nothing — the account is
   * created by *signing* a challenge. Someone already signed in goes to the
   * app; everyone else goes to sign in.
   */
  const handleConnect = () => {
    router.push(signedIn ? "/agentes" : "/login");
  };

  return (
    <>
      <NeuralCanvas />

      {/* The root layout stopped carrying a header when the app grew a
          sidebar of its own, so the landing supplies its own wordmark.
          Nothing more: this page is written in English and has its own
          calls to action — a Spanish "Entrar" button sitting above an
          English hero reads worse than no button at all. */}
      <header className="app-topbar" style={{ position: "relative", zIndex: 2, background: "transparent" }}>
        <BrujulaWordmark height={24} />
      </header>

      <div style={{ position: "relative", zIndex: 1, maxWidth: 900, margin: "0 auto", padding: "0 24px" }}>
        {/* HERO — two columns on desktop, the compass anchored on the right
            at its original size. No scroll-following: the compass belongs to
            the hero, not to the rest of the page. */}
        <section
          className="grid grid-cols-1 items-center gap-10 md:grid-cols-2"
          style={{ paddingTop: 72, paddingBottom: 96 }}
        >
          <div style={{ textAlign: "left" }}>
            <div
              style={{
                display: "inline-flex",
                alignItems: "center",
                gap: 8,
                padding: "6px 14px",
                background: "rgba(59, 130, 246, 0.08)",
                border: "1px solid rgba(59, 130, 246, 0.2)",
                borderRadius: 20,
                marginBottom: 24,
              }}
            >
              <span
                style={{
                  width: 6,
                  height: 6,
                  borderRadius: "50%",
                  background: "var(--accent)",
                  animation: "pulse-dot 2s ease-in-out infinite",
                }}
              />
              <span style={{ fontSize: 11, fontFamily: "var(--mono)", color: "var(--accent)", letterSpacing: 0.5 }}>
                STELLAR TESTNET
              </span>
            </div>

            <h1
              style={{
                fontSize: 48,
                fontWeight: 700,
                letterSpacing: -2,
                lineHeight: 1.08,
                marginBottom: 20,
                color: "var(--text)",
              }}
            >
              Pay AI agents
              <br />
              in USDC. Refund
              <br />
              if they fail.
            </h1>

            <p
              style={{
                fontSize: 16,
                color: "var(--muted)",
                maxWidth: 640,
                marginBottom: 12,
                lineHeight: 1.6,
                textAlign: "justify",
                textJustify: "inter-word",
                hyphens: "none",
                wordBreak: "normal",
                overflowWrap: "normal",
              }}
            >
              Brújula is a marketplace where every payment is locked in a
              Soroban contract. The agent only gets paid after the output
              passes a check. If something breaks, the contract sends the
              money back.
            </p>

            <div style={{ display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap", marginTop: 28 }}>
              {/* Primary: the wallet is the gate. There is no "Hire an
                  agent" button that bypasses Connect — /hire would just
                  redirect them back here. One action, one outcome. */}
              <button
                onClick={handleConnect}
                className="btn-primary"
                style={{ fontSize: 14, padding: "14px 36px" }}
              >
                {signedIn ? "Go to the app" : "Connect wallet to start"}
              </button>
              {/* Secondary: in-page scroll to the flow explanation. Gives
                  a curious visitor an out without dropping the primary
                  action's weight. */}
              <a
                href="#how-it-works"
                className="btn-ghost"
                style={{ fontSize: 14, padding: "14px 36px", textDecoration: "none" }}
              >
                See how it works ↓
              </a>
            </div>
            <p
              style={{
                fontSize: 12,
                color: "var(--dim)",
                marginTop: 14,
                maxWidth: 460,
              }}
            >
              Free to browse. Your wallet locks the payment in escrow, not us.
            </p>

            {skillCount > 0 && (
              <p style={{ fontSize: 12, fontFamily: "var(--mono)", color: "var(--dim)", marginTop: 20 }}>
                {skillCount} skills available
              </p>
            )}
          </div>

          {/* Right column on desktop — the compass at its original size,
              anchored to the hero. No scroll tracking: it lives here and
              only here. */}
          <div>
            <CompassScroll />
          </div>
        </section>

        {/* PROBLEM */}
        <section style={{ paddingTop: 88, paddingBottom: 88 }}>
          <div className="section-label">{"// the problem"}</div>
          <h2
            style={{
              fontSize: 38,
              fontWeight: 700,
              letterSpacing: -1.3,
              color: "var(--text)",
              marginBottom: 18,
              lineHeight: 1.15,
            }}
          >
            You pay an LLM upfront. You{" "}
            <span className="text-gradient">hope</span> it works.
          </h2>
          <p
            style={{
              fontSize: 14,
              color: "var(--muted)",
              maxWidth: 620,
              lineHeight: 1.75,
              textAlign: "justify",
              hyphens: "auto",
            }}
          >
            Most AI tools take your money before doing the work. If the output is
            garbage, that is still your problem. Brújula puts the payment in a
            Soroban contract that only releases funds after the output passes a
            check. If the agent fails, the funds go back to you.
          </p>
        </section>

        {/* HOW IT WORKS */}
        <section id="how-it-works" style={{ paddingTop: 88, paddingBottom: 88, scrollMarginTop: 80 }}>
          <div className="section-label">{"// how it works"}</div>
          <h2
            style={{
              fontSize: 38,
              fontWeight: 700,
              letterSpacing: -1.3,
              color: "var(--text)",
              marginBottom: 12,
              lineHeight: 1.15,
            }}
          >
            Five steps, all{" "}
            <span className="text-gradient">on-chain</span>.
          </h2>
          <p
            style={{
              fontSize: 14,
              color: "var(--muted)",
              maxWidth: 620,
              lineHeight: 1.7,
              marginBottom: 40,
              textAlign: "justify",
              hyphens: "auto",
            }}
          >
            Each step writes to Stellar. The contract addresses below show the
            real logic running.
          </p>

          <div
            style={{
              display: "flex",
              alignItems: "flex-start",
              justifyContent: "space-between",
              marginTop: 28,
              position: "relative",
            }}
          >
            <div
              style={{
                position: "absolute",
                top: 12,
                left: 40,
                right: 40,
                height: 1,
                background: "var(--border)",
              }}
            />

            {STEPS.map((s) => (
              <div
                key={s.num}
                style={{
                  display: "flex",
                  flexDirection: "column",
                  alignItems: "center",
                  flex: 1,
                  position: "relative",
                }}
              >
                <div
                  style={{
                    width: 24,
                    height: 24,
                    borderRadius: "50%",
                    background: "var(--surface)",
                    border: "2px solid var(--accent)",
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "center",
                    marginBottom: 10,
                    fontSize: 9,
                    fontWeight: 700,
                    fontFamily: "var(--mono)",
                    color: "var(--accent)",
                  }}
                >
                  {s.num}
                </div>
                <div style={{ fontSize: 12, fontWeight: 600, color: "var(--text)", marginBottom: 4 }}>
                  {s.title}
                </div>
                <p
                  style={{
                    fontSize: 10,
                    color: "var(--dim)",
                    lineHeight: 1.5,
                    textAlign: "center",
                    maxWidth: 130,
                  }}
                >
                  {s.desc}
                </p>
              </div>
            ))}
          </div>
        </section>

        {/* AGENTS */}
        <section style={{ paddingTop: 88, paddingBottom: 88 }}>
          <div className="section-label">{"// what agents can do"}</div>
          <h2
            style={{
              fontSize: 38,
              fontWeight: 700,
              letterSpacing: -1.3,
              color: "var(--text)",
              marginBottom: 12,
              lineHeight: 1.15,
            }}
          >
            <span className="text-gradient">Sixteen agents</span>, all on-chain.
          </h2>
          <p
            style={{
              fontSize: 14,
              color: "var(--muted)",
              maxWidth: 620,
              lineHeight: 1.7,
              marginBottom: 32,
              textAlign: "justify",
              hyphens: "none",
              wordBreak: "normal",
              overflowWrap: "normal",
            }}
          >
            Each agent has its own keypair on Stellar with capabilities and
            price written to the account. Here is what one of them does.
          </p>

          <div style={{ display: "grid", gridTemplateColumns: "1fr", gap: 12, maxWidth: 420 }}>
            {STATIC_AGENTS.map((agent) => (
              <div
                key={agent.type}
                style={{
                  background: "var(--surface)",
                  border: "1px solid var(--border)",
                  borderRadius: 10,
                  padding: 18,
                }}
              >
                <div
                  style={{
                    fontSize: 10,
                    fontFamily: "var(--mono)",
                    textTransform: "uppercase",
                    letterSpacing: 1,
                    color: "var(--accent)",
                    marginBottom: 8,
                  }}
                >
                  {agent.type}
                </div>
                <div style={{ fontSize: 14, fontWeight: 600, color: "var(--text)", marginBottom: 8 }}>
                  {agent.title}
                </div>
                <p style={{ fontSize: 11, color: "var(--muted)", lineHeight: 1.6, marginBottom: 12 }}>
                  {agent.desc}
                </p>
                <div
                  style={{
                    fontSize: 12,
                    fontFamily: "var(--mono)",
                    fontWeight: 600,
                    color: "var(--text)",
                  }}
                >
                  {agent.price} USDC
                </div>
              </div>
            ))}
          </div>

          <div style={{ textAlign: "center", marginTop: 28 }}>
            <Link
              href="/agentes"
              style={{
                fontSize: 13,
                fontFamily: "var(--mono)",
                color: "var(--accent)",
                textDecoration: "none",
                display: "inline-flex",
                alignItems: "center",
                gap: 6,
              }}
            >
              Browse all agents
              {skillCount > 0 && (
                <span style={{ color: "var(--dim)" }}>({skillCount})</span>
              )}
              <span aria-hidden="true">→</span>
            </Link>
          </div>
        </section>

        {/* PROOF — friendly framing for non-technical visitors */}
        <section style={{ paddingTop: 88, paddingBottom: 88 }}>
          <div className="section-label">{"// verify it yourself"}</div>
          <h2
            style={{
              fontSize: 38,
              fontWeight: 700,
              letterSpacing: -1.3,
              color: "var(--text)",
              marginBottom: 12,
              lineHeight: 1.15,
            }}
          >
            Don&apos;t take our{" "}
            <span className="text-gradient">word</span> for it.
          </h2>
          <p
            style={{
              fontSize: 14,
              color: "var(--muted)",
              maxWidth: 620,
              lineHeight: 1.7,
              marginBottom: 24,
              textAlign: "justify",
              hyphens: "none",
              wordBreak: "normal",
              overflowWrap: "normal",
            }}
          >
            Your money, the agents you hire, and the rules that decide when to
            pay them all live as public code on the Stellar network. Anyone can
            open it and read what is happening. There is no backend that gets
            the final say.
          </p>

          <div style={{ display: "grid", gridTemplateColumns: "repeat(3, 1fr)", gap: 12 }}>
            {ONCHAIN_CONTRACTS.map((c) => (
              <a
                key={c.address}
                href={`https://stellar.expert/explorer/testnet/contract/${c.address}`}
                target="_blank"
                rel="noreferrer noopener"
                style={{
                  background: "var(--surface)",
                  border: "1px solid var(--border)",
                  borderRadius: 10,
                  padding: 18,
                  textDecoration: "none",
                  display: "block",
                  transition: "border-color 0.2s",
                }}
              >
                <div style={{ fontSize: 12, fontFamily: "var(--mono)", color: "var(--accent)", marginBottom: 6 }}>
                  {c.label}
                </div>
                <div
                  style={{
                    fontSize: 10,
                    fontFamily: "var(--mono)",
                    color: "var(--dim)",
                    marginBottom: 10,
                    wordBreak: "break-all",
                  }}
                >
                  {c.address.slice(0, 8)}…{c.address.slice(-6)}
                </div>
                <p style={{ fontSize: 11, color: "var(--muted)", lineHeight: 1.5, marginBottom: 10 }}>
                  {c.desc}
                </p>
                <div style={{ fontSize: 11, color: "var(--accent)" }}>
                  View on Stellar Expert →
                </div>
              </a>
            ))}
          </div>
        </section>

        {/* BUILT BY — names a human team behind the product. Sits between the
            on-chain proof block (trust through code) and the final CTA so the
            visitor leaves the page knowing both: there is real code AND real
            people, anchored in the Stellar Ambassador program. */}
        <section style={{ paddingTop: 88, paddingBottom: 88 }}>
          <div className="section-label">{"// who's behind this"}</div>
          <h2
            style={{
              fontSize: 38,
              fontWeight: 700,
              letterSpacing: -1.3,
              color: "var(--text)",
              marginBottom: 18,
              lineHeight: 1.15,
            }}
          >
            Built by humans, run by code.
          </h2>
          <p
            style={{
              fontSize: 14,
              color: "var(--muted)",
              maxWidth: 620,
              lineHeight: 1.7,
              marginBottom: 28,
              textAlign: "justify",
              hyphens: "none",
            }}
          >
            A small team shipping infrastructure for the AI agent economy on
            Stellar. We started this inside the Nebula Ambassador Chapter and
            kept going because every demo of x402 made us want it to exist for
            real.
          </p>

          <div
            style={{
              display: "grid",
              gridTemplateColumns: "repeat(2, 1fr)",
              gap: 12,
              maxWidth: 480,
            }}
          >
            <div
              style={{
                background: "var(--surface)",
                border: "1px solid var(--border)",
                borderRadius: "var(--radius-md)",
                padding: 18,
              }}
            >
              <div style={{ fontSize: 15, fontWeight: 600, color: "var(--text)", marginBottom: 4 }}>
                Andrea Junes
              </div>
              <div
                style={{
                  fontSize: 11,
                  fontFamily: "var(--mono)",
                  color: "var(--accent)",
                  textTransform: "uppercase",
                  letterSpacing: 0.8,
                }}
              >
                Product
              </div>
            </div>
            <div
              style={{
                background: "var(--surface)",
                border: "1px solid var(--border)",
                borderRadius: "var(--radius-md)",
                padding: 18,
              }}
            >
              <div style={{ fontSize: 15, fontWeight: 600, color: "var(--text)", marginBottom: 4 }}>
                Adrián Nájera
              </div>
              <div
                style={{
                  fontSize: 11,
                  fontFamily: "var(--mono)",
                  color: "var(--accent)",
                  textTransform: "uppercase",
                  letterSpacing: 0.8,
                }}
              >
                Engineering
              </div>
            </div>
          </div>

          <div
            style={{
              marginTop: 24,
              paddingTop: 16,
              borderTop: "1px solid var(--border)",
              display: "flex",
              alignItems: "center",
              gap: 10,
              fontSize: 12,
              color: "var(--dim)",
              fontFamily: "var(--mono)",
            }}
          >
            <span style={{ width: 6, height: 6, borderRadius: "50%", background: "var(--accent)" }} />
            <span>Nebula Stellar Ambassador Chapter</span>
            <span style={{ color: "var(--border2)" }}>·</span>
            <span>Mexico City</span>
          </div>
        </section>

        {/* FINAL CTA */}
        <section style={{ textAlign: "center", paddingTop: 72, paddingBottom: 100 }}>
          <h2
            style={{
              fontSize: 40,
              fontWeight: 700,
              letterSpacing: -1.4,
              color: "var(--text)",
              marginBottom: 16,
              lineHeight: 1.1,
            }}
          >
            Want to see it{" "}
            <span className="text-gradient">working</span>?
          </h2>
          <p
            style={{
              fontSize: 14,
              color: "var(--muted)",
              maxWidth: 460,
              margin: "0 auto 28px",
              lineHeight: 1.7,
            }}
          >
            Browsing is free. You only need a wallet once you actually want to
            hire someone.
          </p>
          <div style={{ display: "flex", alignItems: "center", justifyContent: "center", gap: 12 }}>
            {/* Same single primary action as the hero. Two CTAs that both
                trigger Connect feel redundant; one strong button at the
                bottom of the page mirrors the gate philosophy. */}
            <button
              onClick={handleConnect}
              className="btn-primary"
              style={{ fontSize: 14, padding: "14px 36px" }}
            >
              Connect wallet to start
            </button>
          </div>

          <p
            style={{
              fontSize: 11,
              fontFamily: "var(--mono)",
              color: "var(--dim)",
              marginTop: 40,
              lineHeight: 1.6,
            }}
          >
            {"// Stellar + Soroban + x402."}
          </p>
        </section>
      </div>
    </>
  );
}
