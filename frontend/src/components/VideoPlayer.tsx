"use client";
import { useEffect, useRef, useState } from "react";
import Hls from "hls.js";
import { usePlayer } from "@/store/player";

const RATES = [0.5, 0.75, 1, 1.25, 1.5, 2];

export default function VideoPlayer({ src, poster }: { src: string; poster?: string }) {
  const videoRef = useRef<HTMLVideoElement>(null);
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
      // faster quality switch: keep buffer short so next segment loads quicker
      maxBufferLength: 10,
      maxMaxBufferLength: 15,
      liveSyncDurationCount: 2,
    });
    hls.loadSource(src);
    hls.attachMedia(video);
    hls.on(Hls.Events.MANIFEST_PARSED, () => {
      const lvls = hls.levels.map((l, i) => ({ index: i, height: l.height, bitrate: l.bitrate }));
      setLevels(lvls);
      video.play().catch(() => {});
    });
    hls.on(Hls.Events.LEVEL_SWITCHED, (_e, data) => {
      setActualLevel(data.level);
      setPendingLevel(null);
    });
    hls.on(Hls.Events.LEVEL_SWITCHING, (_e, data) => {
      // optional: could set pending here too
    });
    hls.on(Hls.Events.ERROR, (_e, data) => {
      if (data.fatal) {
        setError(data.details);
        if (data.type === Hls.ErrorTypes.NETWORK_ERROR) hls.startLoad();
        else if (data.type === Hls.ErrorTypes.MEDIA_ERROR) hls.recoverMediaError();
      }
    });

    // expose for quality switch
    (video as unknown as { _hls: Hls })._hls = hls;

    return () => {
      hls.destroy();
    };
  }, [src]);

  useEffect(() => {
    if (videoRef.current) videoRef.current.playbackRate = playbackRate;
  }, [playbackRate]);

  const handleQuality = (idx: number) => {
    setCurrentLevel(idx);
    setPendingLevel(idx);
    const video = videoRef.current as unknown as { _hls?: Hls } | null;
    const hls = video?._hls;
    if (hls) {
      // hls.js 1.x: nextLevel is the preferred API, currentLevel also works but we set both for compat
      (hls as unknown as { nextLevel: number }).nextLevel = idx;
      hls.currentLevel = idx;
      // clear pending after segment duration (fallback if LEVEL_SWITCHED not fired)
      setTimeout(() => setPendingLevel((p) => (p === idx ? null : p)), 2500);
    } else {
      setPendingLevel(null);
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
