/**
 * Tests for DebatePage — x402 payment gated architecture debate.
 */

import { render, screen } from "@testing-library/react";
import { es } from "@/lib/i18n/es";

jest.mock("@/lib/marketplace", () => ({
  hireAgent: jest.fn(),
  buildPayment: jest.fn(),
  submitPayment: jest.fn(),
  prepareWallet: jest.fn(),
  streamExecution: jest.fn(),
  getResult: jest.fn(),
}));

jest.mock("@/lib/wallet", () => ({
  connectWallet: jest.fn(),
  signTransaction: jest.fn(),
}));

jest.mock("@/lib/wallet-auth", () => ({
  getWalletAuthHeader: jest.fn(),
}));

jest.mock("@/lib/stellar-env", () => ({
  HORIZON_URL: "https://horizon-testnet.stellar.org",
  STELLAR_NETWORK: "TESTNET",
}));

jest.mock("react-markdown", () => {
  return function MockMarkdown({ children }: { children: string }) {
    return <div data-testid="markdown">{children}</div>;
  };
});

import DebatePage from "./page";

beforeEach(() => {
  jest.clearAllMocks();
  global.fetch = jest.fn();
});

describe("DebatePage", () => {
  test("renders heading and input", () => {
    render(<DebatePage />);
    expect(screen.getByText(es["debate.title"] as string)).toBeInTheDocument();
    expect(
      screen.getByPlaceholderText(es["debate.placeholder"] as string),
    ).toBeInTheDocument();
  });

  test("renders example buttons", () => {
    render(<DebatePage />);
    const buttons = screen.getAllByRole("button");
    // At least 3 example buttons + submit button
    expect(buttons.length).toBeGreaterThanOrEqual(3);
  });
});
