/**
 * Tests for wallet.ts — Stellar Wallets Kit with localStorage persistence.
 */

const mockAuthModal = jest.fn();
const mockSignTransaction = jest.fn();
const mockGetAddress = jest.fn();
const mockDisconnect = jest.fn();
const mockInit = jest.fn();
const mockSetWallet = jest.fn();

jest.mock("@creit.tech/stellar-wallets-kit", () => ({
  StellarWalletsKit: {
    init: mockInit,
    authModal: mockAuthModal,
    signTransaction: mockSignTransaction,
    getAddress: mockGetAddress,
    disconnect: mockDisconnect,
    setWallet: mockSetWallet,
  },
  Networks: {
    TESTNET: "Test SDF Network ; September 2015",
    PUBLIC: "Public Global Stellar Network ; September 2015",
  },
}));

jest.mock("@creit.tech/stellar-wallets-kit/modules/utils", () => ({
  defaultModules: () => [],
}));

// signTransaction now performs a network mismatch check via the Freighter
// standalone API. In jsdom there is no Freighter, so getNetwork would hang
// waiting for a content-script response. Stub it so the call resolves
// immediately and signTransaction proceeds.
jest.mock("@stellar/freighter-api", () => ({
  getNetwork: jest.fn().mockResolvedValue({ network: "TESTNET", networkPassphrase: "" }),
}));

// Mock localStorage
const mockStorage: Record<string, string> = {};
const localStorageMock = {
  getItem: jest.fn((key: string) => mockStorage[key] ?? null),
  setItem: jest.fn((key: string, value: string) => { mockStorage[key] = value; }),
  removeItem: jest.fn((key: string) => { delete mockStorage[key]; }),
  clear: jest.fn(() => { Object.keys(mockStorage).forEach(k => delete mockStorage[k]); }),
  length: 0,
  key: jest.fn(),
};
Object.defineProperty(global, "localStorage", { value: localStorageMock, writable: true });

beforeEach(() => {
  jest.resetModules();
  jest.clearAllMocks();
  localStorageMock.clear();
});

describe("connectWallet", () => {
  test("returns publicKey from authModal", async () => {
    mockAuthModal.mockResolvedValueOnce({ address: "GABCDEF..." });
    const { connectWallet } = await import("./wallet");
    const result = await connectWallet();
    expect(result.publicKey).toBe("GABCDEF...");
  });

  test("saves publicKey to localStorage on connect", async () => {
    mockAuthModal.mockResolvedValueOnce({ address: "GUSER123" });
    const { connectWallet } = await import("./wallet");
    await connectWallet();
    expect(localStorageMock.setItem).toHaveBeenCalledWith("wallet_public_key", "GUSER123");
  });

  test("initializes SDK on first call", async () => {
    mockAuthModal.mockResolvedValueOnce({ address: "G..." });
    const { connectWallet } = await import("./wallet");
    await connectWallet();
    expect(mockInit).toHaveBeenCalledTimes(1);
  });
});

describe("disconnect", () => {
  test("clears localStorage on disconnect", async () => {
    mockAuthModal.mockResolvedValueOnce({ address: "G..." });
    const { connectWallet, disconnect } = await import("./wallet");
    await connectWallet();
    await disconnect();
    expect(localStorageMock.removeItem).toHaveBeenCalledWith("wallet_public_key");
  });
});

describe("getSavedWallet", () => {
  test("returns null when no wallet saved", async () => {
    const { getSavedWallet } = await import("./wallet");
    expect(getSavedWallet()).toBeNull();
  });

  test("returns saved publicKey from localStorage", async () => {
    mockStorage["wallet_public_key"] = "GSAVED123";
    const { getSavedWallet } = await import("./wallet");
    expect(getSavedWallet()).toBe("GSAVED123");
  });
});

describe("signTransaction", () => {
  test("returns signed XDR", async () => {
    mockAuthModal.mockResolvedValueOnce({ address: "G..." });
    mockSignTransaction.mockResolvedValueOnce({ signedTxXdr: "SIGNED_XDR_HERE" });
    const { connectWallet, signTransaction } = await import("./wallet");
    await connectWallet();
    const result = await signTransaction("UNSIGNED_XDR");
    expect(result).toBe("SIGNED_XDR_HERE");
  });
});

describe("getPublicKey", () => {
  test("returns address", async () => {
    mockAuthModal.mockResolvedValueOnce({ address: "G..." });
    mockGetAddress.mockResolvedValueOnce({ address: "GXYZ123" });
    const { connectWallet, getPublicKey } = await import("./wallet");
    await connectWallet();
    const key = await getPublicKey();
    expect(key).toBe("GXYZ123");
  });
});
