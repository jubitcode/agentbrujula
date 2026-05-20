import { authHeaders } from "./api";

const API = process.env.NEXT_PUBLIC_API_URL || "http://localhost:5050";

export interface StreamEvent {
  event: string;
  data: Record<string, unknown>;
}

export function streamTask(
  text: string,
  options: { skillId?: string; contextId?: string },
  onEvent: (event: StreamEvent) => void,
  onDone: () => void,
  onError: (error: string) => void
): AbortController {
  const controller = new AbortController();

  const headers = authHeaders({ "Content-Type": "application/json" });

  fetch(`${API}/a2a/stream`, {
    method: "POST",
    headers,
    body: JSON.stringify({
      message: { role: "user", parts: [{ kind: "text", text }] },
      metadata: options.skillId ? { skillId: options.skillId } : undefined,
      contextId: options.contextId,
    }),
    signal: controller.signal,
  })
    .then((res) => {
      if (!res.ok || !res.body) {
        onError(`HTTP ${res.status}`);
        return;
      }

      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "";

      function read() {
        reader.read().then(({ done, value }) => {
          if (done) {
            onDone();
            return;
          }

          buffer += decoder.decode(value, { stream: true });
          const lines = buffer.split("\n");
          buffer = lines.pop() || "";

          let currentEvent = "message";
          for (const line of lines) {
            if (line.startsWith("event: ")) {
              currentEvent = line.slice(7).trim();
            } else if (line.startsWith("data: ")) {
              try {
                const data = JSON.parse(line.slice(6));
                onEvent({ event: currentEvent, data });
              } catch {
                // ignore parse errors
              }
            }
          }

          read();
        });
      }

      read();
    })
    .catch((err) => {
      if (err.name !== "AbortError") {
        onError(err.message);
      }
    });

  return controller;
}
