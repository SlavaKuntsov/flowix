"use client";
import VideoCard from "@/components/VideoCard";
import { listVideos, type Video } from "@/lib/api";
import { useEffect, useState } from "react";

export default function HomePage() {
  const [videos, setVideos] = useState<Video[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    listVideos(24, 0)
      .then((res) => setVideos(res.data))
      .catch((e) => setError((e as Error).message))
      .finally(() => setLoading(false));
  }, []);

  if (loading) return <div className="py-20 text-center text-zinc-500">Loading videos…</div>;
  if (error) return <div className="rounded border border-red-200 bg-red-50 p-4 text-sm text-red-700">Failed to load: {error}</div>;

  if (videos.length === 0) {
    return (
      <div className="py-16 text-center">
        <h1 className="text-2xl font-bold">No videos yet</h1>
        <p className="mt-2 text-zinc-500">Upload your first video to get started.</p>
        <a href="/upload" className="mt-6 inline-block rounded-full bg-red-500 px-6 py-2 text-white hover:bg-red-600">
          Upload video
        </a>
      </div>
    );
  }

  return (
    <div>
      <h1 className="mb-4 text-xl font-semibold">Latest videos</h1>
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {videos.map((v) => (
          <VideoCard key={v.id} video={v} onDeleted={(id) => setVideos((prev) => prev.filter((x) => x.id !== id))} />
        ))}
      </div>
    </div>
  );
}
