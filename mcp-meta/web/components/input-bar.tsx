"use client";

import { useState, useRef } from "react";

interface InputBarProps {
  onSubmit: (text: string) => void;
  placeholder?: string;
  defaultValue?: string;
  compact?: boolean;
}

export function InputBar({ onSubmit, placeholder, defaultValue, compact }: InputBarProps) {
  const [value, setValue] = useState(defaultValue || "");
  const inputRef = useRef<HTMLInputElement>(null);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (value.trim()) {
      onSubmit(value.trim());
      setValue("");
    }
  };

  return (
    <form onSubmit={handleSubmit} style={{ width: "100%" }}>
      <div style={{
        display: "flex",
        alignItems: "center",
        background: "var(--surface)",
        border: "1px solid var(--border2)",
        borderRadius: "var(--r)",
        padding: compact ? "8px 12px" : "12px 16px",
        transition: "border 0.15s",
      }}>
        <input
          ref={inputRef}
          type="text"
          value={value}
          onChange={(e) => setValue(e.target.value)}
          placeholder={placeholder || "What do you need?"}
          style={{
            flex: 1,
            outline: "none",
            background: "transparent",
            color: "var(--text)",
            fontSize: compact ? 13 : 15,
            border: "none",
          }}
        />
        <button
          type="submit"
          disabled={!value.trim()}
          style={{
            marginLeft: 8,
            width: 28,
            height: 28,
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            background: value.trim() ? "var(--accent)" : "transparent",
            color: value.trim() ? "white" : "var(--dim)",
            border: "none",
            borderRadius: 5,
            fontSize: 14,
            cursor: value.trim() ? "pointer" : "default",
            transition: "all 0.15s",
          }}
        >
          {"\u2191"}
        </button>
      </div>
    </form>
  );
}
