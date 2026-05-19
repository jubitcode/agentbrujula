"use client";

import { lazy, Suspense, useEffect, useState } from "react";
import { Player } from "@remotion/player";
import { DURATION_IN_FRAMES, FPS } from "./constants";
import { BrujulaLogo } from "../logo/BrujulaLogo";

const LogoIntro = lazy(() =>
  import("./LogoIntro").then((m) => ({ default: m.LogoIntro }))
);

export function LogoIntroPlayer() {
  const [done, setDone] = useState(false);

  useEffect(() => {
    const ms = (DURATION_IN_FRAMES / FPS) * 1000;
    const t = setTimeout(() => setDone(true), ms);
    return () => clearTimeout(t);
  }, []);

  return (
    <div
      style={{
        width: "100%",
        maxWidth: 720,
        aspectRatio: "960 / 400",
        margin: "0 auto 8px",
        position: "relative",
      }}
    >
      {done ? (
        <div
          style={{
            width: "100%",
            height: "100%",
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
          }}
        >
          <div
            style={{
              width: `${(260 * 1.55 * 100) / 960}%`,
              aspectRatio: "1 / 1",
            }}
          >
            <BrujulaLogo size="100%" color="var(--text)" />
          </div>
        </div>
      ) : (
        <Suspense fallback={<div style={{ width: "100%", height: "100%" }} />}>
          <Player
            component={LogoIntro}
            compositionWidth={960}
            compositionHeight={400}
            durationInFrames={DURATION_IN_FRAMES}
            fps={FPS}
            autoPlay
            style={{ width: "100%", height: "100%", background: "transparent" }}
            controls={false}
            acknowledgeRemotionLicense
          />
        </Suspense>
      )}
    </div>
  );
}
