import { ImageResponse } from "next/og";

// Apple touch icon — what shows up when an iPhone/iPad user adds the page
// to their home screen. iOS only accepts raster (PNG/JPG); generating it
// from JSX keeps it in sync with the rest of the brand without a separate
// asset pipeline.

export const runtime = "edge";
export const size = { width: 180, height: 180 };
export const contentType = "image/png";

export default async function AppleIcon() {
  return new ImageResponse(
    (
      <div
        style={{
          width: "100%",
          height: "100%",
          background: "#0a0a0a",
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
        }}
      >
        <svg viewBox="0 0 180 180" width="180" height="180">
          <circle cx="90" cy="90" r="76" fill="#0a0a0a" stroke="#2a2a2a" strokeWidth="4" />
          <circle cx="90" cy="90" r="64" fill="none" stroke="#2a2a2a" strokeWidth="2" />
          <line x1="90"  y1="14"  x2="90"  y2="28"  stroke="#888888" strokeWidth="4" />
          <line x1="166" y1="90"  x2="152" y2="90"  stroke="#444444" strokeWidth="3" />
          <line x1="90"  y1="166" x2="90"  y2="152" stroke="#444444" strokeWidth="3" />
          <line x1="14"  y1="90"  x2="28"  y2="90"  stroke="#444444" strokeWidth="3" />
          <polygon points="90,30 74,90 106,90" fill="#3b82f6" />
          <polygon points="90,150 74,90 106,90" fill="#5a5a5a" />
          <circle cx="90" cy="90" r="8" fill="#0a0a0a" stroke="#3b82f6" strokeWidth="3" />
        </svg>
      </div>
    ),
    size,
  );
}
