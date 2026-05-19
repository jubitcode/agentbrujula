export interface Skill {
  id: string;
  name: string;
  description: string;
  tags?: string[];
  examples?: string[];
}

export interface AgentCard {
  name: string;
  description: string;
  url: string;
  version: string;
  skills: Skill[];
  capabilities?: {
    streaming?: boolean;
    pushNotifications?: boolean;
  };
}

export interface TaskStatus {
  state: "submitted" | "working" | "input-required" | "completed" | "failed" | "canceled";
  message?: { role: string; parts: { kind: string; text?: string }[] };
  timestamp: string;
}

export interface Artifact {
  id: string;
  name?: string;
  parts: { kind: string; text?: string; data?: unknown }[];
}

export interface Task {
  id: string;
  contextId: string;
  status: TaskStatus;
  artifacts?: Artifact[];
  history?: { role: string; parts: { kind: string; text?: string }[] }[];
  metadata?: Record<string, unknown>;
}

export interface StepResult {
  step: string;
  status: "pass" | "fail";
  duration_ms: number;
  error?: string;
  details?: unknown;
}

export interface DeployResult {
  id: string;
  agent: string;
  version: number;
  status: "success" | "failed";
  steps: StepResult[];
  failed_step?: string;
  error?: string;
  total_duration_ms: number;
}

export interface ExecuteResult {
  result?: unknown;
  error?: string;
  durationMs: number;
  requestId: string;
}

export interface SSEEvent {
  event: string;
  data: string;
}
