"use client";
import Link from "next/link";
import VideoCard from "@/components/VideoCard";
import { listVideos, type Video } from "@/lib/api";
import { useCallback, useEffect, useState } from "react";

function SkeletonCard() {
  return (
    <div className="surface overflow-hidden">
      <div className="aspect-video animate-pulse bg-surface-muted" />
      <div className="space-y-2.5 p-4">
        <div className="h-4 w-3/4 animate-pulse rounded bg-surface-muted" />
        <div className="h-3 w-1/2 animate-pulse rounded bg-surface-muted" />
        <div className="h-6 w-20 animate-pulse rounded-full bg-surface-muted" />
      </div>
    </div>
  );
}

function PlayGlyph({ className = "" }: { className?: string }) {
  return (
    <svg width="26" height="26" viewBox="0 0 24 24" fill="currentColor" aria-hidden className={`translate-x-[2px] ${className}`}>
      <path d="M8 5.14v13.72a1 1 0 0 0 1.54.84l10.3-6.86a1 1 0 0 0 0-1.68L9.54 4.3A1 1 0 0 0 8 5.14z" />
    </svg>
  );
}

export default function HomePage() {
  const [videos, setVideos] = useState<Video[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(() => {
    setLoading(true);
    setError(null);
    listVideos(24, 0)
      .then((res) => setVideos(res.data))
      .catch((e) => setError((e as Error).message))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  return (
    <div>
      <div className="mb-6 flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-content">Latest videos</h1>
          <p className="mt-1 text-sm text-content-muted">
            {loading ? "Loading the library…" : `${videos.length} video${videos.length === 1 ? "" : "s"} · adaptive HLS`}
          </p>
        </div>
        <Link href="/upload" className="btn btn-primary">
          Upload video
        </Link>
      </div>

      {loading ? (
        <div className="grid gap-5 sm:grid-cols-2 lg:grid-cols-3">
          {Array.from({ length: 6 }).map((_, i) => (
            <SkeletonCard key={i} />
          ))}
        </div>
      ) : error ? (
        <div className="surface mx-auto mt-10 max-w-lg p-8 text-center">
          <div className="mx-auto flex size-12 items-center justify-center rounded-full bg-red-500/10 text-red-500">
            <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden>
              <path d="M12 8v5M12 17h.01M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z" />
            </svg>
          </div>
          <h2 className="mt-4 text-lg font-semibold text-content">Couldn&apos;t load videos</h2>
          <p className="mt-1 text-sm text-content-muted">{error}</p>
          <button onClick={load} className="btn btn-secondary mt-5">
            Try again
          </button>
        </div>
      ) : videos.length === 0 ? (
        <div className="surface mx-auto mt-10 max-w-lg p-10 text-center">
          <div className="mx-auto flex size-14 items-center justify-center rounded-2xl bg-accent-soft text-accent-text">
            <PlayGlyph />
          </div>
          <h2 className="mt-4 text-xl font-semibold text-content">No videos yet</h2>
          <p className="mt-1 text-sm text-content-muted">Upload your first video to get started.</p>
          <Link href="/upload" className="btn btn-primary mt-6">
            Upload video
          </Link>
        </div>
      ) : (
        <div className="grid gap-5 sm:grid-cols-2 lg:grid-cols-3">
          {videos.map((v) => (
            <VideoCard key={v.id} video={v} onDeleted={(id) => setVideos((prev) => prev.filter((x) => x.id !== id))} />
          ))}
        </div>
      )}
    </div>
  );
}
