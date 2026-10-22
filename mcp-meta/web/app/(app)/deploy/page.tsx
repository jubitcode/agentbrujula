"use client";

import { useState, useRef } from "react";
import { deployAgent } from "@/lib/api";
import type { DeployResult } from "@/lib/types";
import { useI18n } from "@/lib/i18n";
import { ErrorState } from "@/components/ui/states";

export default function DeployPage() {
  const { t } = useI18n();
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
      setError(t("deploy.noManifest"));
      return;
    }

    setDeploying(true);
    setError(null);
    setResult(null);

    try {
      const res = await deployAgent(manifest, extraFiles);
      setResult(res);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("deploy.failed"));
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
    <div className="page">
      <h1 className="t-h2">{t("deploy.title")}</h1>
      <p className="t-intro" style={{ marginBottom: 28 }}>{t("deploy.intro")}</p>

      {/* Drop zone */}
      <div
        onDragOver={(e) => { e.preventDefault(); setDragOver(true); }}
        onDragLeave={() => setDragOver(false)}
        onDrop={handleDrop}
        onClick={() => fileInputRef.current?.click()}
        role="button"
        tabIndex={0}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            fileInputRef.current?.click();
          }
        }}
        style={{
          border: `2px dashed ${dragOver ? "var(--accent)" : "var(--border2)"}`,
          background: dragOver ? "rgba(59,130,246,0.08)" : "transparent",
          borderRadius: "var(--r2)",
          padding: "clamp(32px, 8vw, 56px) 24px",
          textAlign: "center",
          cursor: "pointer",
          transition: "border-color .15s, background .15s",
        }}
      >
        <p className="state-title" style={{ display: "flex", alignItems: "center", justifyContent: "center", gap: 10 }}>
          {deploying && <span className="spinner" aria-hidden="true" />}
          {deploying ? t("deploy.working") : t("deploy.drop")}
        </p>
        <p className="state-hint">{t("deploy.dropHint")}</p>
        <input
          ref={fileInputRef}
          type="file"
          multiple
          style={{ display: "none" }}
          onChange={(e) => e.target.files && handleFiles(e.target.files)}
        />
      </div>

      {/* Error and result are mutually exclusive: a failed deploy clears the
          previous result, and a new attempt clears the error. */}
      {error && (
        <div style={{ marginTop: 16 }}>
          <ErrorState title={t("deploy.failed")} detail={error} />
        </div>
      )}

      {result && (
        <div className="card" style={{ marginTop: 24 }}>
          <div
            className="t-h3"
            style={{ color: result.status === "success" ? "var(--green)" : "#ef4444", marginBottom: 16 }}
          >
            {result.status === "success"
              ? t("deploy.live", { agent: result.agent })
              : t("deploy.failed")}
          </div>

          <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
            {result.steps.map((step, i) => (
              <div key={i} className="t-small" style={{ display: "flex", alignItems: "center", gap: 12, margin: 0 }}>
                <span aria-hidden="true" style={{ color: step.status === "pass" ? "var(--green)" : "#ef4444" }}>
                  {step.status === "pass" ? "✓" : "✗"}
                </span>
                <span className="t-wrap" style={{ color: "var(--text)" }}>{step.step}</span>
                {step.duration_ms > 0 && (
                  <span style={{ marginLeft: "auto", color: "var(--dim)", flexShrink: 0 }}>
                    {(step.duration_ms / 1000).toFixed(1)}s
                  </span>
                )}
              </div>
            ))}
          </div>

          {result.status === "success" && (
            <a
              href={`/test/${encodeURIComponent(result.agent)}`}
              style={{ marginTop: 16, display: "inline-block", fontSize: 13, color: "var(--accent)" }}
            >
              {t("deploy.tryIt")} →
            </a>
          )}

          {result.error && (
            <div className="state-detail">{result.error}</div>
          )}
        </div>
      )}
    </div>
  );
}
