import type { CSSProperties } from "react";

type Props = {
  height?: number | string;
  style?: CSSProperties;
  className?: string;
  title?: string;
  // kept for backward-compat; ignored by this design
  color?: string;
  nodeColor?: string;
};

export function BrujulaWordmark({
  height = 40,
  style,
  className,
  title = "Brújula",
}: Props) {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      viewBox="0 0 520 120"
      height={height}
      preserveAspectRatio="xMidYMid meet"
      role="img"
      aria-label={title}
      className={className}
      style={{ display: "block", ...style }}
    >
      <title>{title}</title>
      <defs>
        <style>{`@import url('https://fonts.googleapis.com/css2?family=Cinzel:wght@400;600;700&family=Inter:wght@300;400;500&display=swap');`}</style>
      </defs>
      <text
        x="260"
        y="72"
        textAnchor="middle"
        fontFamily="Cinzel, serif"
        fontSize="72"
        fontWeight="600"
        fill="#EEF2F8"
        letterSpacing="18"
      >
        BRÚJULA
      </text>
    </svg>
  );
}
