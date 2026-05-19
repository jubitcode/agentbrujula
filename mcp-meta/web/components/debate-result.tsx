"use client";

import Markdown from "react-markdown";

interface DebateResultViewProps {
  output: string;
}

function parseDebateOutput(raw: string) {
  // Extract ADR from ```markdown ... ``` block
  const adrMatch = raw.match(/```(?:markdown)?\n([\s\S]*?)```/);
  const adr = adrMatch ? adrMatch[1].trim() : null;

  // Remove the ADR code block and everything before "Decision" to get the decision text
  let decisionText = raw;

  // Remove the ADR block
  if (adrMatch) {
    decisionText = decisionText.replace(adrMatch[0], "");
  }

  // Remove "### Adr" header if present
  decisionText = decisionText.replace(/^###?\s*Adr\s*\n*/im, "");

  // Remove **Concession Detected:** line (we parse it separately)
  decisionText = decisionText.replace(/\*\*Concession\s*Detected:?\*\*\s*(true|false)\s*\n*/i, "");

  // Remove **Question:** line (we parse it separately)
  decisionText = decisionText.replace(/\*\*Question:?\*\*\s*.+\n*/i, "");

  // Remove **Rounds:** line (we parse it separately)
  decisionText = decisionText.replace(/\*\*Rounds:?\*\*\s*\d+\s*\n*/i, "");

  // Clean up leading/trailing whitespace
  decisionText = decisionText.trim();

  // Extract question
  const questionMatch = raw.match(/\*\*Question:?\*\*\s*(.+)/i);
  const question = questionMatch ? questionMatch[1].trim() : null;

  // Extract rounds
  const roundsMatch = raw.match(/\*\*Rounds:?\*\*\s*(\d+)/i);
  const rounds = roundsMatch ? parseInt(roundsMatch[1]) : null;

  // Extract concession
  const concessionMatch = raw.match(/\*\*Concession\s*Detected:?\*\*\s*(true|false)/i);
  const concession = concessionMatch ? concessionMatch[1].toLowerCase() === "true" : null;

  return {
    adr,
    decision: decisionText || null,
    question,
    rounds,
    concession,
    isDebate: !!(adr || (decisionText && decisionText.includes("Decision"))),
  };
}

export function DebateResultView({ output }: DebateResultViewProps) {
  const parsed = parseDebateOutput(output);

  // Non-debate output: single panel with card styling
  if (!parsed.isDebate) {
    return (
      <div className="card">
        <div className="prose prose-sm max-w-none" style={{ color: "var(--muted)" }}>
          <Markdown>{output}</Markdown>
        </div>
      </div>
    );
  }

  return (
    <div style={{ maxWidth: 1100, margin: "0 auto" }}>
      {/* Metadata bar */}
      <div style={{
        display: "flex",
        gap: 16,
        marginBottom: 16,
        fontSize: 11,
        fontFamily: "var(--mono)",
        flexWrap: "wrap",
      }}>
        {parsed.question && (
          <span style={{ color: "var(--muted)" }}>{parsed.question}</span>
        )}
        {parsed.rounds && (
          <span style={{ color: "var(--dim)" }}>{parsed.rounds} rounds</span>
        )}
        {parsed.concession !== null && (
          <span style={{ color: parsed.concession ? "var(--green)" : "var(--dim)" }}>
            concession: {parsed.concession ? "yes" : "no"}
          </span>
        )}
      </div>

      {/* Two-panel layout */}
      <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 12 }}>
        {/* Left: Decision */}
        <div className="card" style={{ overflow: "auto", maxHeight: 700 }}>
          <div className="section-label" style={{ marginBottom: 16 }}>// decision</div>
          <div className="prose prose-sm max-w-none break-words" style={{ color: "var(--muted)" }}>
            <Markdown>{parsed.decision || ""}</Markdown>
          </div>
        </div>

        {/* Right: ADR Document */}
        <div className="card" style={{ overflow: "auto", maxHeight: 700 }}>
          <div className="section-label" style={{ marginBottom: 16 }}>// adr document</div>
          {parsed.adr ? (
            <div className="prose prose-sm max-w-none break-words" style={{ color: "var(--muted)" }}>
              <Markdown>{parsed.adr}</Markdown>
            </div>
          ) : (
            <p style={{ fontSize: 12, color: "var(--dim)" }}>No ADR document generated.</p>
          )}
        </div>
      </div>
    </div>
  );
}
