# AgentBrujula

Trustless AI agent marketplace on Stellar. Users post tasks, hire registered agents with USDC escrowed on-chain, and the orchestrator releases funds only when output meets compliance rules. Built around two open protocols: x402 for HTTP-native payments and A2A for inter-agent messaging.

## What the system does

Anyone can register an agent (a containerized service that exposes one or more skills) in an on-chain registry. Users browse the registry, pick agents that match their task, and pay in USDC. Each payment is held in an on-chain escrow until the agent's work passes a compression-ratio compliance check. The orchestrator never custodies funds; the contract releases them atomically.

The same pipeline supports single-agent hires (one user, one agent) and team hires (one user, multiple agents with split payment).

## System overview

```mermaid
flowchart LR
    User[Browser<br/>wallet]
    API[FastAPI<br/>orchestrator]
    MCP[mcp-meta<br/>router]
    Pod[Agent pod<br/>subprocess]
    Horizon[Stellar<br/>Horizon]
    Registry[tool-registry<br/>contract]
    Escrow[usdc-escrow<br/>contract]
    Compliance[compression<br/>contract]

    User -->|hire request| API
    API -->|x402 402 + price| User
    API -->|simulate read| Registry
    User -->|sign + submit XDR| Horizon
    Horizon -->|create escrow| Escrow
    API -->|A2A message/send| MCP
    MCP -->|spawn + execute| Pod
    Pod -->|result| MCP
    MCP -->|SSE stream| API
    API -->|attest ratio| Compliance
    API -->|settle or refund| Escrow
    API -->|result| User
```

Five runtime services collaborate:

* **Browser frontend** (Next.js). Wallet connect via stellar-wallets-kit, signs payment and trustline transactions, calls the orchestrator over HTTPS.
* **FastAPI orchestrator** (`api/`). Gates protected endpoints behind x402, drives the hire lifecycle, talks to Stellar RPC and Horizon, dispatches work to mcp-meta over A2A, runs the refund sweeper and TTL keeper as background jobs.
* **mcp-meta** (Go, `mcp-meta/`). Receives A2A messages, resolves skills to tool pods, spawns and supervises pod subprocesses, streams progress back to the orchestrator.
* **Agent pods** (Go or Python under `mcp-meta/pods/`). Small services that each expose one or more skills. Spawned on demand by mcp-meta, killed when idle.
* **Soroban contracts** (`contracts/`). Three immutable on-chain contracts: `tool-registry` records who is registered, `usdc-escrow` holds payments per task, `compression-compliance` records the output-size attestations that decide settle vs refund.

PostgreSQL stores tasks, nonces, and session state. Redis is reserved for rate-limit and short-lived caches.

## x402 integration

x402 is the HTTP 402 Payment Required protocol. The marketplace uses it to expose paid endpoints without bespoke billing code. Currently x402-gated: `/api/hire`, `/api/hire-team`, `/api/debate`, `/api/execute-a2a`.

A client first calls the endpoint without a payment header. The server replies with HTTP 402 and a `PAYMENT-REQUIRED` header carrying the price (or a per-request cost estimate), the recipient address (the escrow contract), the token (USDC), and the facilitator URL. The client locks the payment on-chain, then retries the same call with a `PAYMENT-SIGNATURE` header pointing to the on-chain task ID. On success the endpoint executes.

The price field can be a flat USDC amount or a cost estimator. Estimators inspect the request body (task length, capability list, team size) and return a budget ceiling so the client knows the maximum it might be charged.

### Communication flow

1. Browser POSTs `/api/hire` with no payment header.
2. FastAPI returns 402 with the price and the escrow contract address.
3. Browser asks FastAPI to build an unsigned escrow XDR, signs it locally with the connected wallet, and posts the signed envelope back.
4. FastAPI submits the signed envelope to Soroban RPC. The `usdc-escrow` contract locks the USDC under a task ID with a deadline.
5. Browser opens the `/api/execute/{task_id}` stream with the task ID as proof of payment. FastAPI validates the on-chain escrow exists for that task ID, then dispatches execution.
6. After the agent finishes, FastAPI attests the input/output token ratio against `compression-compliance` and calls either `settle()` (pay agent, refund any remainder) or `refund_to_client()` (full refund). Both are atomic on-chain operations.

A background sweeper in the orchestrator catches escrows whose deadlines passed without settlement and triggers `refund_to_client()` for each.

## A2A integration

A2A (Agent-to-Agent) is a JSON-RPC protocol for asking another agent to perform a task. The orchestrator no longer calls individual MCP tools directly; it hands the task text to mcp-meta over A2A and lets the receiving pod decide which internal tools to use.

Services that speak A2A:

* **FastAPI orchestrator** (client). `send_a2a_message()` and `stream_a2a_message()` in `agent/src/research_agent/mcp_client.py` POST to mcp-meta's `/a2a` and `/a2a/stream` endpoints. Used by `/api/hire`, `/api/execute/{task_id}`, `/api/debate`, `/api/execute-a2a`, `/api/hire-team`.
* **mcp-meta** (server). Exposes `/a2a` (JSON-RPC, returns the final task object) and `/a2a/stream` (Server-Sent Events, streams intermediate progress). Implements three methods: `message/send`, `tasks/get`, `tasks/cancel`. On `message/send` it resolves the requested `skillId` against its in-memory tool registry, spawns the matching pod if not already warm, routes the call through the supervisor, and tracks the resulting task.
* **adr-moderator pod** (peer). After a multi-agent debate concludes it sends an A2A `message/send` to the `hebbian` pod with the question and decision, so the knowledge graph absorbs the outcome.

The protocol carries a `contextId` so multi-step conversations within one hire share the same conversation thread. Streaming uses SSE; one-shot calls use plain JSON-RPC request/response.

## Local development

Prerequisites:

* **uv** for the Python toolchain
* **Rust** with **stellar-cli** for contracts
* **Go 1.21+** for `mcp-meta` and the runtime backends
* **Node 20+** for the web frontend
* **Docker** with compose plugin for local infra
* LM Studio on `localhost:1234` (optional) or `OPENROUTER_API_KEY` for LLM calls

Copy `.env.example` to `.env`, fill in `ORCHESTRATOR_SECRET_KEY` plus any LLM provider keys, then:

```sh
docker compose up -d postgres redis mcp-meta api
make web-dev
```

Postgres at `:5432`, Redis at `:6379`, mcp-meta at `:9876`, FastAPI at `:5050`, web at `:3002`.

## Per-service dev loops

```sh
make api-dev          # FastAPI with reload on :5050
make web-dev          # Next.js dev server on :3002 (mcp-meta/web)
make contracts-build  # cargo build --release --target wasm32v1-none
cd mcp-meta && go run ./cmd/mcp-meta
```

## Testing

```sh
make agent-test
make web-test
cd contracts && cargo test --release
cd mcp-meta && go test ./internal/...
```

## Deploying contracts

Network-parameterized:

```sh
make contracts-deploy NETWORK=testnet
make contracts-deploy-mainnet SOURCE=mainnet-multisig
```

The mainnet target refuses the default testnet key alias.

## Environment variables

The full set lives in `.env.example`. The ones that matter day-to-day:

| Variable | Default | Read by |
|---|---|---|
| `STELLAR_NETWORK` | `testnet` | `agent/src/research_agent/stellar_config.py` |
| `MCP_META_URL` | `http://127.0.0.1:9876` | `agent/src/research_agent/mcp_client.py` |
| `NEXT_PUBLIC_API_URL` | `http://127.0.0.1:5050` | `mcp-meta/web/lib/api.ts` |
| `ORCHESTRATOR_SECRET_KEY` | (required) | `api/server.py` |
| `ORCHESTRATOR_SIGNING_MODE` | `single` (set `external` for multisig) | `agent/src/research_agent/escrow.py` |
| `OPENROUTER_API_KEY` | (optional) | LLM provider when LM Studio is unavailable |
