"use client";
import { useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { uploadVideo } from "@/lib/api";
import { useAuth } from "@/store/auth";

export default function UploadPage() {
  const router = useRouter();
  const { email } = useAuth();
  const [file, setFile] = useState<File | null>(null);
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [progress, setProgress] = useState<number | null>(null);
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [doneId, setDoneId] = useState<string | null>(null);
  const [dragOver, setDragOver] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (uploading) return; // issue #61: двойной клик не должен запускать параллельную загрузку
    if (!file) {
      setError("Choose a file");
      return;
    }
    if (!email) {
      setError("Please login first");
      router.push("/login");
      return;
    }
    setError(null);
    setProgress(0);
    setUploading(true);
    try {
      const video = await uploadVideo(file, title || file.name, description, setProgress);
      setDoneId(video.id);
      setProgress(100);
      setTimeout(() => router.push(`/watch/${video.id}`), 800);
    } catch (err) {
      setError((err as Error).message);
      setProgress(null);
    } finally {
      setUploading(false);
    }
  };

  return (
    <div className="mx-auto w-full max-w-xl">
      <div className="mb-6">
        <h1 className="text-2xl font-bold tracking-tight text-content">Upload video</h1>
        <p className="mt-1 text-sm text-content-muted">
          Uploaded directly to object storage, then transcoded to 360p / 720p / 1080p.
        </p>
      </div>

      {!email && (
        <div className="mb-5 flex items-center gap-3 rounded-xl border border-amber-500/30 bg-amber-500/10 p-3 text-sm text-amber-700 dark:text-amber-300">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden className="shrink-0">
            <path d="M12 8v5M12 17h.01M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z" />
          </svg>
          <span>
            You are not logged in.{" "}
            <Link href="/login" className="font-medium underline">
              Login
            </Link>{" "}
            to upload.
          </span>
        </div>
      )}

      <form onSubmit={handleSubmit} className="surface space-y-5 p-6">
        <div>
          <span className="label">Video file</span>
          <label
            htmlFor="file"
            onDragOver={(e) => {
              e.preventDefault();
              setDragOver(true);
            }}
            onDragLeave={() => setDragOver(false)}
            onDrop={(e) => {
              e.preventDefault();
              setDragOver(false);
              setFile(e.dataTransfer.files?.[0] ?? null);
            }}
            className={`flex cursor-pointer flex-col items-center justify-center gap-2 rounded-xl border-2 border-dashed px-6 py-10 text-center transition-colors ${
              dragOver ? "border-accent bg-accent/5" : "border-line hover:border-accent/60 hover:bg-surface-muted"
            }`}
          >
            <input
              id="file"
              type="file"
              accept="video/*"
              onChange={(e) => setFile(e.target.files?.[0] ?? null)}
              className="sr-only"
            />
            <svg width="26" height="26" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden className="text-content-subtle">
              <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4M17 8l-5-5-5 5M12 3v12" />
            </svg>
            {file ? (
              <>
                <span className="text-sm font-medium text-content">{file.name}</span>
                <span className="text-xs tabular-nums text-content-subtle">
                  {(file.size / 1_000_000).toFixed(1)} MB
                </span>
              </>
            ) : (
              <>
                <span className="text-sm font-medium text-content">Click to choose, or drop a file here</span>
                <span className="text-xs text-content-subtle">MP4, MOV or MKV</span>
              </>
            )}
          </label>
        </div>

        <div>
          <label htmlFor="title" className="label">
            Title
          </label>
          <input
            id="title"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="My video"
            className="input"
          />
        </div>

        <div>
          <label htmlFor="description" className="label">
            Description
          </label>
          <textarea
            id="description"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="What is this video about?"
            rows={3}
            className="input resize-y"
          />
        </div>

        {progress !== null && (
          <div>
            <div className="mb-1.5 flex items-center justify-between text-xs text-content-muted">
              <span>{progress < 100 ? "Uploading…" : "Finalizing…"}</span>
              <span className="tabular-nums">{progress}%</span>
            </div>
            <div className="h-2 overflow-hidden rounded-full bg-surface-muted">
              <div
                className="h-full rounded-full bg-accent transition-[width] duration-200 ease-out"
                style={{ width: `${progress}%` }}
              />
            </div>
          </div>
        )}

        {error && (
          <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-600 dark:text-red-300">
            {error}
          </div>
        )}
        {doneId && (
          <div className="rounded-lg border border-emerald-500/30 bg-emerald-500/10 p-3 text-sm text-emerald-700 dark:text-emerald-300">
            Uploaded! Redirecting to the player…
          </div>
        )}

        <button type="submit" disabled={uploading} className="btn btn-primary w-full">
          {progress !== null && progress < 100 ? `Uploading ${progress}%` : "Upload"}
        </button>
      </form>
    </div>
  );
}
