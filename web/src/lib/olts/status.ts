export const ONLINE_STATUS = new Set(["working", "logging", "sync_mib", "online", "ready"]);

export function normalizeStatusKey(raw?: string): string {
  const v = String(raw || "").trim().toLowerCase().replace(/\s+/g, "_");
  if (!v) return "unknown";
  if (v.includes("work")) return "working";
  if (v.includes("dying")) return "dying_gasp";
  if (v.includes("los")) return "los";
  if (v.includes("offline")) return "offlined";
  if (v.includes("sync")) return "sync_mib";
  if (v.includes("auth")) return "auth_failed";
  return v;
}
