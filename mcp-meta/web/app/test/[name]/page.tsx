"use client";

import { useState, useEffect, use } from "react";
import Markdown from "react-markdown";
import { InputBar } from "@/components/input-bar";
import { executeTool, authHeaders } from "@/lib/api";

const API = process.env.NEXT_PUBLIC_API_URL || "http://localhost:9876";

interface AgentInfo {
  name: string;
  language: string;
  tools: number;
  skills: number;
}

export default function TestAgentPage({ params }: { params: Promise<{ name: string }> }) {
  const { name } = use(params);
  const [agent, setAgent] = useState<AgentInfo | null>(null);
  const [tools, setTools] = useState<string[]>([]);
  const [selectedTool, setSelectedTool] = useState("");
  const [input, setInput] = useState("");
  const [result, setResult] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    // Fetch agent info
    fetch(`${API}/v1/marketplace/agents/${name}`, { headers: authHeaders() })
      .then((r) => r.json())
      .then((data) => {
        if (data.error) {
          setError(data.error);
        } else {
          setAgent(data);
        }
      })
      .catch((err) => setError(err.message));

    // Fetch tools list to find this agent's tools
    fetch(`${API}/v1/tools`, { headers: authHeaders() })
      .then((r) => r.json())
      .then((data) => {
        const allTools = data.tools || [];
        const agentTools = allTools
          .map((t: { name: string }) => t.name)
          .filter((t: string) => t.startsWith(name + ".") && !t.endsWith(".ping"));
        if (agentTools.length > 0) {
          setTools(agentTools);
          setSelectedTool(agentTools[0]);
        } else {
          // Fallback: construct tool names from agent info
          fetch(`${API}/v1/marketplace/agents/${name}`, { headers: authHeaders() })
            .then((r) => r.json())
            .then(() => {
              // Use common tool patterns — first non-ping tool
              const guessed = [`${name}.explain`, `${name}.search`, `${name}.run`, `${name}.process`];
              setTools(guessed.slice(0, 1));
              setSelectedTool(guessed[0]);
            })
            .catch(() => {
              // Last resort — let user type the tool name
              setSelectedTool(`${name}.ping`);
            });
        }
      })
      .catch(() => {
        // API down — guess first tool
        setSelectedTool(`${name}.explain`);
        setTools([`${name}.explain`]);
      });
  }, [name]);

  const handleSubmit = async (text: string) => {
    const tool = selectedTool || `${name}.ping`;
    if (!tool) return;
    setLoading(true);
    setError(null);
    setResult(null);
    setInput(text);

    try {
      const res = await executeTool(tool, { message: text, code: text, query: text });
      if (res.error) {
        setError(res.error);
      } else {
        setResult(JSON.stringify(res.result, null, 2));
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Call failed");
    } finally {
      setLoading(false);
    }
  };

  // Try to extract readable text from result
  let displayText = result || "";
  try {
    const parsed = JSON.parse(displayText);
    displayText =
      parsed.explanation ||
      parsed.summary ||
      parsed.content ||
      parsed.analysis ||
      parsed.argument ||
      parsed.greeting ||
      parsed.message ||
      JSON.stringify(parsed, null, 2);
  } catch {
    // use as-is
  }

  return (
    <div className="max-w-3xl mx-auto">
      <a href="/deploy" className="text-sm text-neutral-400 hover:text-neutral-700 mb-4 block">
        ← Back to deploy
      </a>

      <h1 className="text-xl font-medium text-neutral-800 mb-1">Test: {name}</h1>
      {agent && (
        <p className="text-sm text-neutral-400 mb-6">
          {agent.language} · {agent.tools} tools · {agent.skills} skills
        </p>
      )}

      {tools.length > 1 && (
        <div className="flex gap-2 mb-4">
          {tools.map((t) => (
            <button
              key={t}
              onClick={() => setSelectedTool(t)}
              className={`text-xs px-3 py-1 rounded-lg border transition-colors ${
                selectedTool === t
                  ? "bg-neutral-900 text-white border-neutral-900"
                  : "bg-white text-neutral-600 border-neutral-200 hover:border-neutral-400"
              }`}
            >
              {t.split(".").pop()}
            </button>
          ))}
        </div>
      )}

      {selectedTool && (
        <p className="text-xs text-neutral-400 mb-2">
          Calling: <span className="font-mono">{selectedTool}</span>
        </p>
      )}

      <div className="mb-6">
        <InputBar onSubmit={handleSubmit} placeholder={`Send input to ${name}...`} />
      </div>

      {loading && (
        <div className="bg-white border border-neutral-200 rounded-xl p-6 text-center text-neutral-400">
          <span className="animate-pulse">● </span>Calling {selectedTool}...
        </div>
      )}

      {error && (
        <div className="bg-red-50 border border-red-200 rounded-xl p-4 text-sm text-red-700">
          {error}
        </div>
      )}

      {result && (
        <div className="bg-white border border-neutral-200 rounded-xl p-6">
          <div className="prose prose-neutral prose-sm max-w-none break-words">
            <Markdown>{displayText}</Markdown>
          </div>
        </div>
      )}
    </div>
  );
}
