/**
 * Tests for marketplace.ts — the FastAPI client for x402 payment flow.
 * Mocks fetch globally — no real HTTP calls.
 */

import {
  requestDebate,
  requestA2A,
  buildGenericPayment,
  buildPayment,
  submitPayment,
  type DebateResult,
  type A2AExecutionResult,
} from "./marketplace";

const mockFetch = jest.fn();
global.fetch = mockFetch;

beforeEach(() => {
  mockFetch.mockReset();
});

describe("requestDebate", () => {
  test("returns 402 with payment info when no signature", async () => {
    const paymentInfo = {
      estimated_cost: "0.25",
      budget_ceiling: "0.375",
      network: "stellar:testnet",
      payTo: "GABC...",
      token: "USDC",
      facilitatorUrl: "https://x402.org/facilitator",
    };
    const encoded = btoa(JSON.stringify(paymentInfo));

    mockFetch.mockResolvedValueOnce({
      status: 402,
      headers: new Headers({ "PAYMENT-REQUIRED": encoded }),
    });

    const result = await requestDebate("Should we use Kafka?");

    expect(result.status).toBe(402);
    expect(result.paymentInfo).toBeDefined();
    expect(result.paymentInfo!.estimated_cost).toBe("0.25");
    expect(result.paymentInfo!.payTo).toBe("GABC...");
    expect(result.result).toBeUndefined();
  });

  test("returns debate result with valid payment signature", async () => {
    const debateResult: DebateResult = {
      question: "Kafka?",
      rounds: 3,
      transcript: [{ agent: "advocate", round: 1, content: "Use Kafka" }],
      decision: "Use Kafka",
      adr: "# ADR-001",
      concession_detected: false,
      cost: { total_tokens_in: 100, total_tokens_out: 50, estimated_cost_usd: 0.01 },
    };

    mockFetch.mockResolvedValueOnce({
      status: 200,
      ok: true,
      json: async () => debateResult,
    });

    const result = await requestDebate("Kafka?", "payment-sig-abc");

    expect(result.status).toBe(200);
    expect(result.result).toBeDefined();
    expect(result.result!.question).toBe("Kafka?");
    expect(result.result!.rounds).toBe(3);
  });

  test("sends PAYMENT-SIGNATURE header when provided", async () => {
    mockFetch.mockResolvedValueOnce({
      status: 200,
      ok: true,
      json: async () => ({}),
    });

    await requestDebate("test", "my-payment-sig");

    const [, init] = mockFetch.mock.calls[0];
    expect(init.headers["PAYMENT-SIGNATURE"]).toBe("my-payment-sig");
  });

  test("does NOT send PAYMENT-SIGNATURE when not provided", async () => {
    mockFetch.mockResolvedValueOnce({
      status: 402,
      headers: new Headers({ "PAYMENT-REQUIRED": btoa("{}") }),
    });

    await requestDebate("test");

    const [, init] = mockFetch.mock.calls[0];
    expect(init.headers["PAYMENT-SIGNATURE"]).toBeUndefined();
  });

  test("throws on non-402 error", async () => {
    mockFetch.mockResolvedValueOnce({
      status: 500,
      ok: false,
      text: async () => "Internal Server Error",
    });

    await expect(requestDebate("test", "sig")).rejects.toThrow("Request failed: 500");
  });
});

describe("requestA2A", () => {
  test("returns 402 with cost estimate", async () => {
    const paymentInfo = { estimated_cost: "0.105", payTo: "GXYZ", network: "stellar:testnet", token: "USDC", facilitatorUrl: "" };
    mockFetch.mockResolvedValueOnce({
      status: 402,
      headers: new Headers({ "PAYMENT-REQUIRED": btoa(JSON.stringify(paymentInfo)) }),
    });

    const result = await requestA2A("Research AI trends");
    expect(result.status).toBe(402);
    expect(result.paymentInfo!.estimated_cost).toBe("0.105");
  });

  test("returns formatted output with payment", async () => {
    const a2aResult: A2AExecutionResult = {
      output: "## AI Trends\n\nKey findings...",
      status: "completed",
      task_id: "task-123",
      context_id: "ctx-456",
    };

    mockFetch.mockResolvedValueOnce({
      status: 200,
      ok: true,
      json: async () => a2aResult,
    });

    const result = await requestA2A("Research AI", "payment-sig");
    expect(result.status).toBe(200);
    expect(result.result!.output).toContain("AI Trends");
    expect(result.result!.status).toBe("completed");
  });
});

describe("buildGenericPayment", () => {
  test("returns task_id and unsigned_xdr", async () => {
    mockFetch.mockResolvedValueOnce({
      status: 200,
      ok: true,
      json: async () => ({
        unsigned_xdr: "AAAA...",
        task_id: "x402-abc123",
        deadline: 9999999999,
        amount_usdc: "0.25",
        destination: "GESCROW...",
        token: "USDC",
      }),
    });

    const result = await buildGenericPayment("0.25", "GUSER...", 300);

    expect(result.unsigned_xdr).toBe("AAAA...");
    expect(result.task_id).toBe("x402-abc123");
    expect(result.deadline).toBe(9999999999);
  });

  test("sends deadline_seconds in request body", async () => {
    mockFetch.mockResolvedValueOnce({
      status: 200,
      ok: true,
      json: async () => ({ unsigned_xdr: "X", task_id: "t", deadline: 0 }),
    });

    await buildGenericPayment("0.10", "GUSER...", 600);

    const [, init] = mockFetch.mock.calls[0];
    const body = JSON.parse(init.body);
    expect(body.deadline_seconds).toBe(600);
    expect(body.amount_usdc).toBe("0.10");
    expect(body.user_public_key).toBe("GUSER...");
  });

  test("throws on build failure", async () => {
    mockFetch.mockResolvedValueOnce({
      status: 500,
      ok: false,
      text: async () => "Simulation failed",
    });

    await expect(buildGenericPayment("0.10", "G...", 300)).rejects.toThrow("Build payment failed");
  });
});

describe("buildPayment (agent-specific)", () => {
  test("returns task_id from server", async () => {
    mockFetch.mockResolvedValueOnce({
      status: 200,
      ok: true,
      json: async () => ({
        unsigned_xdr: "XDR_HERE",
        task_id: "hire-research-agent-12345",
        deadline: 9999999999,
        amount_usdc: "0.45",
        destination: "GESCROW...",
        token: "USDC",
      }),
    });

    const result = await buildPayment("research-agent", "0.45", "GUSER...");
    expect(result.task_id).toBe("hire-research-agent-12345");
    expect(result.unsigned_xdr).toBe("XDR_HERE");
  });
});

describe("submitPayment", () => {
  test("sends task_id in request body", async () => {
    mockFetch.mockResolvedValueOnce({
      status: 200,
      ok: true,
      json: async () => ({ task_id: "hire-research-agent-12345", escrow_tx: "tx_abc", deadline_unix: 9999999 }),
    });

    await submitPayment("SIGNED_XDR", "research-agent", "Research AI", 100, "hire-research-agent-12345");

    const [, init] = mockFetch.mock.calls[0];
    const body = JSON.parse(init.body);
    expect(body.task_id).toBe("hire-research-agent-12345");
  });

  test("sends undefined task_id when not provided", async () => {
    mockFetch.mockResolvedValueOnce({
      status: 200,
      ok: true,
      json: async () => ({ task_id: "auto-generated", escrow_tx: "tx", deadline_unix: 0 }),
    });

    await submitPayment("XDR", "agent", "task", 10);

    const [, init] = mockFetch.mock.calls[0];
    const body = JSON.parse(init.body);
    expect(body.task_id).toBeUndefined();
  });
});

describe("single-agent payment flow: task_id consistency", () => {
  test("task_id from buildPayment flows through to submitPayment", async () => {
    // Step 1: buildPayment returns task_id
    mockFetch.mockResolvedValueOnce({
      status: 200,
      ok: true,
      json: async () => ({
        unsigned_xdr: "XDR",
        task_id: "hire-research-agent-1775278051",
        deadline: 1775278351,
        amount_usdc: "0.45",
        destination: "GESCROW...",
        token: "USDC",
      }),
    });

    const tx = await buildPayment("research-agent", "0.45", "GUSER...");
    expect(tx.task_id).toBe("hire-research-agent-1775278051");

    // Step 2: submitPayment sends the SAME task_id
    mockFetch.mockResolvedValueOnce({
      status: 200,
      ok: true,
      json: async () => ({ task_id: "hire-research-agent-1775278051", escrow_tx: "tx_hash", deadline_unix: 1775278351 }),
    });

    await submitPayment("SIGNED_XDR", "research-agent", "Research AI", 100, tx.task_id);

    // Verify the task_id sent to submit-payment matches build-payment
    const [, submitInit] = mockFetch.mock.calls[1];
    const submitBody = JSON.parse(submitInit.body);
    expect(submitBody.task_id).toBe("hire-research-agent-1775278051");
    expect(submitBody.task_id).toBe(tx.task_id); // MUST match
  });
});

describe("x402 payment flow integration", () => {
  test("full flow: request -> 402 -> build payment -> get task_id -> retry with sig -> result", async () => {
    // Step 1: First request returns 402
    const paymentInfo = { estimated_cost: "0.25", payTo: "GESCROW", network: "stellar:testnet", token: "USDC", facilitatorUrl: "" };
    mockFetch.mockResolvedValueOnce({
      status: 402,
      headers: new Headers({ "PAYMENT-REQUIRED": btoa(JSON.stringify(paymentInfo)) }),
    });

    const step1 = await requestDebate("PostgreSQL vs MongoDB?");
    expect(step1.status).toBe(402);
    expect(step1.paymentInfo!.estimated_cost).toBe("0.25");

    // Step 2: Build payment tx
    mockFetch.mockResolvedValueOnce({
      status: 200,
      ok: true,
      json: async () => ({
        unsigned_xdr: "UNSIGNED_XDR_HERE",
        task_id: "x402-payment123",
        deadline: Date.now() / 1000 + 300,
        amount_usdc: "0.375",
      }),
    });

    const tx = await buildGenericPayment("0.375", "GUSER...", 300);
    expect(tx.task_id).toBe("x402-payment123");

    // Step 3: User signs and submits (mocked)
    const txHash = "fake_stellar_tx_hash_abc123";
    const paymentSig = btoa(JSON.stringify({ txHash, task_id: tx.task_id, success: true }));

    // Step 4: Retry debate with payment signature
    mockFetch.mockResolvedValueOnce({
      status: 200,
      ok: true,
      json: async () => ({
        question: "PostgreSQL vs MongoDB?",
        rounds: 3,
        transcript: [],
        decision: "Use PostgreSQL",
        adr: "# ADR",
        concession_detected: false,
        cost: { total_tokens_in: 500, total_tokens_out: 300, estimated_cost_usd: 0.05 },
      }),
    });

    const step4 = await requestDebate("PostgreSQL vs MongoDB?", paymentSig);
    expect(step4.status).toBe(200);
    expect(step4.result!.decision).toBe("Use PostgreSQL");

    // Verify task_id is in the payment signature
    const sigPayload = JSON.parse(atob(paymentSig));
    expect(sigPayload.task_id).toBe("x402-payment123");
  });
});
