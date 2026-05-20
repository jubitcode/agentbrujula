import type { AgentCard, Task, DeployResult, ExecuteResult } from "./types";

const API = process.env.NEXT_PUBLIC_API_URL || "http://localhost:5050";

export function authHeaders(extra?: Record<string, string>): Record<string, string> {
  return { ...extra };
}

async function fetchJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API}${path}`, init);
  if (res.status === 401) {
    throw new Error("Unauthorized — connect your wallet");
  }
  return res.json();
}

export async function getAgentCard(): Promise<AgentCard> {
  return fetchJSON("/.well-known/agent-card.json");
}

export async function getRecentTasks(limit = 20): Promise<{ tasks: Task[]; count: number }> {
  return fetchJSON(`/v1/tasks?limit=${limit}`);
}

export async function getTask(taskId: string): Promise<Task> {
  const res = await fetchJSON<{ result: Task }>("/a2a", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      jsonrpc: "2.0",
      id: 1,
      method: "tasks/get",
      params: { id: taskId },
    }),
  });
  return res.result;
}

export async function sendMessage(
  text: string,
  options?: { skillId?: string; contextId?: string }
): Promise<Task> {
  const res = await fetchJSON<{ result: Task; error?: { message: string } }>("/a2a", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      jsonrpc: "2.0",
      id: 1,
      method: "message/send",
      params: {
        message: {
          role: "user",
          parts: [{ kind: "text", text }],
        },
        metadata: options?.skillId ? { skillId: options.skillId } : undefined,
        contextId: options?.contextId,
      },
    }),
  });
  if (res.error) throw new Error(res.error.message);
  return res.result;
}

export async function executeTool(tool: string, args: Record<string, unknown>): Promise<ExecuteResult> {
  return fetchJSON("/v1/execute", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ tool, arguments: args }),
  });
}

export async function deployAgent(manifest: File, extraFiles: File[] = []): Promise<DeployResult> {
  const form = new FormData();
  form.append("manifest", manifest);
  for (const f of extraFiles) {
    form.append("files", f, f.name);
  }
  const res = await fetch(`${API}/v1/marketplace/deploy`, {
    method: "POST",
    body: form,
  });
  return res.json();
}

export async function listAgents(): Promise<{ tools: number; agents: number }> {
  return fetchJSON("/v1/marketplace/agents");
}

export async function undeployAgent(name: string): Promise<{ status: string }> {
  return fetchJSON(`/v1/marketplace/agents/${name}`, { method: "DELETE" });
}
