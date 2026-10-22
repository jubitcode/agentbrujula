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
          // Browsers refuse to autoplay media that is not muted, and the Player
          // counts as media because it mounts an audio tag for frame timing.
          // Without this the player mounts, renders frame 0, and never advances.
          initiallyMuted
          // This composition has no audio at all, so it needs no audio tags.
          // Dropping them also removes the data: audio element that the site's
          // Content-Security-Policy would otherwise have to allow.
          numberOfSharedAudioTags={0}
          style={{ width: "100%", height: "100%" }}
          // Autoplay is a browser policy decision, not ours: it is refused in
          // background tabs, under some privacy settings, and wherever the page
          // has not been interacted with. When it is refused the composition
          // still mounts and renders frame 0 — which looks like a black box.
          // Controls turn that silent failure into one visible click. This runs
          // in front of people; it must not be able to fail quietly.
          controls
          clickToPlay
          acknowledgeRemotionLicense
        />
      </Suspense>
    </div>
  );
}
