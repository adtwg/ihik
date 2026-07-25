import { APIError, type APIErrorBody } from "./types";

function cookieValue(name: string): string | undefined {
  return document.cookie
    .split("; ")
    .find((item) => item.startsWith(`${name}=`))
    ?.slice(name.length + 1);
}

export async function clientAPI<T>(path: string, init: RequestInit = {}): Promise<T> {
  const method = (init.method ?? "GET").toUpperCase();
  const headers = new Headers(init.headers);
  headers.set("Accept", "application/json");
  if (init.body) headers.set("Content-Type", "application/json");
  if (!["GET", "HEAD", "OPTIONS"].includes(method)) {
    const csrfToken = cookieValue("isp_csrf");
    if (csrfToken) headers.set("X-CSRF-Token", decodeURIComponent(csrfToken));
  }
  const response = await fetch(path, { ...init, headers, credentials: "include" });
  if (!response.ok) {
    const body = (await response.json().catch(() => ({}))) as APIErrorBody;
    throw new APIError(response.status, body.error?.code ?? "request_failed", body.error?.message ?? "Request gagal.");
  }
  if (response.status === 204) return undefined as T;
  return response.json() as Promise<T>;
}