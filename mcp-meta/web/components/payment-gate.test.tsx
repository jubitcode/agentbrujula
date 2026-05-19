import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

jest.mock("@/lib/wallet", () => ({
  connectWallet: jest.fn(),
  signTransaction: jest.fn(),
  getSavedWallet: jest.fn(),
}));

jest.mock("@/lib/marketplace", () => ({
  buildGenericPayment: jest.fn(),
  prepareWallet: jest.fn(),
}));

jest.mock("@/lib/stellar-env", () => ({
  HORIZON_URL: "https://horizon-testnet.stellar.org",
  STELLAR_NETWORK: "TESTNET",
}));

import { PaymentGate } from "./payment-gate";
import * as wallet from "@/lib/wallet";
import * as marketplace from "@/lib/marketplace";

const mockWallet = wallet as jest.Mocked<typeof wallet>;
const mockMarketplace = marketplace as jest.Mocked<typeof marketplace>;

beforeEach(() => {
  jest.clearAllMocks();
  global.fetch = jest.fn();
});

const defaultPaymentInfo = {
  estimated_cost: "0.25",
  budget_ceiling: "0.375",
  network: "stellar:testnet",
  payTo: "GESCROW123456789",
  token: "USDC",
  facilitatorUrl: "",
};

describe("PaymentGate", () => {
  test("renders estimated cost", () => {
    render(
      <PaymentGate paymentInfo={defaultPaymentInfo} onPaymentComplete={jest.fn()} onCancel={jest.fn()} />
    );
    expect(screen.getByText(/0.25 USDC/)).toBeInTheDocument();
  });

  test("renders budget ceiling", () => {
    render(
      <PaymentGate paymentInfo={defaultPaymentInfo} onPaymentComplete={jest.fn()} onCancel={jest.fn()} />
    );
    expect(screen.getByText(/0.375/)).toBeInTheDocument();
  });

  test("renders escrow explanation", () => {
    render(
      <PaymentGate paymentInfo={defaultPaymentInfo} onPaymentComplete={jest.fn()} onCancel={jest.fn()} />
    );
    expect(screen.getByText(/locked in escrow/i)).toBeInTheDocument();
  });

  test("shows connect wallet when not connected", () => {
    mockWallet.getSavedWallet.mockReturnValue(null);
    render(
      <PaymentGate paymentInfo={defaultPaymentInfo} onPaymentComplete={jest.fn()} onCancel={jest.fn()} />
    );
    expect(screen.getByRole("button", { name: /connect wallet/i })).toBeInTheDocument();
  });

  test("shows pay button when wallet connected", () => {
    mockWallet.getSavedWallet.mockReturnValue("GUSER123");
    render(
      <PaymentGate paymentInfo={defaultPaymentInfo} onPaymentComplete={jest.fn()} onCancel={jest.fn()} />
    );
    expect(screen.getByRole("button", { name: /pay and execute/i })).toBeInTheDocument();
  });

  test("cancel button calls onCancel", async () => {
    const onCancel = jest.fn();
    render(
      <PaymentGate paymentInfo={defaultPaymentInfo} onPaymentComplete={jest.fn()} onCancel={onCancel} />
    );
    await userEvent.click(screen.getByText("Cancel"));
    expect(onCancel).toHaveBeenCalled();
  });

  test("shows network info", () => {
    render(
      <PaymentGate paymentInfo={defaultPaymentInfo} onPaymentComplete={jest.fn()} onCancel={jest.fn()} />
    );
    expect(screen.getByText(/stellar/i)).toBeInTheDocument();
  });

  test("full payment flow calls onPaymentComplete", async () => {
    const onComplete = jest.fn();
    mockWallet.getSavedWallet.mockReturnValue("GUSER...");
    mockWallet.connectWallet.mockResolvedValue({ publicKey: "GUSER..." });
    mockMarketplace.prepareWallet.mockResolvedValue({
      public_key: "GUSER...",
      has_trustline: true,
      usdc_balance: 100,
      usdc_balance_display: "100.00",
      trustline_xdr: null,
      mint_tx: null,
      usdc_contract: "C...",
    });
    mockMarketplace.buildGenericPayment.mockResolvedValue({
      unsigned_xdr: "XDR...",
      task_id: "x402-test",
      deadline: Date.now() / 1000 + 300,
      amount_usdc: "0.375",
      destination: "G...",
      token: "USDC",
    });
    mockWallet.signTransaction.mockResolvedValue("SIGNED_XDR");
    (global.fetch as jest.Mock).mockResolvedValueOnce({
      ok: true,
      json: async () => ({ hash: "tx_hash_123" }),
    });

    render(
      <PaymentGate paymentInfo={defaultPaymentInfo} onPaymentComplete={onComplete} onCancel={jest.fn()} />
    );

    await userEvent.click(screen.getByRole("button", { name: /pay/i }));

    await waitFor(() => {
      expect(onComplete).toHaveBeenCalled();
    });

    const sig = onComplete.mock.calls[0][0];
    const parsed = JSON.parse(atob(sig));
    expect(parsed.txHash).toBe("tx_hash_123");
    expect(parsed.task_id).toBe("x402-test");
    expect(parsed.success).toBe(true);
  });
});
