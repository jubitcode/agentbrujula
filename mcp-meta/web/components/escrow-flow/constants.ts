export const COLORS = {
  bg: "#0a0a0a",
  surface: "#111111",
  border: "#222222",
  text: "#e0e0e0",
  muted: "#888888",
  dim: "#6a6a6a",
  accent: "#3b82f6",
  accentDim: "#1d4ed8",
  green: "#22c55e",
  amber: "#f59e0b",
  red: "#ef4444",
};

export const DURATION_IN_FRAMES = 300;
export const FPS = 30;

export interface FlowNode {
  id: string;
  label: string;
  sublabel: string;
  x: number;
  y: number;
  enterFrame: number;
  color: string;
}

export const NODES: FlowNode[] = [
  { id: "user", label: "You", sublabel: "submits task", x: 80, y: 200, enterFrame: 0, color: COLORS.accent },
  { id: "escrow", label: "Escrow", sublabel: "holds usdc", x: 280, y: 200, enterFrame: 45, color: COLORS.amber },
  { id: "agent", label: "Agent", sublabel: "runs the task", x: 480, y: 200, enterFrame: 100, color: COLORS.accent },
  { id: "verify", label: "Compliance", sublabel: "checks output", x: 680, y: 200, enterFrame: 160, color: COLORS.green },
  { id: "settle", label: "Settled", sublabel: "pays out", x: 880, y: 200, enterFrame: 220, color: COLORS.green },
];

export const EDGES = [
  { from: 0, to: 1, startFrame: 25, label: "x402 payment" },
  { from: 1, to: 2, startFrame: 80, label: "task dispatched" },
  { from: 2, to: 3, startFrame: 140, label: "output hash" },
  { from: 3, to: 4, startFrame: 200, label: "release usdc" },
];
