"use client";
import Link from "next/link";
import VideoPlayer from "@/components/VideoPlayer";
import StatusBadge from "@/components/StatusBadge";
import { deleteVideo, getHlsUrl, getHlsToken, getVideo, updateVideo, type Video, type Visibility } from "@/lib/api";
import { formatDate, formatDuration } from "@/lib/format";
import { useAuth } from "@/store/auth";
import { useParams, useRouter } from "next/navigation";
import { useEffect, useState } from "react";

const VISIBILITIES: Visibility[] = ["public", "private", "unlisted"];

function Spinner() {
  return <span className="size-6 animate-spin rounded-full border-2 border-line border-t-accent" aria-hidden />;
}

function PlayerPlaceholder({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex aspect-video flex-col items-center justify-center gap-3 rounded-2xl border border-line bg-surface px-6 text-center text-sm text-content-muted">
      {children}
    </div>
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
  const [hlsSrc, setHlsSrc] = useState<string | null>(null);
  const [visibility, setVisibility] = useState<string>("public");

  useEffect(() => {
    if (!id) return;
    let timer: ReturnType<typeof setInterval> | null = null;
    // issue #61: поллинг с ошибками не должен жить вечно — 5 неудач подряд → стоп
    let failures = 0;

    const fetchOnce = async () => {
      try {
        const v = await getVideo(id);
        failures = 0;
        setVideo(v);
        setError(null);
        if (v.status === "ready" || v.status === "failed") {
          if (timer) clearInterval(timer);
        }
      } catch (e) {
        setError((e as Error).message);
        failures += 1;
        if (failures >= 5 && timer) {
          clearInterval(timer);
          timer = null;
        }
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

  const hlsReady = video?.status === "ready";
  const isOwner = !!(userId && video && video.owner_id === userId);

  useEffect(() => {
    if (!video) return;
    setVisibility(video.visibility || "public");
    if (!hlsReady) {
      setHlsSrc(null);
      return;
    }
    if (video.visibility === "private" && isOwner) {
      getHlsToken(video.id)
        .then((t) => setHlsSrc(getHlsUrl(video.id, t.token)))
        .catch(() => setHlsSrc(getHlsUrl(video.id)));
    } else if (video.visibility === "private") {
      // logout/не-владелец: гасим плеер сразу — подписанный hls-токен старой
      // сессии не должен продолжать играть в открытой вкладке
      setHlsSrc(null);
    } else {
      setHlsSrc(getHlsUrl(video.id));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [video?.id, video?.visibility, hlsReady, isOwner]);

  if (loading)
    return (
      <div className="flex flex-col gap-6">
        <div className="aspect-video w-full animate-pulse rounded-2xl bg-surface-muted" />
        <div className="space-y-3">
          <div className="h-7 w-2/3 animate-pulse rounded bg-surface-muted" />
          <div className="h-4 w-1/3 animate-pulse rounded bg-surface-muted" />
          <div className="flex gap-2">
            <div className="h-6 w-20 animate-pulse rounded-full bg-surface-muted" />
            <div className="h-6 w-24 animate-pulse rounded-full bg-surface-muted" />
          </div>
        </div>
      </div>
    );

  if (error)
    return (
      <div className="surface mx-auto mt-10 max-w-lg p-8 text-center">
        <h2 className="text-lg font-semibold text-content">Couldn&apos;t load this video</h2>
        <p className="mt-1 text-sm text-content-muted">{error}</p>
        <Link href="/" className="btn btn-secondary mt-5">
          Back to feed
        </Link>
      </div>
    );

  if (!video)
    return (
      <div className="surface mx-auto mt-10 max-w-lg p-8 text-center">
        <h2 className="text-lg font-semibold text-content">Video not found</h2>
        <p className="mt-1 text-sm text-content-muted">It may have been deleted or made private.</p>
        <Link href="/" className="btn btn-secondary mt-5">
          Back to feed
        </Link>
      </div>
    );

  const handleDelete = async () => {
    if (!confirm(`Delete "${video.title}"? This removes all files permanently.`)) return;
    setDeleting(true);
    try {
      await deleteVideo(video.id);
      router.push("/");
    } catch (e) {
      alert((e as Error).message);
      setDeleting(false);
    }
  };

  const handleVisibility = async (v: Visibility) => {
    try {
      const upd = await updateVideo(video.id, { visibility: v });
      setVideo(upd);
      setVisibility(upd.visibility || v);
    } catch (e) {
      alert((e as Error).message);
    }
  };

  const duration = formatDuration(video.duration);

  return (
    <div className="flex flex-col gap-6">
      <Link href="/" className="btn btn-ghost -ml-1 self-start">
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
          <path d="M19 12H5M12 19l-7-7 7-7" />
        </svg>
        Back to feed
      </Link>

      {hlsReady ? (
        hlsSrc ? (
          <VideoPlayer src={hlsSrc} />
        ) : video.visibility === "private" && !isOwner ? (
          <PlayerPlaceholder>
            <svg width="26" height="26" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
              <rect x="3" y="11" width="18" height="11" rx="2" />
              <path d="M7 11V7a5 5 0 0 1 10 0v4" />
            </svg>
            <span>Private video — sign in as the owner to watch</span>
          </PlayerPlaceholder>
        ) : (
          <PlayerPlaceholder>
            <Spinner />
            <span>Preparing stream…</span>
          </PlayerPlaceholder>
        )
      ) : (
        <PlayerPlaceholder>
          {video.status === "processing" || video.status === "uploaded" ? (
            <>
              <Spinner />
              <span>
                {video.status === "uploaded" ? "Waiting for upload…" : "Transcoding…"}{" "}
                <span className="text-content-subtle">{video.status}</span>
              </span>
            </>
          ) : video.status === "failed" ? (
            <span className="text-red-500">Transcoding failed</span>
          ) : (
            <span>{video.status}</span>
          )}
        </PlayerPlaceholder>
      )}

      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <h1 className="text-2xl font-bold tracking-tight text-content">{video.title}</h1>
          <p className="mt-1.5 text-sm text-content-muted">{video.description || "No description"}</p>
        </div>

        {isOwner && (
          <div className="flex items-center gap-2">
            <div className="flex items-center rounded-full bg-surface-muted p-0.5" role="group" aria-label="Visibility">
              {VISIBILITIES.map((v) => (
                <button
                  key={v}
                  onClick={() => handleVisibility(v)}
                  aria-pressed={visibility === v}
                  className={`rounded-full px-3 py-1 text-xs font-medium capitalize transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/45 ${
                    visibility === v ? "bg-surface text-content shadow-surface" : "text-content-muted hover:text-content"
                  }`}
                >
                  {v}
                </button>
              ))}
            </div>
            <button onClick={handleDelete} disabled={deleting} className="btn btn-danger">
              {deleting ? "Deleting…" : "Delete"}
            </button>
          </div>
        )}
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <StatusBadge status={video.status} />
        {duration && <span className="chip tabular-nums">{duration}</span>}
        <span className="chip tabular-nums">{new Date(video.created_at).toLocaleString()}</span>
        <span className="chip">
          @{video.owner_email || video.owner_id.slice(0, 8)}
          {isOwner ? " · yours" : ""}
        </span>
        <span className="chip capitalize">{video.visibility || "public"}</span>
      </div>

      {!hlsReady && (
        <p className="-mt-2 text-xs text-content-subtle">
          HLS will appear at{" "}
          <code className="rounded bg-surface-muted px-1.5 py-0.5 font-mono text-[11px]">{getHlsUrl(video.id)}</code>{" "}
          once ready.
        </p>
      )}

      <div className="surface p-5">
        <h2 className="font-semibold text-content">About</h2>
        <p className="mt-2 text-sm text-content-muted">
          Adaptive HLS (360p / 720p / 1080p) packed JIT by nginx-vod. Segments are aligned to
          keyframes for seamless quality switching.
        </p>
        <p className="mt-3 text-xs text-content-subtle">
          Created {formatDate(video.created_at)} · ID <code className="font-mono">{video.id.slice(0, 8)}</code>
        </p>
      </div>
    </div>
  );
}
