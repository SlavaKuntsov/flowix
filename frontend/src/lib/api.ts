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

export function getHlsUrl(videoId: string, token?: string): string {
  const b = hlsBase();
  const base = `${b}/hls/${videoId}/master.m3u8`;
  if (token) return `${base}?token=${encodeURIComponent(token)}`;
  return base;
}

export async function getHlsToken(videoId: string): Promise<{ token: string; expires_in: number; url: string }> {
  return request<{ token: string; expires_in: number; url: string }>(`/api/v1/videos/${videoId}/hls-token`);
}

export type VideoStatus = "uploaded" | "processing" | "ready" | "failed";
export type Visibility = "public" | "private" | "unlisted";
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
  visibility?: Visibility;
  thumbnail_s3_key?: string | null;
  thumbnail_url?: string | null;
  created_at: string;
  renditions?: Rendition[];
}

export function getThumbnailUrl(video: Video): string | null {
  if (video.thumbnail_url) {
    const b = hlsBase();
    // since issue #43 thumbnail_url is a presigned absolute URL (MinIO loopback);
    // legacy relative paths (gateway) kept as fallback
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

// issue #60: silent refresh on 401 — single-flight, параллельные 401-запросы
// ждут один refresh и ретраятся с новым токеном.
const AUTH_PATHS = ["/api/v1/auth/login", "/api/v1/auth/register", "/api/v1/auth/refresh"];

let refreshPromise: Promise<boolean> | null = null;

async function refreshAccessToken(): Promise<boolean> {
  if (typeof window === "undefined") return false;
  const refreshToken = localStorage.getItem("refresh_token");
  if (!refreshToken) return false;
  const res = await fetch(`${base()}/api/v1/auth/refresh`, {
    method: "POST",
    headers: { Authorization: `Bearer ${refreshToken}` },
  });
  if (!res.ok) return false;
  const data = (await res.json()) as { access_token: string; refresh_token: string };
  localStorage.setItem("access_token", data.access_token);
  localStorage.setItem("refresh_token", data.refresh_token);
  const { useAuth } = await import("@/store/auth");
  useAuth.setState({ token: data.access_token });
  return true;
}

function tryRefresh(): Promise<boolean> {
  if (!refreshPromise) {
    refreshPromise = refreshAccessToken().finally(() => {
      refreshPromise = null;
    });
  }
  return refreshPromise;
}

async function handleRefreshFailure(): Promise<void> {
  if (typeof window === "undefined") return;
  localStorage.removeItem("access_token");
  localStorage.removeItem("refresh_token");
  const { useAuth } = await import("@/store/auth");
  useAuth.setState({ token: null, email: null, userId: null });
}

async function request<T>(path: string, init?: RequestInit, retried = false): Promise<T> {
  const url = `${base()}${path}`;
  const res = await fetch(url, {
    ...init,
    headers: { ...(init?.headers as Record<string, string>), ...authHeaders() },
  });
  if (res.status === 401 && !retried && !AUTH_PATHS.some((p) => path.startsWith(p))) {
    if (await tryRefresh()) {
      // authHeaders() прочитает уже обновлённый access_token из localStorage
      return request<T>(path, init, true);
    }
    await handleRefreshFailure();
  }
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

export async function updateVideo(id: string, data: { title?: string; description?: string; visibility?: Visibility }): Promise<Video> {
  return request<Video>(`/api/v1/videos/${id}`, { method: "PATCH", headers: { "Content-Type": "application/json" }, body: JSON.stringify(data) });
}

// S3 multipart upload (issue #56): the browser PUTs presigned chunks directly
// to MinIO; the server only creates the session, presigns parts and completes.
export const MULTIPART_CHUNK_SIZE = 8 * 1024 * 1024;

export interface MultipartCreateResponse {
  id: string;
  video_id: string;
  s3_key: string;
  upload_id: string;
  chunk_size: number;
}

export interface MultipartPart {
  part_number: number;
  size: number;
  etag: string;
}

export async function createMultipartVideo(title: string, description: string, filename: string, contentType: string): Promise<MultipartCreateResponse> {
  return request<MultipartCreateResponse>("/api/v1/videos/multipart", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ title, description, filename, content_type: contentType }),
  });
}

export async function listMultipartParts(videoId: string): Promise<{ upload_id: string; chunk_size: number; parts: MultipartPart[] }> {
  return request(`/api/v1/videos/${videoId}/multipart`);
}

export async function presignPart(videoId: string, partNumber: number): Promise<{ part_number: number; url: string; expires_in: number }> {
  return request(`/api/v1/videos/${videoId}/multipart/presign-part`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ part_number: partNumber }),
  });
}

export async function completeMultipart(videoId: string): Promise<Video> {
  return request<Video>(`/api/v1/videos/${videoId}/multipart/complete`, { method: "POST" });
}

function putPartToPresignedUrl(
  url: string,
  chunk: Blob,
  onProgress?: (chunkPct: number) => void,
  signal?: AbortSignal,
): Promise<void> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("PUT", url);
    if (signal?.aborted) {
      reject(new DOMException("aborted", "AbortError"));
      return;
    }
    signal?.addEventListener("abort", () => xhr.abort());
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable && onProgress) onProgress(e.loaded / e.total);
    };
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) resolve();
      else reject(new Error(`part PUT failed: ${xhr.status} ${xhr.responseText}`));
    };
    xhr.onerror = () => reject(new Error("network error during part PUT"));
    xhr.onabort = () => reject(new DOMException("aborted", "AbortError"));
    xhr.send(chunk);
  });
}

// Chunks of 5-10MB (issue #56): S3 requires every part except the last to be
// ≥5MB; the server sends its chunk_size, this constant is the fallback.
async function uploadVideoMultipart(
  file: File,
  title: string,
  description: string,
  onProgress?: (pct: number) => void,
  signal?: AbortSignal,
): Promise<Video> {
  const created = await createMultipartVideo(title, description, file.name, file.type || "video/mp4");
  const chunkSize = created.chunk_size || MULTIPART_CHUNK_SIZE;
  const totalParts = Math.max(1, Math.ceil(file.size / chunkSize));
  const lastPartSize = file.size - (totalParts - 1) * chunkSize;
  const partSize = (n: number) => (n === totalParts ? lastPartSize : chunkSize);

  // resume: skip parts already uploaded in a previous attempt (issue #56)
  const uploadedParts = new Set<number>();
  try {
    const state = await listMultipartParts(created.video_id);
    state.parts.forEach((p) => uploadedParts.add(p.part_number));
  } catch {}
  let uploadedBytes = 0;
  for (let n = 1; n <= totalParts; n++) {
    if (uploadedParts.has(n)) uploadedBytes += partSize(n);
  }
  const report = () => onProgress?.(Math.min(99, Math.round((uploadedBytes / file.size) * 100)));
  report();

  for (let n = 1; n <= totalParts; n++) {
    if (uploadedParts.has(n)) continue;
    const start = (n - 1) * chunkSize;
    const chunk = file.slice(start, start + partSize(n));
    const { url } = await presignPart(created.video_id, n);
    let lastErr: unknown;
    for (let attempt = 0; attempt < 3; attempt++) {
      try {
        await putPartToPresignedUrl(
          url,
          chunk,
          onProgress
            ? (chunkPct) => onProgress(Math.min(99, Math.round(((uploadedBytes + chunk.size * chunkPct) / file.size) * 100)))
            : undefined,
          signal,
        );
        lastErr = null;
        break;
      } catch (e) {
        lastErr = e;
        if ((e as DOMException).name === "AbortError") throw e;
        if (attempt < 2) await new Promise((r) => setTimeout(r, 500 * (attempt + 1)));
      }
    }
    if (lastErr) throw lastErr instanceof Error ? lastErr : new Error(String(lastErr));
    uploadedBytes += chunk.size;
    report();
  }
  return completeMultipart(created.video_id);
}

// Upload: S3 multipart for all sizes; legacy gateway proxy fallback for <100MB
export async function uploadVideo(
  file: File,
  title: string,
  description: string,
  onProgress?: (pct: number) => void,
  signal?: AbortSignal,
): Promise<Video> {
  try {
    return await uploadVideoMultipart(file, title, description, onProgress, signal);
  } catch (e) {
    if (signal?.aborted) throw e;
    // fallback to legacy gateway upload for small files if multipart flow fails
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
