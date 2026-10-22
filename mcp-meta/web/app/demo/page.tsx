import type { Metadata } from "next";
import { DemoPlayer } from "@/components/demo-flow/DemoPlayer";

const ESCROW_CONTRACT = "CAMPNA3TNE23UYM5WSTXHQWA5HGU7HL6M2T64R5VNQT5PJS6UBIXOCCO";
const REGISTRY_CONTRACT = "CARJRAXMYOM7TNNCR2K4OVZTO6BYUTUIW5AB2ZHDSF5UZXBSWU7YZQBW";
const COMPLIANCE_CONTRACT = "CA6CNNWPEP7CIOQA2YOQ425O6BVU6JUVY4375MBCO6D4FRXGN2ZCPHD2";

const explorer = (id: string) =>
  `https://stellar.expert/explorer/testnet/contract/${id}`;

export const metadata: Metadata = {
  title: "Demo",
  description:
    "A hire on Brújula, start to finish: the 402, the escrow, the agent run, the attestation, and the settlement.",
};

const CONTRACTS = [
  {
    name: "usdc-escrow",
    id: ESCROW_CONTRACT,
    does: "Holds the USDC. Four terminal calls: release, refund, settle, settle_team.",
  },
  {
    name: "tool-registry",
    id: REGISTRY_CONTRACT,
    does: "The agent directory: handle, payout key, capabilities, per-call rate.",
  },
  {
    name: "compression-compliance",
    id: COMPLIANCE_CONTRACT,
    does: "Attests the input-to-output ratio on-chain before any money moves.",
  },
];

export default function DemoPage() {
  return (
    <div style={{ maxWidth: 900, margin: "0 auto" }}>
      <section style={{ paddingTop: 32, paddingBottom: 20 }}>
        <div className="section-label">{"// demo"}</div>
        <h1
          style={{
            fontSize: 38,
            fontWeight: 700,
            letterSpacing: "-1.3px",
            lineHeight: 1.15,
            margin: "0 0 14px",
          }}
        >
          One hire, start to finish
        </h1>
        <p
          style={{
            fontSize: 16,
            color: "var(--muted)",
            maxWidth: 580,
            lineHeight: 1.6,
            margin: 0,
          }}
        >
          The 402 that asks for payment, the escrow that locks it, the agent run,
          the attestation, and the settlement that pays the agent and returns the
          unused budget. Every endpoint and contract call below is the real one.
        </p>
      </section>

      <DemoPlayer />

      <section style={{ paddingTop: 36 }}>
        <div className="section-label">{"// verify it yourself"}</div>
        <p
          style={{
            fontSize: 14,
            color: "var(--muted)",
            lineHeight: 1.6,
            maxWidth: 580,
            marginBottom: 20,
          }}
        >
          The three contracts are live on Stellar testnet. Open any of them and read
          the invocation history — nothing here depends on taking our word for it.
        </p>

        <div style={{ display: "grid", gap: 10 }}>
          {CONTRACTS.map((c) => (
            <a
              key={c.id}
              href={explorer(c.id)}
              target="_blank"
              rel="noopener noreferrer"
              className="card"
              style={{ display: "block", textDecoration: "none" }}
            >
              <div
                style={{
                  fontFamily: "var(--mono)",
                  fontSize: 13,
                  color: "var(--accent)",
                  marginBottom: 4,
                }}
              >
                {c.name}
              </div>
              <div
                style={{
                  fontSize: 13,
                  color: "var(--muted)",
                  lineHeight: 1.5,
                  marginBottom: 8,
                }}
              >
                {c.does}
              </div>
              <div
                style={{
                  fontFamily: "var(--mono)",
                  fontSize: 10,
                  color: "var(--dim)",
                  overflowWrap: "anywhere",
                }}
              >
                {c.id}
              </div>
            </a>
          ))}
        </div>

        <p
          style={{
            fontSize: 12,
            color: "var(--dim)",
            lineHeight: 1.6,
            marginTop: 20,
            maxWidth: 580,
          }}
        >
          The task id and transaction hash in the animation are samples — they stand
          in for whatever a given run produces. The endpoints, contract functions and
          field names are not: they are what the code actually calls.
        </p>
      </section>
    </div>
  );
}
