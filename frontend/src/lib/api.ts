const API_BASE =
  process.env.NEXT_PUBLIC_API_URL?.replace(/\/$/, "") || "";
// In browser, relative URLs go through next rewrites -> gateway. In SSR, use gateway directly.
function base(): string {
  if (typeof window !== "undefined") return API_BASE || "";
  return process.env.GATEWAY_URL?.replace(/\/$/, "") || API_BASE || "http://localhost:8080";
}

function hlsBase(): string {
  // Prefer gateway /hls (proxied to nginx-vod). Fallback to direct vod.
  if (typeof window !== "undefined") return API_BASE || "";
  return process.env.GATEWAY_URL?.replace(/\/$/, "") || API_BASE || "http://localhost:8080";
}

export function getHlsUrl(videoId: string): string {
  const b = hlsBase();
  // nginx-vod mapped mode serves HLS at /hls/{id}/master.m3u8 (with trailing structure)
  // Actual nginx-vod config: location ~ "^/hls/[0-9a-fA-F-]{36}/" with vod hls; expects /hls/{id}/master.m3u8
  return `${b}/hls/${videoId}/master.m3u8`;
}

export type VideoStatus = "uploaded" | "processing" | "ready" | "failed";
export interface Rendition {
  video_id: string;
  quality: string;
  bitrate: number;
  width: number;
  height: number;
  s3_key: string;
}
export interface Video {
  id: string;
  owner_id: string;
  owner_email?: string | null;
  title: string;
  description: string;
  duration?: number | null;
  status: VideoStatus;
  thumbnail_s3_key?: string | null;
  thumbnail_url?: string | null;
  created_at: string;
  renditions?: Rendition[];
}

export function getThumbnailUrl(video: Video): string | null {
  if (video.thumbnail_url) {
    const b = hlsBase();
    // thumbnail_url is like /thumbnails/{id}/thumb.jpg (gateway)
    if (video.thumbnail_url.startsWith("/")) return `${b}${video.thumbnail_url}`;
    return video.thumbnail_url;
  }
  if (video.thumbnail_s3_key) {
    return `${hlsBase()}/${video.thumbnail_s3_key}`;
  }
  return null;
}

interface ListResponse {
  data: Video[];
  limit: number;
  offset: number;
}

function authHeaders(): Record<string, string> {
  if (typeof window === "undefined") return {};
  const token = localStorage.getItem("access_token");
  return token ? { Authorization: `Bearer ${token}` } : {};
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const url = `${base()}${path}`;
  const res = await fetch(url, {
    ...init,
    headers: { ...(init?.headers as Record<string, string>), ...authHeaders() },
  });
  if (!res.ok) {
    const text = await res.text().catch(() => "");
    let msg = text;
    try {
      const j = JSON.parse(text);
      msg = j.error || j.detail || j.message || text;
    } catch { }
    throw new Error(msg || `${res.status} ${res.statusText}`);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

// Auth
export async function register(email: string, password: string) {
  return request<{ access_token: string; refresh_token: string }>("/api/v1/auth/register", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email, password }),
  });
}
export async function login(email: string, password: string) {
  return request<{ access_token: string; refresh_token: string }>("/api/v1/auth/login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email, password }),
  });
}
export async function fetchMe(): Promise<{ id: string; email: string }> {
  return request("/api/v1/auth/me");
}

// Videos
export async function listVideos(limit = 20, offset = 0): Promise<ListResponse> {
  return request<ListResponse>(`/api/v1/videos?limit=${limit}&offset=${offset}`);
}
export async function getVideo(id: string): Promise<Video> {
  return request<Video>(`/api/v1/videos/${id}`);
}
export async function createVideoMeta(title: string, description: string): Promise<Video> {
  return request<Video>("/api/v1/videos", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ title, description }),
  });
}
export async function deleteVideo(id: string): Promise<void> {
  return request<void>(`/api/v1/videos/${id}`, { method: "DELETE" });
}

// Presigned upload: direct PUT to MinIO (Phase 11) — gateway only creates presign, file bypasses Go proxy
export interface PresignResponse {
  id: string;
  video_id: string;
  s3_key: string;
  method: string;
  url: string;
  expires_in: number;
}

export async function presignVideo(title: string, description: string, filename: string, contentType: string): Promise<PresignResponse> {
  return request<PresignResponse>("/api/v1/videos/presign", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ title, description, filename, content_type: contentType }),
  });
}

export async function completeVideo(videoId: string): Promise<Video> {
  return request<Video>(`/api/v1/videos/${videoId}/complete`, { method: "POST" });
}

function putToPresignedUrl(
  url: string,
  file: File,
  contentType: string,
  onProgress?: (pct: number) => void,
  signal?: AbortSignal,
): Promise<void> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("PUT", url);
    if (contentType) xhr.setRequestHeader("Content-Type", contentType);
    if (signal?.aborted) {
      reject(new DOMException("aborted", "AbortError"));
      return;
    }
    signal?.addEventListener("abort", () => xhr.abort());
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable && onProgress) onProgress(Math.round((e.loaded / e.total) * 100));
    };
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) resolve();
      else reject(new Error(`presigned PUT failed: ${xhr.status} ${xhr.responseText}`));
    };
    xhr.onerror = () => reject(new Error("network error during presigned PUT"));
    xhr.onabort = () => reject(new DOMException("aborted", "AbortError"));
    xhr.send(file);
  });
}

// Upload: presigned direct to MinIO with fallback to legacy gateway proxy for <100MB
export async function uploadVideo(
  file: File,
  title: string,
  description: string,
  onProgress?: (pct: number) => void,
  signal?: AbortSignal,
): Promise<Video> {
  // Phase 11: use presigned PUT for all files; legacy fallback if presign fails
  const storageKey = `presign:${file.name}:${file.size}:${file.lastModified}`;
  try {
    let presign: PresignResponse | null = null;
    // resume: reuse pending presign from localStorage if exists and not expired
    if (typeof window !== "undefined") {
      try {
        const cached = localStorage.getItem(storageKey);
        if (cached) {
          const parsed = JSON.parse(cached) as PresignResponse & { _ts: number };
          if (Date.now() - parsed._ts < parsed.expires_in * 1000 - 60000) {
            presign = parsed;
          } else {
            localStorage.removeItem(storageKey);
          }
        }
      } catch {}
    }
    if (!presign) {
      presign = await presignVideo(title || file.name, description, file.name, file.type || "video/mp4");
      if (typeof window !== "undefined") {
        try {
          localStorage.setItem(storageKey, JSON.stringify({ ...presign, _ts: Date.now() }));
        } catch {}
      }
    }
    // retry PUT up to 3 times
    let lastErr: unknown;
    for (let attempt = 0; attempt < 3; attempt++) {
      try {
        await putToPresignedUrl(presign.url, file, file.type || "video/mp4", onProgress, signal);
        lastErr = null;
        break;
      } catch (e) {
        lastErr = e;
        if ((e as DOMException).name === "AbortError") throw e;
        if (attempt < 2) await new Promise((r) => setTimeout(r, 500 * (attempt + 1)));
      }
    }
    if (lastErr) throw lastErr;
    const video = await completeVideo(presign.video_id);
    if (typeof window !== "undefined") localStorage.removeItem(storageKey);
    return video;
  } catch (e) {
    // fallback to legacy gateway upload for small files if presign flow fails (e.g., server not updated)
    if (file.size < 100 * 1024 * 1024) {
      return uploadViaGateway(file, title, description, onProgress);
    }
    throw e instanceof Error ? e : new Error(String(e));
  }
}

function uploadViaGateway(
  file: File,
  title: string,
  description: string,
  onProgress?: (pct: number) => void,
): Promise<Video> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    const url = `${base()}/api/v1/videos/upload`;
    xhr.open("POST", url);
    const token = typeof window !== "undefined" ? localStorage.getItem("access_token") : null;
    if (token) xhr.setRequestHeader("Authorization", `Bearer ${token}`);
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable && onProgress) onProgress(Math.round((e.loaded / e.total) * 100));
    };
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        try {
          resolve(JSON.parse(xhr.responseText) as Video);
        } catch {
          reject(new Error("invalid json response"));
        }
      } else {
        let msg = xhr.responseText;
        try {
          const j = JSON.parse(xhr.responseText);
          msg = j.error || j.detail || msg;
        } catch {}
        reject(new Error(msg || `upload failed: ${xhr.status}`));
      }
    };
    xhr.onerror = () => reject(new Error("network error"));
    const fd = new FormData();
    fd.append("file", file);
    if (title) fd.append("title", title);
    if (description) fd.append("description", description);
    xhr.send(fd);
  });
}
