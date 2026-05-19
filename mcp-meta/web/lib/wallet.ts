/**
 * Stellar Wallet Kit — with localStorage persistence.
 * Wallet address persists across page loads. User explicitly disconnects.
 */

import { STELLAR_NETWORK } from "./stellar-env";

const STORAGE_KEY = "wallet_public_key";

let _initialized = false;

async function ensureInit(): Promise<void> {
  if (_initialized) return;
  const { StellarWalletsKit, Networks } = await import("@creit.tech/stellar-wallets-kit");
  const { defaultModules } = await import("@creit.tech/stellar-wallets-kit/modules/utils");

  StellarWalletsKit.init({
    network: STELLAR_NETWORK === "PUBLIC" ? Networks.PUBLIC : Networks.TESTNET,
    modules: defaultModules(),
  });
  _initialized = true;
}

/**
 * Get the saved wallet public key from localStorage (no SDK call needed).
 * Returns null if no wallet is saved.
 */
export function getSavedWallet(): string | null {
  if (typeof window === "undefined") return null;
  return localStorage.getItem(STORAGE_KEY);
}

/**
 * Connect wallet via modal. Saves address to localStorage for persistence.
 */
export async function connectWallet(): Promise<{ publicKey: string }> {
  await ensureInit();
  const { StellarWalletsKit } = await import("@creit.tech/stellar-wallets-kit");
  const { address } = await StellarWalletsKit.authModal();
  localStorage.setItem(STORAGE_KEY, address);
  return { publicKey: address };
}

/**
 * Read the current network the wallet (Freighter) is on. Returns null if we
 * cannot ask the wallet — caller should treat that as "do not block".
 */
export async function getWalletNetwork(): Promise<"PUBLIC" | "TESTNET" | null> {
  try {
    const { getNetwork } = await import("@stellar/freighter-api");
    const res = await getNetwork();
    if (!res || res.error) return null;
    if (res.network === "PUBLIC") return "PUBLIC";
    if (res.network === "TESTNET") return "TESTNET";
    return null;
  } catch {
    return null;
  }
}

/**
 * Throws a friendly error if the connected wallet is on a different Stellar
 * network than the app expects. Catching code in the UI surfaces this directly
 * so the user does not have to read Freighter's modal text.
 */
export async function assertWalletOnExpectedNetwork(): Promise<void> {
  const expected = STELLAR_NETWORK === "PUBLIC" ? "PUBLIC" : "TESTNET";
  const actual = await getWalletNetwork();
  if (actual && actual !== expected) {
    const expectedLabel = expected === "PUBLIC" ? "Mainnet" : "Testnet";
    const actualLabel = actual === "PUBLIC" ? "Mainnet" : "Testnet";
    throw new Error(
      `Your wallet is on ${actualLabel}. Brújula runs on ${expectedLabel}. ` +
        `Open Freighter, switch the network to ${expectedLabel}, then try again.`
    );
  }
}

export async function signTransaction(xdr: string): Promise<string> {
  await ensureInit();
  await assertWalletOnExpectedNetwork();
  const { StellarWalletsKit, Networks } = await import("@creit.tech/stellar-wallets-kit");
  const { signedTxXdr } = await StellarWalletsKit.signTransaction(xdr, {
    networkPassphrase: STELLAR_NETWORK === "PUBLIC" ? Networks.PUBLIC : Networks.TESTNET,
  });
  return signedTxXdr;
}

export async function getPublicKey(): Promise<string> {
  await ensureInit();
  const { StellarWalletsKit } = await import("@creit.tech/stellar-wallets-kit");
  const { address } = await StellarWalletsKit.getAddress();
  return address;
}

/**
 * Disconnect wallet and clear persistence.
 */
export async function disconnect(): Promise<void> {
  const { StellarWalletsKit } = await import("@creit.tech/stellar-wallets-kit");
  await StellarWalletsKit.disconnect();
  localStorage.removeItem(STORAGE_KEY);
  _initialized = false;
}
