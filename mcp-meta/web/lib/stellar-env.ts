/**
 * Central Stellar network configuration for the frontend.
 *
 * Controlled by environment variables:
 *   NEXT_PUBLIC_STELLAR_NETWORK  — "TESTNET" (default) or "PUBLIC"
 *   NEXT_PUBLIC_HORIZON_URL      — overrides the network default
 */

export const STELLAR_NETWORK = process.env.NEXT_PUBLIC_STELLAR_NETWORK || "TESTNET";
export const HORIZON_URL =
  process.env.NEXT_PUBLIC_HORIZON_URL ||
  (STELLAR_NETWORK === "PUBLIC"
    ? "https://horizon.stellar.org"
    : "https://horizon-testnet.stellar.org");
