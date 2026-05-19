export const DEMO_FPS = 30;
export const DEMO_DURATION = 600; // 20 seconds
export const DEMO_WIDTH = 960;
export const DEMO_HEIGHT = 540;

export const SCENES = [
  { id: "task", label: "Describe", start: 0, end: 135 },
  { id: "discover", label: "Discover", start: 125, end: 260 },
  { id: "escrow", label: "Lock", start: 250, end: 385 },
  { id: "execute", label: "Execute", start: 375, end: 495 },
  { id: "result", label: "Settle", start: 485, end: 599 },
] as const;

export const C = {
  bg: "#0a0a0a",
  surface: "#111111",
  surface2: "#1a1a1a",
  border: "#222222",
  border2: "#2a2a2a",
  text: "#e0e0e0",
  muted: "#888888",
  dim: "#555555",
  accent: "#3b82f6",
  accentDim: "#1d4ed8",
  green: "#22c55e",
  amber: "#f59e0b",
  red: "#ef4444",
};
