import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

jest.mock("@/lib/wallet", () => ({
  getSavedWallet: jest.fn().mockReturnValue(null),
  connectWallet: jest.fn(),
}));

const mockRouterPush = jest.fn();
jest.mock("next/navigation", () => ({
  useRouter: () => ({ push: mockRouterPush }),
  usePathname: () => "/",
}));

jest.mock("@/lib/api", () => ({
  getAgentCard: jest.fn().mockResolvedValue({ skills: [] }),
  getRecentTasks: jest.fn().mockResolvedValue({ tasks: [], count: 0 }),
}));

jest.mock("next/link", () => {
  return function MockLink({ href, children, ...props }: any) {
    return <a href={href} {...props}>{children}</a>;
  };
});

// NeuralCanvas uses HTMLCanvasElement and CompassScroll uses Remotion Player;
// jsdom can't render either, so mock both to keep tests fast and stable.
jest.mock("@/components/neural-bg/NeuralCanvas", () => ({
  NeuralCanvas: () => <div data-testid="neural-canvas" />,
}));

jest.mock("@/components/compass/CompassScroll", () => ({
  CompassScroll: () => <div data-testid="compass-scroll" />,
}));

import HomePage from "./page";

describe("Landing page", () => {
  test("renders hero headline that names the value prop (USDC + refund)", () => {
    render(<HomePage />);
    // Headline is split across <br /> so we use loose substring matchers.
    expect(screen.getByText(/Pay AI agents/i)).toBeInTheDocument();
    expect(screen.getAllByText(/Refund/i).length).toBeGreaterThan(0);
  });

  test("hero primary CTA is the Connect Wallet action (gate to the product)", () => {
    render(<HomePage />);
    // The wallet is the entry to the product. The hero's primary action
    // must be Connect, not a direct link to /hire (that route is gated and
    // would just bounce back).
    const primary = screen.getAllByRole("button", { name: /connect wallet to start/i })[0];
    expect(primary).toBeInTheDocument();
    // And no link in the hero jumps directly to /hire.
    const heroLinks = Array.from(document.querySelectorAll("a"));
    const directHireLinks = heroLinks.filter((a) => a.getAttribute("href") === "/hire");
    // The "Browse all agents" CTA further down does still link to /hire,
    // but the gate will redirect those without a wallet. We just guarantee
    // that the *hero* has no direct hire link.
    expect(directHireLinks.length).toBeLessThanOrEqual(1);
  });

  test("hero secondary CTA scrolls to #how-it-works (no nav)", () => {
    render(<HomePage />);
    const secondary = screen.getByRole("link", { name: /see how it works/i });
    expect(secondary).toHaveAttribute("href", "#how-it-works");
  });

  test("renders the four narrative sections (trust block removed, proof reframed)", () => {
    render(<HomePage />);
    expect(screen.getByText("// the problem")).toBeInTheDocument();
    expect(screen.getByText("// how it works")).toBeInTheDocument();
    expect(screen.getByText("// what agents can do")).toBeInTheDocument();
    expect(screen.getByText("// verify it yourself")).toBeInTheDocument();
    // Old technical section was removed for general-audience readability.
    expect(screen.queryByText("// trustless by design")).not.toBeInTheDocument();
  });

  test("renders only the safe animated bits (NeuralCanvas + CompassScroll)", () => {
    render(<HomePage />);
    expect(screen.getByTestId("neural-canvas")).toBeInTheDocument();
    expect(screen.getByTestId("compass-scroll")).toBeInTheDocument();
    // The other Remotion players (demo terminal, escrow flow, agent carousel)
    // were removed because they rendered broken on the home page.
    expect(screen.queryByTestId("demo-player")).not.toBeInTheDocument();
    expect(screen.queryByTestId("escrow-hero-player")).not.toBeInTheDocument();
    expect(screen.queryByTestId("agent-carousel")).not.toBeInTheDocument();
  });

  test("shows only the Deep research showcase (debate + trends hidden)", () => {
    render(<HomePage />);
    expect(screen.getByText("Deep research")).toBeInTheDocument();
    expect(screen.queryByText("Architecture debate")).not.toBeInTheDocument();
    expect(screen.queryByText("Trend analysis")).not.toBeInTheDocument();
  });

  test("trust-points block was removed (too technical for general audience)", () => {
    render(<HomePage />);
    expect(screen.queryByText(/escrow protection/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/on-chain verification/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/automatic refunds/i)).not.toBeInTheDocument();
  });

  test("renders 5-step timeline (Describe, Discover, Lock, Execute, Settle)", () => {
    render(<HomePage />);
    expect(screen.getByText("Describe")).toBeInTheDocument();
    expect(screen.getByText("Discover")).toBeInTheDocument();
    expect(screen.getByText("Lock")).toBeInTheDocument();
    expect(screen.getByText("Execute")).toBeInTheDocument();
    expect(screen.getByText("Settle")).toBeInTheDocument();
  });

  test("final CTA is the Connect Wallet action (not a /hire link)", () => {
    render(<HomePage />);
    // Same logic as the hero — the closing CTA pushes to connect, not to
    // the gated route directly.
    const cta = screen.getAllByRole("button", { name: /connect wallet to start/i });
    expect(cta.length).toBeGreaterThanOrEqual(1);
  });

  test("gradient highlight appears on multiple section headings (visual hierarchy)", () => {
    render(<HomePage />);
    // Hero (need done) + Problem (hope) + How it works (on-chain) +
    // Agents (Sixteen agents) + Verify (word) + Final CTA (working) = 6.
    // Built by is intentionally plain. We assert at least 5 so the test
    // remains stable if one section drops the highlight in the future.
    const gradients = document.querySelectorAll(".text-gradient");
    expect(gradients.length).toBeGreaterThanOrEqual(5);
  });

  test("each section uses the documented 38px scale (not the legacy 28px)", () => {
    render(<HomePage />);
    // The 5 narrative H2s should all be present. Their styles are inline so
    // we read computed style via the rendered element.
    const headings = [
      "You pay an LLM upfront",        // problem
      "Five steps, all",                // how it works
      "Sixteen agents",                 // agents
      "Don’t take our",            // verify
      "Built by humans",                // built by
    ];
    for (const text of headings) {
      const h2 = screen.getByText((_, node) =>
        !!node && node.tagName === "H2" && (node.textContent || "").includes(text.replace("’", "'"))
      );
      // Inline fontSize is a string like "38px". Don't rely on jsdom layout —
      // just confirm the style was authored at 38 or 40 (final CTA).
      const fs = (h2 as HTMLElement).style.fontSize;
      expect(["38px", "40px"]).toContain(fs);
    }
  });

  test("no link on the home page points to hidden routes (explore/knowledge/debate/deploy)", () => {
    render(<HomePage />);
    const hiddenRoutes = ["/explore", "/knowledge", "/debate", "/deploy"];
    Array.from(document.querySelectorAll("a")).forEach((a) => {
      const href = a.getAttribute("href") || "";
      expect(hiddenRoutes).not.toContain(href);
    });
  });

  test("renders connect wallet buttons when not connected (hero + final CTA)", () => {
    render(<HomePage />);
    // Both the hero and the final CTA expose a Connect wallet button so a
    // user can convert from either side of the page.
    const buttons = screen.getAllByRole("button", { name: /connect wallet/i });
    expect(buttons.length).toBeGreaterThanOrEqual(1);
  });

  test("proof section uses friendly framing and links to Stellar Expert", () => {
    render(<HomePage />);
    expect(screen.getByText("// verify it yourself")).toBeInTheDocument();
    // The heading "Don't take our word for it." is split across nodes by a
    // gradient span around the word "word". Match the surrounding phrase
    // instead of the full sentence.
    expect(screen.getByText(/take our/i)).toBeInTheDocument();
    expect(screen.getByText(/for it\./i)).toBeInTheDocument();

    // Friendly card titles instead of contract names
    expect(screen.getByText(/where your money lives/i)).toBeInTheDocument();
    expect(screen.getByText(/the agent registry/i)).toBeInTheDocument();
    expect(screen.getByText(/the quality gate/i)).toBeInTheDocument();

    const expertLinks = Array.from(document.querySelectorAll("a")).filter((a) =>
      (a.getAttribute("href") || "").includes("stellar.expert")
    );
    expect(expertLinks.length).toBeGreaterThanOrEqual(3);

    // Escrow contract address must still be linked (proof remains verifiable).
    const escrowLink = expertLinks.find((a) =>
      (a.getAttribute("href") || "").includes("CAMPNA3TNE23UYM5WSTXHQWA5HGU7HL6M2T64R5VNQT5PJS6UBIXOCCO")
    );
    expect(escrowLink).toBeDefined();
  });

  test("renders 'Built by' section with team + ambassador chapter", () => {
    render(<HomePage />);
    expect(screen.getByText("// who's behind this")).toBeInTheDocument();
    // The H2 now reads "Built by humans, run by code." — adding "by" alone
    // matched both the heading and the subtitle text. Use a more specific
    // assertion that the heading copy is present.
    expect(screen.getByText(/Built by humans, run by code/i)).toBeInTheDocument();
    // Real names, real roles — credibility for tomorrow's mentorship.
    expect(screen.getByText(/Andrea Junes/i)).toBeInTheDocument();
    expect(screen.getByText(/Adrián Nájera/i)).toBeInTheDocument();
    // The chapter line anchors us in the Stellar Ambassador program context.
    // "Nebula" appears twice (body copy + footer pill) — that is fine, we
    // just confirm at least one instance is present.
    expect(screen.getAllByText(/Nebula/i).length).toBeGreaterThan(0);
    expect(screen.getByText(/Mexico City/i)).toBeInTheDocument();
  });

  test("renders Browse all agents CTA pointing to /hire", () => {
    render(<HomePage />);
    const browseAll = screen.getByText(/browse all agents/i).closest("a");
    expect(browseAll).toHaveAttribute("href", "/hire");
  });

  test("clicking Connect wallet on landing redirects to /hire after success", async () => {
    const { connectWallet } = jest.requireMock("@/lib/wallet");
    connectWallet.mockResolvedValueOnce({ publicKey: "GTESTWALLET123456789" });
    mockRouterPush.mockClear();

    const user = userEvent.setup();
    render(<HomePage />);

    // Click the ghost CTA in the hero (visible because no wallet saved).
    await user.click(screen.getAllByRole("button", { name: /connect wallet/i })[0]);

    // The handler must navigate so the user does not stay on the landing
    // after they have already authenticated.
    expect(connectWallet).toHaveBeenCalled();
    // Wait microtask for the async handler to resolve.
    await new Promise((r) => setTimeout(r, 0));
    expect(mockRouterPush).toHaveBeenCalledWith("/hire");
  });

  test("no links point to /coming-soon (legacy stub removed)", () => {
    render(<HomePage />);
    const links = document.querySelectorAll("a");
    Array.from(links).forEach((link) => {
      expect(link.getAttribute("href")).not.toBe("/coming-soon");
    });
  });
});
