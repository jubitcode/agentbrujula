"use client";

import { lazy, Suspense, useEffect, useRef } from "react";
import { Player, type PlayerRef } from "@remotion/player";

const CompassComposition = lazy(() =>
  import("./CompassComposition").then((m) => ({ default: m.CompassComposition }))
);

const FPS = 30;
const DURATION_IN_FRAMES = 1800;
const SCROLL_RANGE_PX = 1600;

/**
 * Compass that rotates as the visitor scrolls the page. Sizes itself to the
 * parent's width (we drop it into a grid column and it fills it), keeping
 * the square 1:1 aspect ratio. The original size for the landing hero was a
 * full grid column — keeping that intact here.
 */
export function CompassScroll() {
  const ref = useRef<PlayerRef>(null);

  useEffect(() => {
    let raf = 0;
    const update = () => {
      const progress = Math.min(
        1,
        Math.max(0, window.scrollY / SCROLL_RANGE_PX)
      );
      const frame = Math.floor(progress * (DURATION_IN_FRAMES - 1));
      ref.current?.seekTo(frame);
    };
    const onScroll = () => {
      cancelAnimationFrame(raf);
      raf = requestAnimationFrame(update);
    };
    window.addEventListener("scroll", onScroll, { passive: true });
    update();
    return () => {
      window.removeEventListener("scroll", onScroll);
      cancelAnimationFrame(raf);
    };
  }, []);

  return (
    <div style={{ width: "100%", aspectRatio: "1 / 1" }}>
      <Suspense fallback={<div style={{ width: "100%", height: "100%" }} />}>
        <Player
          ref={ref}
          component={CompassComposition}
          compositionWidth={400}
          compositionHeight={400}
          durationInFrames={DURATION_IN_FRAMES}
          fps={FPS}
          style={{ width: "100%", height: "100%", background: "transparent" }}
          controls={false}
          acknowledgeRemotionLicense
        />
      </Suspense>
    </div>
  );
}
