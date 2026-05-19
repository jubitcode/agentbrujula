"use client";

interface Step {
  step: string;
  detail?: string;
  status?: "active" | "done" | "error";
}

interface TaskProgressProps {
  steps: Step[];
}

export function TaskProgress({ steps }: TaskProgressProps) {
  return (
    <div className="space-y-2">
      {steps.map((s, i) => (
        <div key={i} className="flex items-center gap-3 text-sm">
          <span className="w-4 text-center">
            {s.status === "done" && <span style={{ color: "var(--green)" }}>✓</span>}
            {s.status === "active" && <span className="animate-pulse" style={{ color: "var(--accent)" }}>●</span>}
            {s.status === "error" && <span style={{ color: "#ef4444" }}>✗</span>}
            {!s.status && <span style={{ color: "var(--dim)" }}>○</span>}
          </span>
          <span style={{ color: "var(--text)" }}>{s.step}</span>
          {s.detail && <span className="ml-auto" style={{ color: "var(--dim)" }}>{s.detail}</span>}
        </div>
      ))}
    </div>
  );
}
