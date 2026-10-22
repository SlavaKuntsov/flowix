import StatusBadge from "@/components/StatusBadge";
import { deleteVideo, getThumbnailUrl, type Video } from "@/lib/api";
import { formatDate, formatDuration } from "@/lib/format";
import { useAuth } from "@/store/auth";
import Link from "next/link";
import { useState } from "react";

function ownerName(video: Video): string {
  return video.owner_email || video.owner_id.slice(0, 8);
}

export default function VideoCard({ video, onDeleted }: { video: Video; onDeleted?: (id: string) => void }) {
  const thumb = getThumbnailUrl(video);
  const { userId } = useAuth();
  const [deleting, setDeleting] = useState(false);
  const isOwner = !!userId && video.owner_id === userId;
  const duration = formatDuration(video.duration);
  const initial = ownerName(video).charAt(0).toUpperCase();

  const handleDelete = async (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (!confirm(`Delete "${video.title}"? This can't be undone.`)) return;
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
    <article className="group surface surface-hover relative overflow-hidden">
      <Link href={`/watch/${video.id}`} className="block focus-visible:outline-none">
        <div className="relative aspect-video overflow-hidden bg-surface-muted">
          {thumb ? (
            // eslint-disable-next-line @next/next/no-img-element
            <img
              src={thumb}
              alt={video.title}
              loading="lazy"
              className="h-full w-full object-cover outline outline-1 -outline-offset-1 outline-black/10 transition-transform duration-300 ease-out group-hover:scale-[1.03] dark:outline-white/10"
            />
          ) : (
            <div className="flex h-full w-full items-center justify-center bg-gradient-to-br from-surface-muted to-surface text-content-subtle">
              <svg width="40" height="40" viewBox="0 0 24 24" fill="currentColor" aria-hidden className="translate-x-[2px] opacity-70">
                <path d="M8 5.14v13.72a1 1 0 0 0 1.54.84l10.3-6.86a1 1 0 0 0 0-1.68L9.54 4.3A1 1 0 0 0 8 5.14z" />
              </svg>
            </div>
          )}
          {duration && (
            <span className="absolute bottom-2 right-2 rounded-md bg-black/75 px-1.5 py-0.5 text-xs font-medium tabular-nums text-white">
              {duration}
            </span>
          )}
        </div>

        <div className="p-4">
          <h3 className="line-clamp-2 font-semibold leading-snug text-content">{video.title || "Untitled"}</h3>
          <p className="mt-1 line-clamp-1 text-sm text-content-muted">
            {video.description || "No description"}
          </p>

          <div className="mt-3 flex items-center gap-2 text-xs text-content-subtle">
            <span className="flex size-5 shrink-0 items-center justify-center rounded-full bg-surface-muted text-[10px] font-semibold uppercase text-content-muted">
              {initial}
            </span>
            <span className="truncate">{ownerName(video)}</span>
            <span aria-hidden>·</span>
            <time dateTime={video.created_at} className="shrink-0 tabular-nums">
              {formatDate(video.created_at)}
            </time>
          </div>

          <div className="mt-3 flex items-center justify-between gap-2">
            <StatusBadge status={video.status} />
            {isOwner && <span className="chip">Yours</span>}
          </div>
        </div>
      </Link>

      {isOwner && (
        <button
          onClick={handleDelete}
          disabled={deleting}
          title="Delete video"
          aria-label="Delete video"
          className="absolute right-2 top-2 flex size-8 items-center justify-center rounded-lg bg-black/60 text-white opacity-0 backdrop-blur-sm transition-opacity duration-150 hover:bg-black/80 focus-visible:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-white group-hover:opacity-100 disabled:opacity-50 after:absolute after:left-1/2 after:top-1/2 after:size-10 after:-translate-x-1/2 after:-translate-y-1/2"
        >
          {deleting ? (
            <span className="text-xs">…</span>
          ) : (
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden>
              <path d="M6 6l12 12M18 6L6 18" />
            </svg>
          )}
        </button>
      )}
    </article>
  );
}
