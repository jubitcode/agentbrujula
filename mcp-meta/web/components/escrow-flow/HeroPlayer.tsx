"use client";

import { lazy, Suspense } from "react";
import { Player } from "@remotion/player";
import { DURATION_IN_FRAMES, FPS } from "./constants";

const EscrowFlow = lazy(() =>
  import("./EscrowFlow").then((m) => ({ default: m.EscrowFlow }))
);

export function HeroPlayer() {
  return (
    <div
      style={{
        width: "100%",
        maxWidth: 900,
        aspectRatio: "960 / 400",
        margin: "0 auto",
        borderRadius: 10,
        overflow: "hidden",
        border: "1px solid var(--border)",
      }}
    >
      <Suspense
        fallback={
          <div style={{ width: "100%", height: "100%", background: "var(--surface)", display: "flex", alignItems: "center", justifyContent: "center" }}>
            <span style={{ fontSize: 12, color: "var(--dim)", fontFamily: "var(--mono)" }}>loading animation...</span>
          </div>
        }
      >
        <Player
          component={EscrowFlow}
          compositionWidth={960}
          compositionHeight={400}
          durationInFrames={DURATION_IN_FRAMES}
          fps={FPS}
          loop
          autoPlay
          style={{ width: "100%", height: "100%" }}
          controls={false}
          acknowledgeRemotionLicense
        />
      </Suspense>
    </div>
  );
}
