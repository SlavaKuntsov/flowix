"use client";
import { useEffect, useRef, useState } from "react";
import Hls from "hls.js";
import { usePlayer } from "@/store/player";

const RATES = [0.5, 0.75, 1, 1.25, 1.5, 2];
// Never flush media closer than this to the playhead: the decoder is already
// working on it, and replacing it is exactly what shows up as a dropped frame
// or an audio click.
const SWITCH_LEAD_SECONDS = 1;

export default function VideoPlayer({ src, poster }: { src: string; poster?: string }) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const hlsRef = useRef<Hls | null>(null);
  const { playbackRate, setPlaybackRate } = usePlayer();
  const [error, setError] = useState<string | null>(null);
  const [levels, setLevels] = useState<{ index: number; height: number; bitrate: number }[]>([]);
  const [currentLevel, setCurrentLevel] = useState<number>(-1); // -1 = auto
  const [pendingLevel, setPendingLevel] = useState<number | null>(null);
  const [actualLevel, setActualLevel] = useState<number>(-1);

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;
    setError(null);

    // Prefer hls.js when supported (Chrome/Firefox/macOS Safari with MSE) to enable quality switching.
    // Native HLS fallback only when hls.js not supported (iOS Safari).
    if (Hls.isSupported()) {
      // handled below
    } else if (video.canPlayType("application/vnd.apple.mpegurl")) {
      video.src = src;
      return;
    } else {
      setError("HLS not supported in this browser");
      return;
    }

    const hls = new Hls({
      enableWorker: true,
      lowLatencyMode: false,
    });
    hlsRef.current = hls;
    hls.loadSource(src);
    hls.attachMedia(video);
    hls.on(Hls.Events.MANIFEST_PARSED, () => {
      const lvls = hls.levels.map((l, i) => ({ index: i, height: l.height, bitrate: l.bitrate }));
      setLevels(lvls);
      video.play().catch(() => {});
    });
    // FRAG_CHANGED, not LEVEL_SWITCHED: the level switches when hls.js starts
    // appending the new rendition, but the viewer only sees it once the playhead
    // enters that fragment. Reporting the earlier event is what made the button
    // look done while the picture was still the old quality.
    hls.on(Hls.Events.FRAG_CHANGED, (_e, data) => {
      setActualLevel(data.frag.level);
      setPendingLevel((p) => (p === -1 || p === data.frag.level ? null : p));
    });
    // While paused FRAG_CHANGED never fires (the playhead does not move), so fall
    // back to the level commit event to clear the indicator.
    hls.on(Hls.Events.LEVEL_SWITCHED, (_e, data) => {
      if (!video.paused) return;
      setActualLevel(data.level);
      setPendingLevel((p) => (p === -1 || p === data.level ? null : p));
    });
    hls.on(Hls.Events.ERROR, (_e, data) => {
      if (data.fatal) {
        setError(data.details);
        if (data.type === Hls.ErrorTypes.NETWORK_ERROR) hls.startLoad();
        else if (data.type === Hls.ErrorTypes.MEDIA_ERROR) hls.recoverMediaError();
      }
    });

    return () => {
      hlsRef.current = null;
      hls.destroy();
    };
  }, [src]);

  useEffect(() => {
    if (videoRef.current) videoRef.current.playbackRate = playbackRate;
  }, [playbackRate]);

  const handleQuality = (idx: number) => {
    const hls = hlsRef.current;
    const video = videoRef.current;
    if (!hls || !video || idx === currentLevel) return;

    // Segment boundaries are identical across renditions (aligned GOPs), so any
    // loaded level's fragment list gives the switch points — which is what makes
    // a rapid second click work, when the level just picked has no playlist yet.
    const details =
      hls.levels[hls.currentLevel]?.details ?? hls.levels.find((l) => l.details)?.details;

    setCurrentLevel(idx);
    setPendingLevel(idx);

    // nextLevel, not currentLevel: currentLevel calls immediateLevelSwitch(),
    // which pauses and drops the whole buffer including the frame on screen —
    // that is the freeze-then-jump. nextLevel keeps the playhead's media intact.
    hls.nextLevel = idx;

    // nextLevel alone picks its flush point via a bandwidth-based fetch delay,
    // which on a 2s-segment stream lands 4-6s out and feels like the click did
    // nothing. Redo the forward flush from the first segment starting a full
    // SWITCH_LEAD_SECONDS ahead instead, so the new quality arrives within a
    // segment while the flush still never reaches the decoded region.
    // Auto is excluded on purpose: ABR would just refill what we dropped, and a
    // thin buffer biases it toward picking a lower rendition.
    if (idx === -1) return;
    const target = details?.fragments.find(
      (f) => f.start >= video.currentTime + SWITCH_LEAD_SECONDS,
    );
    if (target) {
      hls.trigger(Hls.Events.BUFFER_FLUSHING, {
        startOffset: target.start + target.duration / 2,
        endOffset: Infinity,
        type: null,
      });
    }
  };

  return (
    <div className="overflow-hidden rounded-xl bg-black">
      <video
        ref={videoRef}
        poster={poster}
        controls
        playsInline
        className="aspect-video w-full"
        crossOrigin="anonymous"
      />
      {error && <div className="bg-red-600 px-3 py-2 text-sm text-white">Playback error: {error}</div>}
      <div className="flex flex-wrap items-center gap-2 bg-zinc-900 px-3 py-2 text-xs text-zinc-300">
        <span className="mr-1">Speed:</span>
        {RATES.map((r) => (
          <button
            key={r}
            onClick={() => setPlaybackRate(r)}
            className={`rounded px-2 py-1 ${playbackRate === r ? "bg-white text-black" : "bg-zinc-800 hover:bg-zinc-700"}`}
          >
            {r}x
          </button>
        ))}
        {levels.length > 0 && (
          <>
            <span className="ml-3 mr-1">Quality:</span>
            <button
              onClick={() => handleQuality(-1)}
              className={`rounded px-2 py-1 ${currentLevel === -1 ? "bg-white text-black" : "bg-zinc-800 hover:bg-zinc-700"}`}
            >
              Auto{pendingLevel === -1 ? "…" : ""}
            </button>
            {levels.map((l) => (
              <button
                key={l.index}
                onClick={() => handleQuality(l.index)}
                className={`rounded px-2 py-1 ${currentLevel === l.index ? "bg-white text-black" : "bg-zinc-800 hover:bg-zinc-700"} ${pendingLevel === l.index ? "animate-pulse ring-1 ring-white" : ""}`}
              >
                {l.height}p{pendingLevel === l.index ? "…" : actualLevel === l.index && currentLevel !== -1 ? " ●" : ""}
              </button>
            ))}
          </>
        )}
      </div>
    </div>
  );
}
