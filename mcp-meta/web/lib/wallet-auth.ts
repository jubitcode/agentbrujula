/**
 * Wallet auth for marketplace endpoints — session-token flow.
 *
 * Prior design: every protected request prompted Freighter to sign a fresh
 * challenge (single-use nonce). Result: 6 modals per hire flow.
 *
 * Current design:
 *   1. First protected call: exchange a signed challenge for a 1-hour JWT
 *      (POST /api/auth/session). Cache in sessionStorage.
 *   2. Subsequent calls: return cached `Bearer <jwt>` — zero Freighter prompts.
 *   3. On 401, call `clearSessionToken()` and retry once.
 *
 * Cache scope: sessionStorage (cleared on tab close). JWT expiry honored
 * with a 30-second safety margin to avoid mid-request expiry.
 */

import { signTransaction, getPublicKey } from "./wallet";

const MARKETPLACE_API = process.env.NEXT_PUBLIC_MARKETPLACE_URL || "http://localhost:5050";

const SESSION_KEY = "wallet_session_jwt_v1";
const SESSION_EXP_KEY = "wallet_session_jwt_v1_exp";
const EXPIRY_SAFETY_MARGIN_SECONDS = 30;

function getCachedToken(): string | null {
  if (typeof window === "undefined") return null;
  const token = sessionStorage.getItem(SESSION_KEY);
  const expStr = sessionStorage.getItem(SESSION_EXP_KEY);
  if (!token || !expStr) return null;
  const expEpoch = parseInt(expStr, 10);
  if (!Number.isFinite(expEpoch)) {
    clearSessionToken();
    return null;
  }
  // Honor safety margin: treat as expired if within N seconds of real expiry
  if (Date.now() / 1000 > expEpoch - EXPIRY_SAFETY_MARGIN_SECONDS) {
    clearSessionToken();
    return null;
  }
  return token;
}

function setCachedToken(token: string, expiresInSeconds: number): void {
  if (typeof window === "undefined") return;
  const expEpoch = Math.floor(Date.now() / 1000) + expiresInSeconds;
  sessionStorage.setItem(SESSION_KEY, token);
  sessionStorage.setItem(SESSION_EXP_KEY, String(expEpoch));
}

/**
 * Clear the cached session JWT. Call this on 401 from any protected
 * endpoint to force re-exchange on the next request.
 */
export function clearSessionToken(): void {
  if (typeof window === "undefined") return;
  sessionStorage.removeItem(SESSION_KEY);
  sessionStorage.removeItem(SESSION_EXP_KEY);
}

// Inflight exchange dedupe: if N concurrent calls hit a cold cache, they
// all await the same Freighter prompt instead of triggering N prompts.
let _inflightExchange: Promise<string> | null = null;

async function exchangeChallengeForToken(): Promise<string> {
  if (_inflightExchange) return _inflightExchange;

  _inflightExchange = (async () => {
    try {
      const publicKey = await getPublicKey();

      // 1. Get challenge transaction
      const challengeRes = await fetch(
        `${MARKETPLACE_API}/api/auth/challenge-tx?public_key=${publicKey}`,
      );
      if (!challengeRes.ok) {
        throw new Error(`Auth challenge fetch failed: ${challengeRes.status} ${await challengeRes.text()}`);
      }
      const { challenge, unsigned_xdr } = await challengeRes.json();

      // 2. Sign with Freighter — this is the ONE prompt per session
      const signedXdr = await signTransaction(unsigned_xdr);

      // 3. Exchange signed challenge for a JWT
      const sessionRes = await fetch(`${MARKETPLACE_API}/api/auth/session`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ publicKey, challenge, signedXdr }),
      });
      if (!sessionRes.ok) {
        throw new Error(`Session exchange failed: ${sessionRes.status} ${await sessionRes.text()}`);
      }
      const { token, expires_in } = await sessionRes.json();
      if (typeof token !== "string" || typeof expires_in !== "number") {
        throw new Error("Session endpoint returned malformed response");
      }

      setCachedToken(token, expires_in);
      return token;
    } finally {
      _inflightExchange = null;
    }
  })();

  return _inflightExchange;
}

/**
 * Returns the Authorization header value for protected marketplace API
 * calls. Format: `Bearer <jwt>`. Cached in sessionStorage for ~1 hour;
 * subsequent calls within the window do NOT prompt Freighter.
 *
 * Callers: `headers: { Authorization: await getWalletAuthHeader() }`
 *
 * On a 401 response, the caller should call `clearSessionToken()` and
 * retry the request once — the cached JWT may have been invalidated by
 * a server restart (ephemeral SESSION_TOKEN_SECRET in local dev) or
 * clock skew. A subsequent `getWalletAuthHeader()` will trigger a new
 * Freighter prompt and exchange.
 */
export async function getWalletAuthHeader(): Promise<string> {
  const cached = getCachedToken();
  if (cached) return `Bearer ${cached}`;
  const fresh = await exchangeChallengeForToken();
  return `Bearer ${fresh}`;
}

/**
 * Fetch wrapper that handles wallet auth + transparent 401 retry.
 *
 * Behavior:
 *   1. Attaches Authorization: Bearer <jwt> from cache (or one-time
 *      challenge exchange if cache is cold).
 *   2. If response is 401, clears the cached JWT, re-exchanges (which
 *      triggers ONE Freighter prompt), and retries the request once.
 *   3. Second 401 (or any other error) propagates to the caller.
 *
 * Use this for all protected marketplace endpoints. Callers don't have
 * to think about session caching or token refresh — the wrapper handles it.
 *
 * Concurrent requests share one in-flight exchange (see _inflightExchange
 * above) so 10 parallel calls with cold cache = 1 Freighter prompt, not 10.
 */
export async function walletAuthFetch(
  input: RequestInfo | URL,
  init: RequestInit = {},
): Promise<Response> {
  const attempt = async (): Promise<Response> => {
    const header = await getWalletAuthHeader();
    const headers = new Headers(init.headers);
    headers.set("Authorization", header);
    return fetch(input, { ...init, headers });
  };

  const response = await attempt();
  if (response.status !== 401) return response;

  // 401 → cached JWT rejected (expired, server restarted, secret rotated).
  // Clear cache, re-exchange (Freighter prompt), retry once.
  clearSessionToken();
  return attempt();
}
