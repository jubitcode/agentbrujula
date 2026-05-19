import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

jest.mock("next/link", () => {
  return function MockLink({ href, children, ...props }: any) {
    return <a href={href} {...props}>{children}</a>;
  };
});

const mockRouterPush = jest.fn();
jest.mock("next/navigation", () => ({
  usePathname: () => "/hire",
  useRouter: () => ({ push: mockRouterPush }),
}));

jest.mock("@/lib/wallet", () => ({
  connectWallet: jest.fn(),
  disconnect: jest.fn(),
  getSavedWallet: jest.fn(),
}));

jest.mock("@/lib/api", () => ({
  sendMessage: jest.fn(),
}));

jest.mock("@/lib/marketplace", () => ({
  prepareWallet: jest.fn(),
}));

import { Header } from "./header";
import * as wallet from "@/lib/wallet";

const mockWallet = wallet as jest.Mocked<typeof wallet>;

beforeEach(() => {
  jest.clearAllMocks();
  // Default: balance lookup resolves cleanly. Tests that care about the
  // failure path or specific value override this.
  const marketplace = jest.requireMock("@/lib/marketplace");
  marketplace.prepareWallet.mockResolvedValue({
    public_key: "GUSER",
    has_trustline: true,
    usdc_balance: 0,
    usdc_balance_display: "0",
    trustline_xdr: null,
    mint_tx: null,
    usdc_contract: "",
  });
});

describe("Header", () => {
  test("logo links to landing when wallet is NOT connected", () => {
    mockWallet.getSavedWallet.mockReturnValue(null);
    render(<Header />);
    const logo = screen.getByRole("img", { name: /brújula/i });
    expect(logo).toBeInTheDocument();
    expect(logo.closest("a")).toHaveAttribute("href", "/");
  });

  test("logo links to /hire when wallet IS connected (skip landing for active users)", () => {
    mockWallet.getSavedWallet.mockReturnValue("GUSER123456789012345");
    render(<Header />);
    const logo = screen.getByRole("img", { name: /brújula/i });
    expect(logo.closest("a")).toHaveAttribute("href", "/hire");
  });

  test("shows Connect Wallet when no wallet saved", () => {
    mockWallet.getSavedWallet.mockReturnValue(null);
    render(<Header />);
    expect(screen.getByText("Connect Wallet")).toBeInTheDocument();
  });

  test("shows truncated address when wallet is saved", () => {
    mockWallet.getSavedWallet.mockReturnValue("GABCDEFGHIJKLMNOP");
    render(<Header />);
    expect(screen.getByText(/GABC.*MNOP/)).toBeInTheDocument();
  });

  test("header has zero nav links — only logo + wallet (single-page product)", () => {
    mockWallet.getSavedWallet.mockReturnValue(null);
    render(<Header />);
    // The product flow is single-page (/hire). Showing one nav item is silly
    // and showing more invites users to pages we hid for tomorrow's demo.
    for (const label of ["Explore", "Research", "Hire", "Debate", "Knowledge"]) {
      expect(screen.queryByRole("link", { name: label })).toBeNull();
    }
  });

  test("connect wallet calls connectWallet AND redirects to /hire on success", async () => {
    // Production bug: after connecting from the header on the landing, the
    // user stayed on /. Now that the landing → product transition is
    // explicit, the click handler must navigate too.
    mockRouterPush.mockClear();
    mockWallet.getSavedWallet.mockReturnValue(null);
    mockWallet.connectWallet.mockResolvedValueOnce({ publicKey: "GNEWWALLET123456" });
    render(<Header />);

    await userEvent.click(screen.getByText("Connect Wallet"));

    expect(mockWallet.connectWallet).toHaveBeenCalled();
    // Wait a microtask so the async handler resolves before we assert.
    await new Promise((r) => setTimeout(r, 0));
    expect(mockRouterPush).toHaveBeenCalledWith("/hire");
  });

  test("connect wallet does NOT redirect if user cancels (no publicKey)", async () => {
    mockRouterPush.mockClear();
    mockWallet.getSavedWallet.mockReturnValue(null);
    mockWallet.connectWallet.mockRejectedValueOnce(new Error("user cancelled"));
    render(<Header />);

    await userEvent.click(screen.getByText("Connect Wallet"));
    await new Promise((r) => setTimeout(r, 0));

    expect(mockRouterPush).not.toHaveBeenCalled();
  });

  test("wallet dropdown shows live USDC balance + Stellar Expert link", async () => {
    mockRouterPush.mockClear();
    mockWallet.getSavedWallet.mockReturnValue("GUSER123456789012345");
    const marketplace = jest.requireMock("@/lib/marketplace");
    marketplace.prepareWallet.mockResolvedValueOnce({
      public_key: "GUSER123456789012345",
      has_trustline: true,
      usdc_balance: 4_500_000,
      usdc_balance_display: "0.45",
      trustline_xdr: null,
      mint_tx: null,
      usdc_contract: "CDUQPQ",
    });

    render(<Header />);
    // Open the dropdown
    await userEvent.click(screen.getByText(/GUSE.*2345/));

    // Balance must show within the dropdown (no mock fetches; uses the real
    // prepareWallet shape).
    await new Promise((r) => setTimeout(r, 30));
    expect(marketplace.prepareWallet).toHaveBeenCalledWith("GUSER123456789012345");
    // The "0.45" and "USDC" labels live in separate spans for type weighting.
    // Find them independently so a future visual tweak does not break this.
    await waitFor(() => expect(screen.getByText(/0\.45/)).toBeInTheDocument());
    expect(screen.getAllByText(/USDC/i).length).toBeGreaterThan(0);
    // And a link to verify the account on-chain
    const expertLink = screen.getByRole("link", { name: /stellar expert/i });
    expect(expertLink.getAttribute("href")).toMatch(/stellar\.expert.*GUSER123456789012345/);
  });

  test("dropdown shows a graceful fallback when balance lookup fails", async () => {
    mockWallet.getSavedWallet.mockReturnValue("GUSER111111111111111");
    const marketplace = jest.requireMock("@/lib/marketplace");
    marketplace.prepareWallet.mockRejectedValueOnce(new Error("rpc down"));

    render(<Header />);
    await userEvent.click(screen.getByText(/GUSE.*1111/));
    await new Promise((r) => setTimeout(r, 30));
    // The dropdown must remain usable even if the balance call fails.
    expect(screen.getByText(/balance unavailable|—/i)).toBeInTheDocument();
  });

  test("copy button gives visual feedback after copying and reverts later", async () => {
    // The button must FEEL responsive: hover + pressed states come for free
    // through CSS, but the post-click acknowledgement (icon swap + "Copied!"
    // label + green tint) is what makes the action feel like it actually
    // happened. We assert that loop here. Fake timers let us flush the
    // 1.5s revert without burning real test time; userEvent.setup is
    // configured to advance them so async clicks still work.
    jest.useFakeTimers();
    try {
      const user = userEvent.setup({ advanceTimers: jest.advanceTimersByTime });

      mockWallet.getSavedWallet.mockReturnValue("GCOPYWALLET1234567890");
      const writeText = jest.fn().mockResolvedValue(undefined);
      Object.defineProperty(navigator, "clipboard", {
        value: { writeText },
        configurable: true,
      });

      render(<Header />);
      await user.click(screen.getByText(/GCOP.*7890/));

      const copyBtn = screen.getByRole("button", { name: /copy full wallet address/i });
      expect(copyBtn).toBeInTheDocument();
      expect(screen.getByText(/^Copy$/)).toBeInTheDocument();

      await user.click(copyBtn);
      expect(writeText).toHaveBeenCalledWith("GCOPYWALLET1234567890");

      // Feedback visible
      expect(await screen.findByText(/Copied/i)).toBeInTheDocument();
      expect(screen.queryByText(/^Copy$/)).toBeNull();

      // Advance past the 1.5s revert window
      jest.advanceTimersByTime(1600);
      expect(await screen.findByText(/^Copy$/)).toBeInTheDocument();
      expect(screen.queryByText(/Copied/i)).toBeNull();
    } finally {
      jest.useRealTimers();
    }
  });

  test("disconnect wallet routes the user back to the landing", async () => {
    // Reproduces the production bug: the user lands in /hire after they've
    // connected, then hits Disconnect — the chrome cleared their wallet
    // state but left them stranded on the authenticated page.
    mockRouterPush.mockClear();
    mockWallet.getSavedWallet.mockReturnValue("GABCDEFGHIJKLMNOP");
    mockWallet.disconnect.mockResolvedValueOnce(undefined);

    render(<Header />);

    // Open the wallet dropdown
    await userEvent.click(screen.getByText(/GABC.*MNOP/));
    // Click Disconnect inside it
    await userEvent.click(screen.getByText(/disconnect/i));

    expect(mockWallet.disconnect).toHaveBeenCalled();
    expect(mockRouterPush).toHaveBeenCalledWith("/");
  });
});
