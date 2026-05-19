"use client";

import { useState, useRef } from "react";
import { deployAgent } from "@/lib/api";
import type { DeployResult } from "@/lib/types";

export default function DeployPage() {
  const [deploying, setDeploying] = useState(false);
  const [result, setResult] = useState<DeployResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [dragOver, setDragOver] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const handleFiles = async (files: FileList) => {
    let manifest: File | null = null;
    const extraFiles: File[] = [];

    for (let i = 0; i < files.length; i++) {
      const f = files[i];
      if (f.name === "manifest.yaml" || f.name === "manifest.yml") {
        manifest = f;
      } else {
        extraFiles.push(f);
      }
    }

    if (!manifest) {
      setError("No manifest.yaml found in dropped files");
      return;
    }

    setDeploying(true);
    setError(null);
    setResult(null);

    try {
      const res = await deployAgent(manifest, extraFiles);
      setResult(res);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Deploy failed");
    } finally {
      setDeploying(false);
    }
  };

  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault();
    setDragOver(false);
    if (e.dataTransfer.files.length > 0) {
      handleFiles(e.dataTransfer.files);
    }
  };

  return (
    <div className="max-w-3xl mx-auto">
      <h1 className="text-xl font-medium mb-2" style={{ color: "var(--text)" }}>Add your own agent</h1>
      <p className="text-sm mb-8" style={{ color: "var(--dim)" }}>Drop your manifest.yaml + code file to deploy</p>

      {/* Drop zone */}
      <div
        onDragOver={(e) => { e.preventDefault(); setDragOver(true); }}
        onDragLeave={() => setDragOver(false)}
        onDrop={handleDrop}
        onClick={() => fileInputRef.current?.click()}
        className="border-2 border-dashed rounded-xl p-12 text-center cursor-pointer transition-colors"
        style={
          dragOver
            ? { borderColor: "var(--accent)", background: "rgba(59,130,246,0.08)" }
            : { borderColor: "var(--border2)" }
        }
      >
        <p className="mb-2" style={{ color: "var(--muted)" }}>
          {deploying ? "Deploying..." : "Drop your agent folder here"}
        </p>
        <p className="text-xs" style={{ color: "var(--dim)" }}>(manifest.yaml + code file)</p>
        <input
          ref={fileInputRef}
          type="file"
          multiple
          className="hidden"
          onChange={(e) => e.target.files && handleFiles(e.target.files)}
        />
      </div>

      {/* Error */}
      {error && (
        <div className="mt-4 rounded-xl p-4 text-sm" style={{ background: "rgba(239,68,68,0.08)", borderColor: "rgba(239,68,68,0.2)", color: "#ef4444" }}>
          {error}
        </div>
      )}

      {/* Deploy result */}
      {result && (
        <div className="mt-6 border rounded-xl p-6" style={{ background: "var(--surface)", borderColor: "var(--border)" }}>
          <div className="text-sm font-medium mb-4" style={{ color: result.status === "success" ? "var(--green)" : "#ef4444" }}>
            {result.status === "success" ? `${result.agent} is live` : `Deploy failed`}
          </div>

          <div className="space-y-2">
            {result.steps.map((step, i) => (
              <div key={i} className="flex items-center gap-3 text-sm">
                <span>{step.status === "pass" ? "✓" : "✗"}</span>
                <span style={{ color: "var(--text)" }}>{step.step}</span>
                {step.duration_ms > 0 && (
                  <span className="ml-auto" style={{ color: "var(--dim)" }}>{(step.duration_ms / 1000).toFixed(1)}s</span>
                )}
              </div>
            ))}
          </div>

          {result.status === "success" && (
            <a
              href={`/test/${encodeURIComponent(result.agent)}`}
              className="mt-4 inline-block text-sm"
              style={{ color: "var(--accent)" }}
            >
              Try it →
            </a>
          )}

          {result.error && (
            <div className="mt-4 text-sm" style={{ color: "#ef4444" }}>{result.error}</div>
          )}
        </div>
      )}
    </div>
  );
}
