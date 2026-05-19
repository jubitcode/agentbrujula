/**
 * E2E integration test for the SOW-aligned hire flow.
 *
 * Simulates the full happy path with mocked APIs in JSDOM:
 *   input → discover → pick agent → 402 → connect wallet
 *     → build tx → sign → submit → execute → result
 *
 * No real Stellar / FastAPI / mcp-meta calls. The intent is to lock the
 * UX flow shape so it can't silently break before tomorrow's demo.
 */

import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

jest.mock("next/link", () => {
  return function MockLink({ href, children, ...props }: any) {
    return <a href={href} {...props}>{children}</a>;
  };
});

// next/navigation is mocked so the wallet gate's `router.replace("/")` can be
// observed by assertions. The same shared mock object is reused across tests.
const mockReplace = jest.fn();
const mockPush = jest.fn();
jest.mock("next/navigation", () => ({
  useRouter: () => ({ replace: mockReplace, push: mockPush }),
  usePathname: () => "/hire",
}));

jest.mock("react-markdown", () => {
  return function MockMarkdown({ children }: { children: string }) {
    return <div>{children}</div>;
  };
});

jest.mock("@/components/debate-result", () => ({
  DebateResultView: ({ output }: { output: string }) => <div data-testid="debate-output">{output}</div>,
}));

// Marketplace API — happy-path responses for the entire flow.
jest.mock("@/lib/marketplace", () => {
  return {
    discoverAgents: jest.fn(async () => ({
      agents: [
        {
          handle: "research-agent",
          tier: "free",
          rate_xlm: 0.1,
          rep_score: 50,
          hebbian_confidence: 0.8,
          capabilities: ["research", "web_search"],
        },
      ],
      hebbian_nodes: 0,
      registry_contract: "CARJRAXMYOM7TNNCR2K4OVZTO6BYUTUIW5AB2ZHDSF5UZXBSWU7YZQBW",
      rep_contract: "CDJCWXFHMO3QSZJNP6ZPQKOB4Z6EC6R5SBOOBJ5ULIWPW4FYD4N2KTVY",
    })),
    hireAgent: jest.fn(async () => ({
      status: 402,
      paymentInfo: {
        estimated_cost: "0.10",
        budget_ceiling: "0.15",
        rate_breakdown: [{ tool: "search", rate: "0.05" }],
        network: "stellar:testnet",
        payTo: "GORCHESTRATOR000000000000000000000000000000000000000000000",
        token: "USDC",
      },
    })),
    prepareWallet: jest.fn(async () => ({
      has_trustline: true,
      trustline_xdr: null,
      usdc_balance: "10",
    })),
    buildPayment: jest.fn(async () => ({
      task_id: "task-e2e-001",
      unsigned_xdr: "AAAA-fake-xdr",
      amount_usdc: "0.15",
      destination_label: "Escrow",
    })),
    submitPayment: jest.fn(async () => ({
      task_id: "task-e2e-001",
      tx_hash: "abc123",
    })),
    streamExecution: jest.fn((taskId: string, addEvent: any, onComplete: any) => {
      // Mirror what the FastAPI backend emits today — technical strings that
      // the frontend must translate for end users.
      addEvent({ type: "log", timestamp: "12:00:00", message: "Sending to A2A" });
      addEvent({ type: "tool_call", timestamp: "12:00:01", message: "A2A task: c6d7e6216e207d1d631a56ccfa3930c4" });
      addEvent({ type: "complete", timestamp: "12:00:05", message: "Execution complete" });
      setTimeout(() => onComplete(), 0);
    }),
    getResult: jest.fn(async () => ({
      output: "## Result\n\nFound key insights.",
      compliant: true,
      settlement: {
        actual_cost_usdc: "0.08",
        settle_tx: "deadbeefcafebabe000000000000000000000000000000000000000000000000",
      },
      settlement_status: "settled",
    })),
  };
});

jest.mock("@/lib/wallet", () => ({
  connectWallet: jest.fn(async () => ({ publicKey: "GTESTUSER000000000000000000000000000000000000000000000000" })),
  signTransaction: jest.fn(async () => "AAAA-signed-xdr"),
  // Default: a wallet IS saved. /hire is auth-gated; tests that exercise the
  // hire flow assume the visitor is already past the gate. The gate test
  // overrides this to null below.
  getSavedWallet: jest.fn(() => "GTESTUSER000000000000000000000000000000000000000000000000"),
  disconnect: jest.fn(),
  assertWalletOnExpectedNetwork: jest.fn(async () => undefined),
  getWalletNetwork: jest.fn(async () => "TESTNET"),
}));

jest.mock("@/lib/wallet-auth", () => ({
  getWalletAuthHeader: jest.fn(async () => undefined),
}));

jest.mock("@/lib/stellar-env", () => ({
  HORIZON_URL: "https://horizon-testnet.stellar.org",
  EXPLORER_BASE: "https://stellar.expert/explorer/testnet",
}));

import HirePage, { humaniseEventMessage, humaniseResultMarkdown } from "./page";

// ─────────────────────────────────────────────────────────────────────────
// Welcome banner — first visit after connecting shows a small banner. It
// must dismiss on click AND not return on the next render within the same
// session (state lives in sessionStorage). Uses real sessionStorage, no
// mock — the storage API is part of what we're testing.
// ─────────────────────────────────────────────────────────────────────────
describe("HirePage welcome banner", () => {
  beforeEach(() => {
    mockReplace.mockClear();
    mockPush.mockClear();
    window.sessionStorage.clear();
  });

  test("renders on first visit", async () => {
    render(<HirePage />);
    await waitFor(() => {
      expect(screen.getByTestId("welcome-banner")).toBeInTheDocument();
    });
    expect(screen.getByText(/welcome back/i)).toBeInTheDocument();
  });

  test("auto-dismisses after a timeout without user interaction", async () => {
    jest.useFakeTimers();
    try {
      render(<HirePage />);
      await waitFor(() => screen.getByTestId("welcome-banner"));

      // 4s in: still there (we don't want to flash a banner away faster
      // than the user can read it).
      jest.advanceTimersByTime(4000);
      expect(screen.getByTestId("welcome-banner")).toBeInTheDocument();

      // Past 8s: gone.
      jest.advanceTimersByTime(5000);
      await waitFor(() =>
        expect(screen.queryByTestId("welcome-banner")).toBeNull()
      );

      // And the auto-dismiss persists — re-rendering does not bring it back
      // within the same session.
      expect(window.sessionStorage.getItem("welcome_dismissed")).toBeTruthy();
    } finally {
      jest.useRealTimers();
    }
  });

  test("dismiss button hides the banner AND persists across remount", async () => {
    const user = userEvent.setup();
    const { unmount } = render(<HirePage />);
    await waitFor(() => screen.getByTestId("welcome-banner"));
    await user.click(screen.getByRole("button", { name: /dismiss welcome/i }));
    expect(screen.queryByTestId("welcome-banner")).toBeNull();

    // Remount — banner should NOT reappear in the same session.
    unmount();
    render(<HirePage />);
    await waitFor(() =>
      expect(screen.getByLabelText(/task description/i)).toBeInTheDocument()
    );
    expect(screen.queryByTestId("welcome-banner")).toBeNull();
  });
});

// ─────────────────────────────────────────────────────────────────────────
// Wallet gate — /hire is auth-gated. A visitor without a wallet must be
// redirected to the landing. The shared mock returns a saved wallet by
// default; this block overrides it to null for the closed-gate case.
// ─────────────────────────────────────────────────────────────────────────
describe("HirePage wallet gate", () => {
  beforeEach(() => {
    mockReplace.mockClear();
    mockPush.mockClear();
  });

  test("redirects to / when no wallet is saved (gate closed)", async () => {
    const wallet = jest.requireMock("@/lib/wallet");
    wallet.getSavedWallet.mockReturnValueOnce(null);

    render(<HirePage />);

    await waitFor(() => {
      expect(mockReplace).toHaveBeenCalledWith("/");
    });
  });

  test("does NOT redirect when a wallet is already saved (gate open)", async () => {
    // Default mock returns a wallet so no override needed.
    render(<HirePage />);

    // Give the gate effect a tick to run.
    await new Promise((r) => setTimeout(r, 50));
    expect(mockReplace).not.toHaveBeenCalledWith("/");
    // And the actual form is reachable.
    await waitFor(() =>
      expect(screen.getByLabelText(/task description/i)).toBeInTheDocument()
    );
  });
});

// ─────────────────────────────────────────────────────────────────────────
// Unit coverage for the event message translator. Pure function, no React.
// The integration of these strings into the running UI is in HirePage.
// ─────────────────────────────────────────────────────────────────────────
describe("humaniseEventMessage", () => {
  test("translates the FastAPI 'Sending to A2A' to plain English", () => {
    expect(humaniseEventMessage("Sending to A2A", "log")).toBe(
      "Reaching out to the agent…"
    );
  });

  test("hides the raw A2A task hash from the user", () => {
    const raw = "A2A task: c6d7e6216e207d1d631a56ccfa3930c4";
    const friendly = humaniseEventMessage(raw, "tool_call");
    expect(friendly).toBe("Agent picked it up");
    expect(friendly).not.toMatch(/c6d7e621/);
  });

  test("turns 'Execution complete' into a celebratory user-facing message", () => {
    expect(humaniseEventMessage("Execution complete", "complete")).toBe(
      "The agent finished"
    );
  });

  test("rewrites tool-call traces like 'search invoked' to 'Used: search'", () => {
    expect(humaniseEventMessage("search invoked", "tool_call")).toBe("Used: search");
  });

  test("scrubs any 16+ char hex hash from unknown messages", () => {
    const unknown = "submitted tx 0123456789abcdef0123456789abcdef please wait";
    const out = humaniseEventMessage(unknown, "log");
    expect(out).not.toMatch(/0123456789abcdef/);
  });

  test("falls back to a safe phrase when the message is empty", () => {
    expect(humaniseEventMessage("", "log")).toBe("Working…");
  });

  test("removes the trailing ellipsis from log messages", () => {
    expect(humaniseEventMessage("Loading...", "log")).toMatch(/^Loading$/);
  });
});

// ─────────────────────────────────────────────────────────────────────────
// humaniseResultMarkdown — strips machine-named labels ("Hebbian Knowledge
// Graph") from the rendered result so general users understand the page.
// ─────────────────────────────────────────────────────────────────────────
describe("humaniseResultMarkdown", () => {
  test("renames 'Hebbian Knowledge Graph' heading to a friendly label", () => {
    const md = "## Hebbian Knowledge Graph\n\nSome remembered fact about blockchain.";
    const out = humaniseResultMarkdown(md);
    expect(out).not.toMatch(/Hebbian/);
    expect(out).toMatch(/what we already knew|remembered|from memory/i);
  });

  test("works on bold-prefixed variants too (LLM sometimes writes **Hebbian Knowledge Graph**)", () => {
    const md = "**Hebbian Knowledge Graph:**\n- bullet";
    const out = humaniseResultMarkdown(md);
    expect(out).not.toMatch(/Hebbian/);
  });

  test("strips other Hebbian mentions inline without breaking the surrounding text", () => {
    const md = "The Hebbian memory recalled this. Hebbian weight = 0.5.";
    const out = humaniseResultMarkdown(md);
    expect(out).not.toMatch(/Hebbian/i);
    expect(out).toMatch(/memory/i);
  });

  test("leaves regular markdown untouched", () => {
    const md = "# Research summary\n\nBlockchain is a distributed ledger.";
    expect(humaniseResultMarkdown(md)).toBe(md);
  });

  test("returns empty string for empty input", () => {
    expect(humaniseResultMarkdown("")).toBe("");
  });
});

describe("Hire flow — full E2E happy path (SOW E1 + E2 + E3)", () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  test("user can: input task → discover → pick agent → see 402 → pay → see result", async () => {
    const user = userEvent.setup();
    render(<HirePage />);

    // ─── Step 1: type the task. Capability auto-detects from keywords. ──
    const taskInput = await screen.findByLabelText(/task description/i);
    await user.type(taskInput, "Research AI infra trends for 2026");

    // No more manual capability picker in the main form. The CTA enables as
    // soon as the task is long enough.
    const findBtn = screen.getByRole("button", { name: /find an agent/i });
    expect(findBtn).not.toBeDisabled();
    await user.click(findBtn);

    // ─── Step 2: agent appears in a redesigned card with Best match badge
    await waitFor(() => {
      expect(screen.getByText("research-agent")).toBeInTheDocument();
    });
    expect(screen.getByText(/best match/i)).toBeInTheDocument();

    await user.click(screen.getByText("research-agent"));

    // ─── Step 3: 402 payment screen (now headlined "Confirm payment") ──
    await waitFor(() => {
      expect(screen.getByText(/confirm payment/i)).toBeInTheDocument();
    });
    expect(screen.getByText(/Paying research-agent/i)).toBeInTheDocument();
    expect(screen.getByText(/connect wallet and pay/i)).toBeInTheDocument();

    // ─── Step 4: pay → executes → settles → done ──────────────────────
    await user.click(screen.getByRole("button", { name: /connect wallet and pay/i }));

    await waitFor(
      () => {
        expect(screen.getByText(/task complete/i)).toBeInTheDocument();
      },
      { timeout: 4000 }
    );

    expect(screen.getByText(/found key insights/i)).toBeInTheDocument();
    expect(screen.getByText(/PASS/i)).toBeInTheDocument();
    expect(screen.getByText(/0.08 USDC/)).toBeInTheDocument();
  }, 10000);

  test("error path — discover failure surfaces a Try Again button", async () => {
    const user = userEvent.setup();

    const marketplace = jest.requireMock("@/lib/marketplace");
    marketplace.discoverAgents.mockRejectedValueOnce(new Error("registry unreachable"));

    render(<HirePage />);

    const taskInput = await screen.findByLabelText(/task description/i);
    await user.type(taskInput, "Some task that is long enough");
    await user.click(screen.getByRole("button", { name: /find an agent/i }));

    await waitFor(() => {
      expect(screen.getByText(/registry unreachable/i)).toBeInTheDocument();
    });
    expect(screen.getByRole("button", { name: /try again/i })).toBeInTheDocument();
  });

  // Translation is exercised at the unit level (see describe block at the
  // bottom of this file). Re-running it through the full hire flow is too
  // flaky because the execution-stream UI unmounts as soon as the task
  // completes, and the events are not preserved on the done view.

  test("network mismatch: shows friendly error before any payment is built", async () => {
    const user = userEvent.setup();
    const wallet = jest.requireMock("@/lib/wallet");
    const marketplace = jest.requireMock("@/lib/marketplace");

    wallet.assertWalletOnExpectedNetwork.mockRejectedValueOnce(
      new Error("Your wallet is on Mainnet. Brújula runs on Testnet. Open Freighter, switch the network to Testnet, then try again.")
    );

    render(<HirePage />);

    const taskInput = await screen.findByLabelText(/task description/i);
    await user.type(taskInput, "research the meaning of x402 protocol");
    await user.click(screen.getByRole("button", { name: /find an agent/i }));

    await waitFor(() => screen.getByText("research-agent"));
    await user.click(screen.getByText("research-agent"));

    await waitFor(() => screen.getByText(/confirm payment/i));
    await user.click(screen.getByRole("button", { name: /connect wallet and pay/i }));

    // The dedicated network-mismatch step replaces the generic error step,
    // and shows step-by-step instructions plus a retry button that preserves
    // the form state.
    await waitFor(() => {
      expect(screen.getByText(/wrong network selected/i)).toBeInTheDocument();
    });
    expect(screen.getByText(/Open the Freighter extension/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /I switched\. Try again/i })).toBeInTheDocument();

    // No payment was built because the assertion fired before the API call.
    expect(marketplace.buildPayment).not.toHaveBeenCalled();
    expect(marketplace.submitPayment).not.toHaveBeenCalled();
  });

  test("capability picker is hidden by default; appears inside Advanced", async () => {
    const user = userEvent.setup();
    render(<HirePage />);
    await screen.findByLabelText(/task description/i);

    // Capability chips are NOT in the main form anymore — they live behind
    // "Advanced options". This keeps the casual UX clean.
    for (const cap of ["research", "writing", "trends", "summarize"]) {
      expect(screen.queryByRole("button", { name: new RegExp(`^${cap}$`, "i") })).toBeNull();
    }

    // Open Advanced — now the four supported caps appear.
    await user.click(screen.getByRole("button", { name: /advanced options/i }));
    expect(screen.getByRole("button", { name: /^research$/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^writing$/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^trends$/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^summarize$/i })).toBeInTheDocument();

    // The seven dropped ones are still hidden — even inside Advanced.
    for (const fake of ["analysis", "web_search", "code_review", "security", "nlp", "news", "monitoring"]) {
      expect(screen.queryByRole("button", { name: new RegExp(`^${fake}$`, "i") })).toBeNull();
    }
  });

  test("hero shows kicker + emotive H1 with both lines + subtitle", async () => {
    render(<HirePage />);
    await screen.findByLabelText(/task description/i);
    expect(screen.getByText(/pay only when the work ships/i)).toBeInTheDocument();
    expect(screen.getByText("What do you")).toBeInTheDocument();
    expect(screen.getByText("need done?")).toBeInTheDocument();
    expect(screen.getByText(/AI agent will pick it up/i)).toBeInTheDocument();
    // The bland old strings are gone.
    expect(screen.queryByText("// hire")).toBeNull();
    expect(screen.queryByText(/Discover agents on-chain →/)).toBeNull();
  });

  test("clicking an example prompt fills the textarea", async () => {
    const user = userEvent.setup();
    render(<HirePage />);
    const textarea = await screen.findByLabelText(/task description/i) as HTMLTextAreaElement;
    expect(textarea.value).toBe("");

    const example = screen.getByRole("button", { name: /research the x402 protocol/i });
    await user.click(example);
    expect(textarea.value).toMatch(/x402 protocol/i);
  });

  test("auto-detect chip appears when the task text matches a capability", async () => {
    const user = userEvent.setup();
    render(<HirePage />);
    const textarea = await screen.findByLabelText(/task description/i);

    await user.type(textarea, "Write a blog post about Brújula");
    expect(screen.getByText(/Detected: writing task/i)).toBeInTheDocument();
  });

  test("budget input does not appear in the main form (only inside Advanced)", async () => {
    const user = userEvent.setup();
    render(<HirePage />);
    await screen.findByLabelText(/task description/i);

    expect(screen.queryByLabelText(/budget/i)).toBeNull();
    await user.click(screen.getByRole("button", { name: /advanced options/i }));
    expect(screen.getByLabelText(/max budget/i)).toBeInTheDocument();
  });

  test("settle_tx renders even when backend returns it as an object (not a string)", async () => {
    const user = userEvent.setup();
    const marketplace = jest.requireMock("@/lib/marketplace");
    marketplace.getResult.mockResolvedValueOnce({
      output: "ok",
      compliant: true,
      // Reproduce the production bug: settle_tx was wrapped as an object.
      settlement: { actual_cost_usdc: "0.08", settle_tx: { hash: "abcd1234efgh5678ijkl" } },
      settlement_status: "settled",
    });

    render(<HirePage />);
    const taskInput = await screen.findByLabelText(/task description/i);
    await user.type(taskInput, "research the meaning of x402 protocol");
    await user.click(screen.getByRole("button", { name: /find an agent/i }));
    await waitFor(() => screen.getByText("research-agent"));
    await user.click(screen.getByText("research-agent"));
    await waitFor(() => screen.getByText(/confirm payment/i));
    await user.click(screen.getByRole("button", { name: /connect wallet and pay/i }));

    await waitFor(
      () => {
        expect(screen.getByText(/task complete/i)).toBeInTheDocument();
      },
      { timeout: 4000 }
    );

    // The page must not crash; the hash is rendered as a string slice.
    expect(screen.getByText(/abcd1234efgh5678/i)).toBeInTheDocument();
  }, 10000);

  test("escrow contract address from SOW is visible in the payment confirmation", async () => {
    const user = userEvent.setup();
    render(<HirePage />);

    const taskInput = await screen.findByLabelText(/task description/i);
    await user.type(taskInput, "research the meaning of x402 protocol");
    await user.click(screen.getByRole("button", { name: /find an agent/i }));

    await waitFor(() => screen.getByText("research-agent"));
    await user.click(screen.getByText("research-agent"));

    await waitFor(() => {
      // The truncated mock payTo "GORCHEST…" must appear in the payment
      // detail line. We match on the start of the address to avoid coupling
      // to the exact escrow label string.
      expect(screen.getByText(/GORCHEST/i)).toBeInTheDocument();
    });
  });
});
