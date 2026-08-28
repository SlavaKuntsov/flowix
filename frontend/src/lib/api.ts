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
  title: string;
  description: string;
  duration?: number | null;
  status: VideoStatus;
  created_at: string;
  renditions?: Rendition[];
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

// Upload: multipart via gateway -> upload service (requires auth)
export function uploadVideo(
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
        } catch { }
        reject(new Error(msg || `upload failed: ${xhr.status}`));
      }
    };
    xhr.onerror = () => reject(new Error("network error"));
    const fd = new FormData();
    fd.append("file", file);
    // upload service expects file field; title/description optional via query? Check handler: form file + title?
    // Upload handler reads title from form if present, else filename.
    if (title) fd.append("title", title);
    if (description) fd.append("description", description);
    xhr.send(fd);
  });
}
