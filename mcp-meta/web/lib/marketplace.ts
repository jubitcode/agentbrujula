/**
 * Client for the FastAPI agent marketplace (x402 payment + hiring).
 * Separate from mcp-meta API — talks to the FastAPI server.
 */

import { walletAuthFetch } from "./wallet-auth";

const MARKETPLACE_API = process.env.NEXT_PUBLIC_MARKETPLACE_URL || "http://localhost:5050";

function authFetchHeaders(extra?: Record<string, string>): Record<string, string> {
  return { "Content-Type": "application/json", ...extra };
}

export interface Agent {
  handle: string;
  public_key: string;
  tier: string;
  capabilities: string[];
  rate_stroops: number;
  rate_xlm: number;
  rep_score: number;
  hebbian_confidence: number | null;
}

export interface DiscoverResponse {
  agents: Agent[];
  registry_contract: string;
  rep_contract: string;
  hebbian_nodes: number;
}

export interface PaymentInfo {
  estimated_cost?: string;
  budget_ceiling?: string;
  rate_breakdown?: { tool: string; rate: string }[];
  price?: string;
  network: string;
  payTo: string;
  token: string;
  facilitatorUrl: string;
}

export interface TeamAgent {
  handle: string;
  capabilities: string[];
  rate_xlm: number;
  rep_score: number;
}

export interface SubTask {
  id: string;
  agent_handle: string;
  action: string;
  depends_on: string[];
}

export interface HireTeamResponse {
  task_id: string;
  team: TeamAgent[];
  sub_tasks: SubTask[];
  execution_plan: string[][];
  estimated_cost_usdc: string;
  budget_ceiling_usdc: string;
  deadline_unix: number;
}

export interface ExecutionEvent {
  type: "log" | "tool_call" | "complete" | "error";
  timestamp: string;
  message: string;
  data?: Record<string, unknown>;
}

export interface SettlementInfo {
  settle_tx: string;
  actual_cost_usdc?: string;
  total_cost_usdc?: string;
  by_agent?: Record<string, number>;
  splits?: { address: string; amount_usdc: string }[];
}

export async function discoverAgents(
  task: string,
  capabilities: string[],
  budget: number,
): Promise<DiscoverResponse> {
  const res = await fetch(`${MARKETPLACE_API}/api/discover`, {
    method: "POST",
    headers: authFetchHeaders(),
    body: JSON.stringify({ task, capabilities, budget_xlm: budget }),
  });
  if (!res.ok) throw new Error(`Discover failed: ${res.status}`);
  return res.json();
}

export async function hireAgent(
  agentHandle: string,
  task: string,
  budget: number,
): Promise<{ status: number; paymentInfo?: PaymentInfo; taskId?: string }> {
  const res = await fetch(`${MARKETPLACE_API}/api/hire`, {
    method: "POST",
    headers: authFetchHeaders(),
    body: JSON.stringify({ agent_handle: agentHandle, task, budget_xlm: budget }),
  });

  if (res.status === 402) {
    const header = res.headers.get("PAYMENT-REQUIRED");
    if (!header) throw new Error("402 without PAYMENT-REQUIRED header");
    const info: PaymentInfo = JSON.parse(atob(header));
    return { status: 402, paymentInfo: info };
  }

  if (!res.ok) throw new Error(`Hire failed: ${res.status}`);
  const data = await res.json();
  return { status: 200, taskId: data.task_id };
}

export interface UnsignedPayment {
  unsigned_xdr: string;
  amount_usdc: string;
  destination: string;
  destination_label?: string;
  token: string;
  network?: string;
  task_id?: string;
  deadline?: number;
}

export async function buildPayment(
  agentHandle: string,
  amountUsdc: string,
  userPublicKey: string,
): Promise<UnsignedPayment> {
  const res = await fetch(`${MARKETPLACE_API}/api/build-payment`, {
    method: "POST",
    headers: authFetchHeaders(),
    body: JSON.stringify({ agent_handle: agentHandle, amount_usdc: amountUsdc, user_public_key: userPublicKey }),
  });
  if (!res.ok) throw new Error(`Build payment failed: ${res.status} ${await res.text()}`);
  return res.json();
}

export async function submitPayment(
  signedXdr: string,
  agentHandle: string,
  task: string,
  budget: number,
  taskId?: string,
  linkTaskId?: string,
): Promise<{ task_id: string; escrow_tx: string; deadline_unix: number }> {
  // Server requires wallet auth on this endpoint. walletAuthFetch
  // attaches the cached Bearer token (or signs once if cold), and
  // transparently retries on 401 (e.g. JWT expired mid-flow).
  const res = await walletAuthFetch(`${MARKETPLACE_API}/api/submit-payment`, {
    method: "POST",
    headers: authFetchHeaders(),
    body: JSON.stringify({ signed_xdr: signedXdr, agent_handle: agentHandle, task, budget_xlm: budget, task_id: taskId, link_task_id: linkTaskId }),
  });
  if (!res.ok) throw new Error(`Submit payment failed: ${res.status} ${await res.text()}`);
  return res.json();
}

export async function hireTeam(
  task: string,
  capabilities: string[],
  budget: number,
  paymentSignature?: string,
): Promise<{ status: number; paymentInfo?: PaymentInfo; data?: HireTeamResponse }> {
  const headers = authFetchHeaders();
  if (paymentSignature) {
    headers["PAYMENT-SIGNATURE"] = paymentSignature;
  }
  const res = await fetch(`${MARKETPLACE_API}/api/hire-team`, {
    method: "POST",
    headers,
    body: JSON.stringify({ task, capabilities, budget_xlm: budget }),
  });

  if (res.status === 402) {
    const header = res.headers.get("PAYMENT-REQUIRED");
    if (!header) throw new Error("402 without PAYMENT-REQUIRED header");
    const info: PaymentInfo = JSON.parse(atob(header));
    return { status: 402, paymentInfo: info };
  }

  if (!res.ok) throw new Error(`Hire team failed: ${res.status} ${await res.text()}`);
  const data: HireTeamResponse = await res.json();
  return { status: 200, data };
}

export function streamExecution(
  taskId: string,
  onEvent: (evt: ExecutionEvent) => void,
  onDone: () => void,
  _walletAuthHeader?: string,
): AbortController {
  const controller = new AbortController();

  // walletAuthFetch handles the Bearer header + 401 retry. The cached JWT
  // is reused if available; a 401 (e.g. expired session) triggers a single
  // re-exchange before the stream begins. After this point, SSE state
  // streams without further auth interaction.
  void (async () => {
    try {
      const res = await walletAuthFetch(
        `${MARKETPLACE_API}/api/execute/${taskId}`,
        { signal: controller.signal, headers: authFetchHeaders() },
      );
      if (!res.ok || !res.body) {
        onEvent({ type: "error", timestamp: "", message: `HTTP ${res.status}` });
        onDone();
        return;
      }
      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "";
      const read = () => {
        reader.read().then(({ done, value }) => {
          if (done) { onDone(); return; }
          buffer += decoder.decode(value, { stream: true });
          const lines = buffer.split("\n");
          buffer = lines.pop() || "";
          for (const line of lines) {
            if (line.startsWith("data: ")) {
              try { onEvent(JSON.parse(line.slice(6))); } catch {}
            }
          }
          read();
        });
      };
      read();
    } catch (err) {
      const e = err as Error;
      if (e.name !== "AbortError") {
        onEvent({ type: "error", timestamp: "", message: e.message });
      }
      onDone();
    }
  })();

  return controller;
}

export function streamTeamExecution(
  taskId: string,
  onEvent: (evt: ExecutionEvent) => void,
  onDone: () => void,
  _walletAuthHeader?: string,
): AbortController {
  const controller = new AbortController();

  void (async () => {
    try {
      const res = await walletAuthFetch(
        `${MARKETPLACE_API}/api/execute-team/${taskId}`,
        { signal: controller.signal, headers: authFetchHeaders() },
      );
      if (!res.ok || !res.body) {
        onEvent({ type: "error", timestamp: "", message: `HTTP ${res.status}` });
        onDone();
        return;
      }
      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "";
      const read = () => {
        reader.read().then(({ done, value }) => {
          if (done) { onDone(); return; }
          buffer += decoder.decode(value, { stream: true });
          const lines = buffer.split("\n");
          buffer = lines.pop() || "";
          for (const line of lines) {
            if (line.startsWith("data: ")) {
              try { onEvent(JSON.parse(line.slice(6))); } catch {}
            }
          }
          read();
        });
      };
      read();
    } catch (err) {
      const e = err as Error;
      if (e.name !== "AbortError") {
        onEvent({ type: "error", timestamp: "", message: e.message });
      }
      onDone();
    }
  })();

  return controller;
}

// walletAuthHeader parameter kept for source compat with existing callers
// in app/hire/page.tsx + app/debate/page.tsx — pages still pre-resolve and
// pass it. Either way the request goes through walletAuthFetch so retry on
// 401 works. The parameter is now ignored; remove it in a follow-up sweep
// once page callers are updated.
export async function getResult(taskId: string, _walletAuthHeader?: string): Promise<Record<string, unknown>> {
  const res = await walletAuthFetch(`${MARKETPLACE_API}/api/result/${taskId}`, {
    headers: authFetchHeaders(),
  });
  if (!res.ok) throw new Error(`Result failed: ${res.status}`);
  return res.json();
}

export async function getTeamResult(taskId: string, _walletAuthHeader?: string): Promise<Record<string, unknown>> {
  const res = await walletAuthFetch(`${MARKETPLACE_API}/api/result-team/${taskId}`, {
    headers: authFetchHeaders(),
  });
  if (!res.ok) throw new Error(`Team result failed: ${res.status}`);
  return res.json();
}

export interface WalletStatus {
  public_key: string;
  has_trustline: boolean;
  usdc_balance: number;
  usdc_balance_display: string;
  trustline_xdr: string | null;
  mint_tx: string | null;
  usdc_contract: string;
}

export async function prepareWallet(publicKey: string): Promise<WalletStatus> {
  // Server requires auth + rate-limits + per-wallet cooldown.
  // walletAuthFetch makes auth invisible: cached Bearer reused, transparent
  // retry on 401 if the session expired.
  const res = await walletAuthFetch(`${MARKETPLACE_API}/api/prepare-wallet`, {
    method: "POST",
    headers: authFetchHeaders(),
    body: JSON.stringify({ public_key: publicKey }),
  });
  if (!res.ok) throw new Error(`Prepare wallet failed: ${res.status} ${await res.text()}`);
  return res.json();
}

export async function getRegistry(): Promise<{ agents: Agent[] }> {
  const res = await fetch(`${MARKETPLACE_API}/api/registry`);
  if (!res.ok) throw new Error(`Registry failed: ${res.status}`);
  return res.json();
}

// --- x402-gated execution endpoints ---

export interface DebateResult {
  question: string;
  rounds: number;
  transcript: { agent: string; round: number; content: string }[];
  decision: string;
  adr: string;
  concession_detected: boolean;
  cost: {
    total_tokens_in: number;
    total_tokens_out: number;
    estimated_cost_usd: number;
  };
  error?: string;
}

export interface A2AExecutionResult {
  output: string;
  status: string;
  task_id: string;
  context_id?: string;
}

/**
 * Request an x402-gated endpoint. Returns 402 with payment info on first call (no signature).
 * On retry with paymentSignature, executes and returns result.
 */
async function x402Fetch<T>(
  path: string,
  body: Record<string, unknown>,
  paymentSignature?: string,
): Promise<{ status: number; paymentInfo?: PaymentInfo; data?: T }> {
  const headers: Record<string, string> = authFetchHeaders();
  if (paymentSignature) {
    headers["PAYMENT-SIGNATURE"] = paymentSignature;
  }

  const res = await fetch(`${MARKETPLACE_API}${path}`, {
    method: "POST",
    headers,
    body: JSON.stringify(body),
  });

  if (res.status === 402) {
    const header = res.headers.get("PAYMENT-REQUIRED");
    if (!header) throw new Error("402 without PAYMENT-REQUIRED header");
    const info: PaymentInfo = JSON.parse(atob(header));
    return { status: 402, paymentInfo: info };
  }

  if (!res.ok) throw new Error(`Request failed: ${res.status} ${await res.text()}`);
  const data: T = await res.json();
  return { status: 200, data };
}

/** Request debate pricing (returns 402), or execute with payment signature. */
export async function requestDebate(
  question: string,
  paymentSignature?: string,
): Promise<{ status: number; paymentInfo?: PaymentInfo; result?: DebateResult }> {
  const res = await x402Fetch<DebateResult>("/api/debate", { question }, paymentSignature);
  return { status: res.status, paymentInfo: res.paymentInfo, result: res.data };
}

/** Request A2A execution pricing (returns 402), or execute with payment signature. */
export async function requestA2A(
  task: string,
  paymentSignature?: string,
  contextId?: string,
): Promise<{ status: number; paymentInfo?: PaymentInfo; result?: A2AExecutionResult }> {
  const res = await x402Fetch<A2AExecutionResult>(
    "/api/execute-a2a",
    { task, context_id: contextId },
    paymentSignature,
  );
  return { status: res.status, paymentInfo: res.paymentInfo, result: res.data };
}

/** Build a generic USDC escrow tx (creates on-chain escrow with deadline). */
export async function buildGenericPayment(
  amountUsdc: string,
  userPublicKey: string,
  deadlineSeconds: number = 300,
): Promise<UnsignedPayment> {
  const res = await fetch(`${MARKETPLACE_API}/api/build-payment-generic`, {
    method: "POST",
    headers: authFetchHeaders(),
    body: JSON.stringify({
      amount_usdc: amountUsdc,
      user_public_key: userPublicKey,
      deadline_seconds: deadlineSeconds,
    }),
  });
  if (!res.ok) throw new Error(`Build payment failed: ${res.status} ${await res.text()}`);
  return res.json();
}
