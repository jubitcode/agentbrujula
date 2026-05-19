"use client";

import { lazy, Suspense } from "react";
import { Player } from "@remotion/player";
import { DEMO_DURATION, DEMO_FPS, DEMO_WIDTH, DEMO_HEIGHT } from "./constants";

const DemoFlow = lazy(() =>
  import("./DemoFlow").then((m) => ({ default: m.DemoFlow }))
);

export function DemoPlayer() {
  return (
    <div
      style={{
        width: "100%",
        maxWidth: 900,
        aspectRatio: `${DEMO_WIDTH} / ${DEMO_HEIGHT}`,
        margin: "0 auto",
        borderRadius: 10,
        overflow: "hidden",
        border: "1px solid var(--border)",
      }}
    >
      <Suspense
        fallback={
          <div
            style={{
              width: "100%",
              height: "100%",
              background: "var(--surface)",
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
            }}
          >
            <span style={{ fontSize: 12, color: "var(--dim)", fontFamily: "var(--mono)" }}>
              loading demo...
            </span>
          </div>
        }
      >
        <Player
          component={DemoFlow}
          compositionWidth={DEMO_WIDTH}
          compositionHeight={DEMO_HEIGHT}
          durationInFrames={DEMO_DURATION}
          fps={DEMO_FPS}
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
