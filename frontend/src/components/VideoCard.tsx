import Link from "next/link";
import { deleteVideo, getThumbnailUrl, type Video } from "@/lib/api";
import { useAuth } from "@/store/auth";
import { useState } from "react";

function statusColor(s: Video["status"]): string {
  switch (s) {
    case "ready":
      return "bg-green-100 text-green-700";
    case "processing":
      return "bg-yellow-100 text-yellow-700";
    case "failed":
      return "bg-red-100 text-red-700";
    default:
      return "bg-zinc-100 text-zinc-600";
  }
}

export default function VideoCard({ video, onDeleted }: { video: Video; onDeleted?: (id: string) => void }) {
  const thumb = getThumbnailUrl(video);
  const { userId } = useAuth();
  const [deleting, setDeleting] = useState(false);
  const isOwner = userId && video.owner_id === userId;

  const handleDelete = async (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (!confirm(`Удалить "${video.title}"?`)) return;
    setDeleting(true);
    try {
      await deleteVideo(video.id);
      onDeleted?.(video.id);
    } catch (err) {
      alert((err as Error).message);
      setDeleting(false);
    }
  };

  return (
    <div className="group relative overflow-hidden rounded-xl border bg-white hover:shadow-md transition">
      <Link href={`/watch/${video.id}`} className="block">
        <div className="aspect-video overflow-hidden bg-zinc-100 flex items-center justify-center text-zinc-400 text-sm">
          {thumb ? (
            // eslint-disable-next-line @next/next/no-img-element
            <img src={thumb} alt={video.title} className="h-full w-full object-cover group-hover:scale-[1.02] transition" loading="lazy" />
          ) : (
            <span className="group-hover:text-zinc-600">▶ {video.title}</span>
          )}
        </div>
        <div className="p-3">
          <h3 className="line-clamp-1 font-medium leading-tight">{video.title || "Untitled"}</h3>
          <p className="line-clamp-1 text-sm text-zinc-500">{video.description || "—"}</p>
          <div className="mt-1 flex items-center gap-1 text-xs text-zinc-500">
            <span className="truncate">@{video.owner_email || video.owner_id.slice(0, 8)}</span>
            <span>·</span>
            <span>{new Date(video.created_at).toLocaleDateString()}</span>
          </div>
          <div className="mt-2 flex items-center justify-between">
            <span className={`rounded px-2 py-0.5 text-xs font-medium ${statusColor(video.status)}`}>{video.status}</span>
            {isOwner && <span className="rounded bg-zinc-900 px-2 py-0.5 text-xs text-white">твое</span>}
          </div>
        </div>
      </Link>
      {isOwner && (
        <button
          onClick={handleDelete}
          disabled={deleting}
          title="Удалить видео"
          className="absolute right-2 top-2 rounded bg-white/90 px-2 py-1 text-xs font-medium text-red-600 opacity-0 shadow hover:bg-white group-hover:opacity-100 disabled:opacity-50"
        >
          {deleting ? "…" : "✕"}
        </button>
      )}
    </div>
  );
}
