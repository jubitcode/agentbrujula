import { AbsoluteFill, interpolate, spring, useCurrentFrame, useVideoConfig } from "remotion";
import { BrujulaLogo } from "../logo/BrujulaLogo";
import { COLORS } from "./constants";

const WORD = ["r", "ú", "j", "u", "l", "a"];

const NODE_OFFSETS: Array<{ x: number; y: number }> = [
  { x: -55, y: -70 },
  { x: 45, y: -58 },
  { x: 68, y: 10 },
  { x: 30, y: 70 },
  { x: -40, y: 60 },
  { x: -70, y: -5 },
];

const LOGO_SIZE = 260;
const LOGO_CX_START = 200;
const LOGO_CX_END = 480;
const LOGO_CY = 200;
const LETTER_FONT_SIZE = 144;
const LETTER_GAP = 68;
const LETTER_Y = LOGO_CY;
const FIRST_LETTER_X = LOGO_CX_START + LOGO_SIZE / 2 - 18;

export function LogoIntro() {
  const frame = useCurrentFrame();
  const { fps, durationInFrames } = useVideoConfig();

  const logoEntry = spring({ frame, fps, config: { damping: 18, stiffness: 90 } });
  const entryScale = interpolate(logoEntry, [0, 1], [0.6, 1]);
  const growth = interpolate(
    frame,
    [110, 155],
    [1, 1.55],
    { extrapolateLeft: "clamp", extrapolateRight: "clamp" }
  );
  const logoScale = entryScale * growth;
  const logoOpacity = interpolate(frame, [0, 14], [0, 1], { extrapolateRight: "clamp" });

  const collapseStart = 70;
  const collapseEnd = 110;

  const lastCollapseEnd = collapseStart + (WORD.length - 1) * 3 + 28;
  const logoCx = interpolate(
    frame,
    [collapseStart + 5, lastCollapseEnd + 10],
    [LOGO_CX_START, LOGO_CX_END],
    { extrapolateLeft: "clamp", extrapolateRight: "clamp" }
  );

  const glow = interpolate(
    frame,
    [collapseStart, collapseEnd, collapseEnd + 20, durationInFrames - 10, durationInFrames],
    [0, 1, 0.55, 0.55, 0],
    { extrapolateLeft: "clamp", extrapolateRight: "clamp" }
  );

  const taglineOpacity = interpolate(
    frame,
    [collapseEnd - 5, collapseEnd + 20],
    [0, 1],
    { extrapolateLeft: "clamp", extrapolateRight: "clamp" }
  );

  return (
    <AbsoluteFill
      style={{
        background: "transparent",
        fontFamily: "-apple-system, BlinkMacSystemFont, 'Helvetica Neue', sans-serif",
        color: COLORS.text,
      }}
    >
      {/* Logo glow halo */}
      <div
        style={{
          position: "absolute",
          left: logoCx - LOGO_SIZE / 2,
          top: LOGO_CY - LOGO_SIZE / 2,
          width: LOGO_SIZE,
          height: LOGO_SIZE,
          borderRadius: "50%",
          background: `radial-gradient(circle, ${COLORS.accent}33 0%, transparent 65%)`,
          filter: "blur(14px)",
          opacity: 0.3 + glow * 0.6,
          transform: `scale(${0.9 + glow * 0.2})`,
          pointerEvents: "none",
        }}
      />

      {/* Logo */}
      <div
        style={{
          position: "absolute",
          left: logoCx - LOGO_SIZE / 2,
          top: LOGO_CY - LOGO_SIZE / 2,
          width: LOGO_SIZE,
          height: LOGO_SIZE,
          transform: `scale(${logoScale})`,
          transformOrigin: "center",
          opacity: logoOpacity,
          color: COLORS.text,
        }}
      >
        <BrujulaLogo size={LOGO_SIZE} color="currentColor" />
      </div>

      {/* Landing node pulses inside logo */}
      {NODE_OFFSETS.map((off, i) => {
        const arrival = collapseStart + 24 + i * 2;
        const pulse = interpolate(
          frame,
          [arrival - 4, arrival, arrival + 18],
          [0, 1, 0],
          { extrapolateLeft: "clamp", extrapolateRight: "clamp" }
        );
        const size = 4 + pulse * 22;
        return (
          <div
            key={`pulse-${i}`}
            style={{
              position: "absolute",
              left: logoCx + off.x - size / 2,
              top: LOGO_CY + off.y - size / 2,
              width: size,
              height: size,
              borderRadius: "50%",
              background: COLORS.accent,
              opacity: pulse * 0.8,
              filter: "blur(2px)",
              pointerEvents: "none",
            }}
          />
        );
      })}

      {/* Trailing letters */}
      {WORD.map((ch, i) => {
        const enterAt = 24 + i * 4;
        const appear = spring({
          frame: frame - enterAt,
          fps,
          config: { damping: 14, stiffness: 120 },
        });
        const appearOpacity = interpolate(frame, [enterAt, enterAt + 10], [0, 1], {
          extrapolateLeft: "clamp",
          extrapolateRight: "clamp",
        });

        const startX = FIRST_LETTER_X + i * LETTER_GAP;
        const targetX = logoCx + NODE_OFFSETS[i].x;
        const targetY = LOGO_CY + NODE_OFFSETS[i].y;

        const letterCollapseStart = collapseStart + i * 3;
        const letterCollapseEnd = letterCollapseStart + 28;

        const t = interpolate(frame, [letterCollapseStart, letterCollapseEnd], [0, 1], {
          extrapolateLeft: "clamp",
          extrapolateRight: "clamp",
        });
        // Ease-in curve so letters accelerate into the logo
        const eased = t * t;

        const x = interpolate(eased, [0, 1], [startX, targetX]);
        const y = interpolate(eased, [0, 1], [LETTER_Y, targetY]);
        const scale = interpolate(eased, [0, 1], [1, 0.08]) * (0.7 + 0.3 * appear);
        const fadeCollapse = interpolate(eased, [0.6, 1], [1, 0], {
          extrapolateLeft: "clamp",
          extrapolateRight: "clamp",
        });

        const strokeColor = COLORS.text;
        return (
          <div
            key={ch + i}
            style={{
              position: "absolute",
              left: 0,
              top: 0,
              width: LETTER_GAP,
              height: LETTER_FONT_SIZE,
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              transform: `translate(${x - LETTER_GAP / 2}px, ${y - LETTER_FONT_SIZE / 2}px) scale(${scale})`,
              transformOrigin: "center",
              fontSize: LETTER_FONT_SIZE,
              fontFamily: "'Georgia', 'Times New Roman', serif",
              fontStyle: "italic",
              fontWeight: 400,
              lineHeight: 1,
              letterSpacing: 0,
              color: strokeColor,
              opacity: appearOpacity * fadeCollapse,
            }}
          >
            {ch}
          </div>
        );
      })}

      {/* Tagline */}
      <div
        style={{
          position: "absolute",
          left: 0,
          right: 0,
          bottom: 60,
          textAlign: "center",
          fontFamily: "'Courier New', monospace",
          fontSize: 12,
          letterSpacing: 2,
          textTransform: "uppercase",
          color: COLORS.dim,
          opacity: taglineOpacity,
        }}
      >
        // direction, intelligence, trust
      </div>
    </AbsoluteFill>
  );
}
