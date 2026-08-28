"use client";
import VideoPlayer from "@/components/VideoPlayer";
import { getHlsUrl, getVideo, type Video } from "@/lib/api";
import { useParams } from "next/navigation";
import { useEffect, useState } from "react";

export default function WatchPage() {
  const { id } = useParams<{ id: string }>();
  const [video, setVideo] = useState<Video | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!id) return;
    let timer: ReturnType<typeof setInterval> | null = null;

    const fetchOnce = async () => {
      try {
        const v = await getVideo(id);
        setVideo(v);
        setError(null);
        if (v.status === "ready" || v.status === "failed") {
          if (timer) clearInterval(timer);
        }
      } catch (e) {
        setError((e as Error).message);
      } finally {
        setLoading(false);
      }
    };

    fetchOnce();
    // Poll while not ready (transcoding)
    timer = setInterval(fetchOnce, 3000);
    return () => {
      if (timer) clearInterval(timer);
    };
  }, [id]);

  if (loading) return <div className="py-20 text-center text-zinc-500">Loading…</div>;
  if (error) return <div className="rounded border border-red-200 bg-red-50 p-4 text-sm text-red-700">{error}</div>;
  if (!video) return <div className="py-20 text-center text-zinc-500">Not found</div>;

  const hlsReady = video.status === "ready";

  return (
    // <div className="grid gap-6 lg:grid-cols-3">
    // <div className="lg:col-span-2">
    <div className="flex flex-col gap-6">
      <div className="">
        {hlsReady ? (
          <VideoPlayer src={getHlsUrl(video.id)} />
        ) : (
          <div className="flex aspect-video items-center justify-center rounded-xl border bg-white text-zinc-500">
            {video.status === "processing" || video.status === "uploaded" ? (
              <span className="animate-pulse">Transcoding… status: {video.status}</span>
            ) : video.status === "failed" ? (
              <span className="text-red-600">Transcoding failed</span>
            ) : (
              <span>{video.status}</span>
            )}
          </div>
        )}
        <h1 className="mt-4 text-xl font-semibold">{video.title}</h1>
        <p className="mt-1 text-sm text-zinc-600">{video.description || "—"}</p>
        <div className="mt-3 flex gap-2 text-xs">
          <span className="rounded bg-zinc-100 px-2 py-1">{video.status}</span>
          {video.duration && <span className="rounded bg-zinc-100 px-2 py-1">{video.duration}s</span>}
          <span className="rounded bg-zinc-100 px-2 py-1">{new Date(video.created_at).toLocaleString()}</span>
        </div>
        {!hlsReady && (
          <p className="mt-2 text-xs text-zinc-400">
            HLS will appear at <code className="rounded bg-zinc-100 px-1">{getHlsUrl(video.id)}</code> once ready.
          </p>
        )}
      </div>
      <div className="rounded-xl border bg-white p-4">
        <h2 className="font-medium">About</h2>
        <p className="mt-2 text-sm text-zinc-600">
          Adaptive HLS (360p/720p/1080p) packed JIT by nginx-vod. Segments aligned to keyframes for seamless switching.
        </p>
        <a href="/" className="mt-4 inline-block text-sm text-red-600 hover:underline">
          ← Back to feed
        </a>
      </div>
    </div>
  );
}
