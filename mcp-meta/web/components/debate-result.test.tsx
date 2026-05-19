import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

jest.mock("react-markdown", () => {
  return function MockMarkdown({ children }: { children: string }) {
    return <div data-testid="markdown">{children}</div>;
  };
});

import { DebateResultView } from "./debate-result";

const SAMPLE_OUTPUT = `### Adr

\`\`\`markdown
## Title
PostgreSQL vs MongoDB

## Status
Finalized

## Decision
PostgreSQL is better for user profiles.

## Consequences
- Strong data integrity
- ACID compliance
\`\`\`

**Concession Detected:** True

### Decision

PostgreSQL is better because of data integrity and ACID compliance.

**Key Arguments:**
1. Data structure flexibility
2. Query performance

**Question:** Is postgres better than mongodb?

**Rounds:** 3`;

describe("DebateResultView", () => {
  test("renders two panels", () => {
    render(<DebateResultView output={SAMPLE_OUTPUT} />);
    // Left panel: conversation/decision
    expect(screen.getByText("// decision")).toBeInTheDocument();
    // Right panel: ADR document
    expect(screen.getByText("// adr document")).toBeInTheDocument();
  });

  test("extracts and renders ADR in right panel", () => {
    render(<DebateResultView output={SAMPLE_OUTPUT} />);
    const markdowns = screen.getAllByTestId("markdown");
    // At least one markdown block should contain ADR content
    const adrContent = markdowns.find(m => m.textContent?.includes("PostgreSQL vs MongoDB"));
    expect(adrContent).toBeDefined();
  });

  test("extracts and renders decision in left panel", () => {
    render(<DebateResultView output={SAMPLE_OUTPUT} />);
    const markdowns = screen.getAllByTestId("markdown");
    const decisionContent = markdowns.find(m => m.textContent?.includes("ACID compliance"));
    expect(decisionContent).toBeDefined();
  });

  test("decision panel does not contain Concession Detected", () => {
    render(<DebateResultView output={SAMPLE_OUTPUT} />);
    // The left panel markdown should not contain raw **Concession Detected:**
    const markdowns = screen.getAllByTestId("markdown");
    const leftPanel = markdowns[0]; // first markdown is the decision
    expect(leftPanel.textContent).not.toContain("Concession Detected");
  });

  test("shows question and rounds metadata", () => {
    render(<DebateResultView output={SAMPLE_OUTPUT} />);
    expect(screen.getByText(/postgres better than mongodb/i)).toBeInTheDocument();
    expect(screen.getByText(/3/)).toBeInTheDocument();
  });

  test("shows concession status", () => {
    render(<DebateResultView output={SAMPLE_OUTPUT} />);
    expect(screen.getByText(/concession: yes/i)).toBeInTheDocument();
  });

  test("falls back to single panel for non-debate output", () => {
    render(<DebateResultView output="Just a regular research result with no ADR structure." />);
    // Should render as single markdown block
    const markdown = screen.getByTestId("markdown");
    expect(markdown.textContent).toContain("regular research result");
  });
});
