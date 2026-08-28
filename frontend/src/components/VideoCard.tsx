import Link from "next/link";
import { getThumbnailUrl, type Video } from "@/lib/api";

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

export default function VideoCard({ video }: { video: Video }) {
  const thumb = getThumbnailUrl(video);
  return (
    <Link href={`/watch/${video.id}`} className="group overflow-hidden rounded-xl border bg-white hover:shadow-md transition">
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
        <div className="mt-2 flex items-center justify-between">
          <span className={`rounded px-2 py-0.5 text-xs font-medium ${statusColor(video.status)}`}>{video.status}</span>
          <span className="text-xs text-zinc-400">{new Date(video.created_at).toLocaleDateString()}</span>
        </div>
      </div>
    </Link>
  );
}
