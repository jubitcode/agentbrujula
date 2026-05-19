"use client";

import Markdown from "react-markdown";
import type { Task } from "@/lib/types";

interface TaskResultProps {
  task: Task;
}

export function TaskResult({ task }: TaskResultProps) {
  const artifact = task.artifacts?.[0];
  const resultText = artifact?.parts?.[0]?.text || "";

  // Try to parse as JSON (agent responses wrap results)
  let displayText = resultText;
  let sources: { title: string; url: string }[] = [];

  try {
    const parsed = JSON.parse(resultText);
    if (parsed.content) {
      displayText = parsed.content;
    } else if (parsed.summary) {
      displayText = parsed.summary;
    } else if (parsed.analysis) {
      displayText = parsed.analysis;
    } else if (parsed.greeting) {
      displayText = parsed.greeting;
    } else if (parsed.findings) {
      const summary = parsed.findings.find(
        (f: Record<string, string>) => f.type === "research_summary"
      );
      displayText = summary?.content || JSON.stringify(parsed, null, 2);
    } else {
      displayText = JSON.stringify(parsed, null, 2);
    }
    if (parsed.sources) {
      sources = parsed.sources;
    }
  } catch {
    // Not JSON — use as-is (markdown)
  }

  return (
    <div>
      <div className="border rounded-xl p-6" style={{ background: "var(--surface)", borderColor: "var(--border)" }}>
        <div className="prose prose-sm max-w-none [&>h1]:text-lg [&>h2]:text-base [&>h3]:text-sm [&>p]:leading-relaxed [&>ul]:space-y-1 [&>ol]:space-y-1" style={{ color: "var(--text)" }}>
          <Markdown>{displayText}</Markdown>
        </div>
      </div>

      {sources.length > 0 && (
        <div className="mt-3 text-xs" style={{ color: "var(--dim)" }}>
          <span className="font-medium">Sources: </span>
          {sources.map((s, i) => (
            <span key={i}>
              {i > 0 && " · "}
              {s.url ? (
                <a
                  href={s.url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="underline"
                  style={{ color: "var(--muted)" }}
                >
                  {s.title || s.url}
                </a>
              ) : (
                s.title
              )}
            </span>
          ))}
        </div>
      )}
    </div>
  );
}
