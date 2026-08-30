"use client";
import VideoPlayer from "@/components/VideoPlayer";
import { deleteVideo, getHlsUrl, getVideo, type Video } from "@/lib/api";
import { useAuth } from "@/store/auth";
import { useParams, useRouter } from "next/navigation";
import { useEffect, useState } from "react";

function BackToFeed({ className = "mt-6" }: { className?: string }) {
  return (
    <a href="/" className={`${className} inline-block text-sm text-red-600 hover:underline`}>
      ← Back to feed
    </a>
  );
}

export default function WatchPage() {
  const { id } = useParams<{ id: string }>();
  const router = useRouter();
  const { userId } = useAuth();
  const [video, setVideo] = useState<Video | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [deleting, setDeleting] = useState(false);

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

  if (loading)
    return (
      <div className="py-20 text-center">
        <div className="text-zinc-500">Loading…</div>
        <BackToFeed />
      </div>
    );
  if (error)
    return (
      <div className="py-10 text-center">
        <div className="mx-auto max-w-lg rounded border border-red-200 bg-red-50 p-4 text-sm text-red-700">{error}</div>
        <BackToFeed />
      </div>
    );
  if (!video)
    return (
      <div className="py-20 text-center">
        <div className="text-zinc-500">Not found</div>
        <BackToFeed />
      </div>
    );

  const hlsReady = video.status === "ready";
  const isOwner = userId && video.owner_id === userId;

  const handleDelete = async () => {
    if (!confirm(`Удалить "${video.title}"? Это удалит все файлы безвозвратно.`)) return;
    setDeleting(true);
    try {
      await deleteVideo(video.id);
      router.push("/");
    } catch (e) {
      alert((e as Error).message);
      setDeleting(false);
    }
  };

  return (
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
        <div className="mt-4 flex items-start justify-between gap-4">
          <div>
            <h1 className="text-xl font-semibold">{video.title}</h1>
            <p className="mt-1 text-sm text-zinc-600">{video.description || "—"}</p>
          </div>
          {isOwner && (
            <button
              onClick={handleDelete}
              disabled={deleting}
              className="shrink-0 rounded bg-red-600 px-4 py-2 text-sm font-medium text-white hover:bg-red-700 disabled:opacity-50"
            >
              {deleting ? "Удаление…" : "Удалить"}
            </button>
          )}
        </div>
        <div className="mt-3 flex flex-wrap gap-2 text-xs">
          <span className="rounded bg-zinc-100 px-2 py-1">{video.status}</span>
          {video.duration && <span className="rounded bg-zinc-100 px-2 py-1">{video.duration}s</span>}
          <span className="rounded bg-zinc-100 px-2 py-1">{new Date(video.created_at).toLocaleString()}</span>
          <span className={`rounded px-2 py-1 ${isOwner ? "bg-green-100 text-green-700" : "bg-zinc-100"}`}>@{video.owner_email || video.owner_id.slice(0, 8)}{isOwner ? " · твое" : ""}</span>
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
        <BackToFeed className="mt-4" />
      </div>
    </div>
  );
}
