import { APIError, type APIErrorBody } from "./types";

function cookieValue(name: string): string | undefined {
  return document.cookie
    .split("; ")
    .find((item) => item.startsWith(`${name}=`))
    ?.slice(name.length + 1);
}

export function activeImpersonatedTenant(): string | undefined {
  return cookieValue("impersonate_tenant");
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
  const impersonated = activeImpersonatedTenant();
  if (impersonated) headers.set("X-On-Behalf-Tenant", decodeURIComponent(impersonated));

  const isLongRunning =
    path.includes("/sync-onus") ||
    path.includes("/sync-optical") ||
    path.includes("/onu-sync") ||
    path.includes("/onu-traffic-cli") ||
    path.includes("/onu-detail-cli") ||
    path.includes("/onus") ||
    path.includes("/refresh-traffic") ||
    path.includes("/onu-config-cli");
  // Timeout harus selalu lebih pendek dari backend timeout, supaya browser/gateway
  // tidak mencapai idle timeout sebelum API sempat merespons.
  const timeoutMs = isLongRunning ? 35000 : method === "GET" ? 20000 : 18000;

  const maxAttempts = method === "GET" ? 3 : 1;
  let response: Response | null = null;
  let lastError: unknown = null;

  function isRetriable(error: unknown): boolean {
    if (!(error instanceof Error)) return false;
    const isAbort = error.name === "AbortError" || /aborted/i.test(error.message);
    const isNetwork = error instanceof TypeError;
    return method === "GET" && (isNetwork || isAbort);
  }

  for (let attempt = 1; attempt <= maxAttempts; attempt += 1) {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), timeoutMs);
    try {
      const requestInit: RequestInit = { ...init, headers, credentials: "include", signal: controller.signal };
      if (method === "GET") {
        requestInit.cache = "no-store";
        headers.set("Cache-Control", "no-cache, no-store, must-revalidate");
        headers.set("Pragma", "no-cache");
        headers.set("Expires", "0");
      }

      response = await fetch(path, requestInit);
      lastError = null;
      break;
    } catch (error) {
      lastError = error;
      if (!isRetriable(error) || attempt >= maxAttempts) {
        break;
      }
      await new Promise((resolve) => setTimeout(resolve, attempt * 700));
    } finally {
      clearTimeout(timeout);
    }
  }

  if (!response) {
    if (lastError instanceof Error && (lastError.name === "AbortError" || /aborted/i.test(lastError.message))) {
      throw new Error("Request timeout. Data sedang sinkron, coba refresh lagi.");
    }
    throw lastError instanceof Error ? lastError : new Error("Request gagal.");
  }

  if (!response.ok) {
    const raw = await response.text();
    const body = (() => {
      try {
        return JSON.parse(raw || "{}") as APIErrorBody;
      } catch {
        return {} as APIErrorBody;
      }
    })();
    const fallback = raw.trim()
      ? `HTTP ${response.status}: ${raw.replace(/\s+/g, " ").slice(0, 220)}`
      : `HTTP ${response.status} ${response.statusText || "Request gagal."}`;
    // 409 CONFLICT dari backend berarti OLT sedang dipakai sesi CLI lain.
    // Kita kasih pesan yang jelas agar UI tidak tampak macet.
    if (response.status === 409 && body.error?.code === "cli_busy") {
      throw new APIError(response.status, "cli_busy", body.error.message ?? "OLT sedang dipakai sesi CLI lain, coba beberapa saat lagi.");
    }
    throw new APIError(response.status, body.error?.code ?? "request_failed", body.error?.message ?? fallback);
  }

  if (response.status === 204) return undefined as T;
  return response.json() as Promise<T>;
}
