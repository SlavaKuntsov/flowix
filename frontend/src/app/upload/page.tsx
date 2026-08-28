"use client";
import { useState } from "react";
import { useRouter } from "next/navigation";
import { uploadVideo } from "@/lib/api";
import { useAuth } from "@/store/auth";

export default function UploadPage() {
  const router = useRouter();
  const { email } = useAuth();
  const [file, setFile] = useState<File | null>(null);
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [progress, setProgress] = useState<number | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [doneId, setDoneId] = useState<string | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
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
    try {
      const video = await uploadVideo(file, title || file.name, description, setProgress);
      setDoneId(video.id);
      setProgress(100);
      setTimeout(() => router.push(`/watch/${video.id}`), 800);
    } catch (err) {
      setError((err as Error).message);
      setProgress(null);
    }
  };

  return (
    <div className="mx-auto max-w-xl">
      <h1 className="text-xl font-semibold">Upload video</h1>
      <p className="mt-1 text-sm text-zinc-500">MP4 up to ~500MB. Stored as raw, then transcoded to 3 renditions.</p>

      {!email && (
        <div className="mt-4 rounded border border-yellow-200 bg-yellow-50 p-3 text-sm">
          You are not logged in. <a href="/login" className="font-medium text-yellow-800 underline">Login</a> to upload.
        </div>
      )}

      <form onSubmit={handleSubmit} className="mt-6 space-y-4">
        <div>
          <label className="text-sm font-medium">File (mp4, mov, mkv)</label>
          <input
            type="file"
            accept="video/*"
            onChange={(e) => setFile(e.target.files?.[0] ?? null)}
            className="mt-1 block w-full rounded border p-2 text-sm"
          />
          {file && <p className="mt-1 text-xs text-zinc-500">{file.name} — {(file.size / 1_000_000).toFixed(1)} MB</p>}
        </div>
        <div>
          <label className="text-sm font-medium">Title</label>
          <input value={title} onChange={(e) => setTitle(e.target.value)} placeholder="My video" className="mt-1 w-full rounded border px-3 py-2 text-sm" />
        </div>
        <div>
          <label className="text-sm font-medium">Description</label>
          <textarea value={description} onChange={(e) => setDescription(e.target.value)} rows={3} className="mt-1 w-full rounded border px-3 py-2 text-sm" />
        </div>

        {progress !== null && (
          <div className="h-2 overflow-hidden rounded bg-zinc-200">
            <div className="h-full bg-red-500 transition-all" style={{ width: `${progress}%` }} />
          </div>
        )}
        {error && <div className="rounded bg-red-50 p-3 text-sm text-red-700">{error}</div>}
        {doneId && <div className="rounded bg-green-50 p-3 text-sm text-green-700">Uploaded! Redirecting to player…</div>}

        <button
          type="submit"
          disabled={progress !== null && progress < 100 && progress > 0}
          className="w-full rounded bg-black py-2 text-sm font-medium text-white hover:bg-zinc-800 disabled:opacity-50"
        >
          {progress !== null && progress < 100 ? `Uploading ${progress}%` : "Upload"}
        </button>
      </form>
    </div>
  );
}
