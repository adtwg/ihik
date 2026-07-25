import { cookies } from "next/headers";
import { APIError, type APIErrorBody, type Principal } from "./types";

const apiBaseURL = process.env.API_INTERNAL_URL ?? "http://localhost:8080";

export async function serverAPI<T>(path: string): Promise<T> {
  const cookieStore = await cookies();
  const response = await fetch(`${apiBaseURL}${path}`, {
    cache: "no-store",
    headers: { Cookie: cookieStore.toString(), Accept: "application/json" },
  });
  if (!response.ok) {
    const body = (await response.json().catch(() => ({}))) as APIErrorBody;
    throw new APIError(response.status, body.error?.code ?? "request_failed", body.error?.message ?? "Request gagal.");
  }
  return response.json() as Promise<T>;
}

export function getCurrentUser(): Promise<Principal> {
  return serverAPI<Principal>("/api/v1/me");
}