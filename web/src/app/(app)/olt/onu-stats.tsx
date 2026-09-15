"use client";

// Statistik ONU — card modern dengan ikon (lucide), bukan emoji.
// Grid responsif: 2 kolom mobile, 4 desktop.

import { useEffect } from "react";
import { Activity, CircleSlash, Gauge, WifiOff } from "lucide-react";
import { clientAPI } from "@/lib/api/client";
import type { OLT } from "@/lib/olts/types";

export type OnuStats = {
  total: number;
  working: number;
  los: number;
  offline: number;
  other: number;
  pon_ports: { port: string; count: number }[];
};

const cards = [
  { key: "total", label: "Total ONU", icon: Gauge, accent: "#2c7a5b", bg: "#f0f7f4" },
  { key: "working", label: "Online", icon: Activity, accent: "#059669", bg: "#ecfdf5" },
  { key: "los", label: "LOS", icon: WifiOff, accent: "#dc2626", bg: "#fef2f2" },
  { key: "offline", label: "Offline", icon: CircleSlash, accent: "#64748b", bg: "#f8fafc" },
] as const;

export function OnuStatsBar({
  olt,
  stats,
  setStats,
  live,
}: {
  olt: OLT;
  stats: OnuStats | null;
  setStats: (s: OnuStats | null) => void;
  live: boolean;
}) {
  useEffect(() => {
    let cancelled = false;
    let controller: AbortController | null = null;
    async function load() {
      if (cancelled || controller || document.hidden) return;
      controller = new AbortController();
      try {
        const result = await clientAPI<OnuStats>(`/api/v1/olts/${olt.id}/onus-stats`, { signal: controller.signal });
        if (!cancelled) setStats(result);
      } catch { /* abaikan */ }
      finally { controller = null; }
    }
    void load();
    const timer = setInterval(load, 5000);
    return () => { cancelled = true; clearInterval(timer); controller?.abort(); };
  }, [olt.id, setStats]);

  const value = (key: string): number => {
    if (!stats) return 0;
    if (key === "offline") return stats.offline + stats.other;
    return stats[key as keyof OnuStats] as number;
  };

  return (
    <div className="mb-4 space-y-3">
      {/* Card statistik */}
      <div className="grid grid-cols-2 gap-2.5 sm:grid-cols-4">
        {cards.map(({ key, label, icon: Icon, accent, bg }) => (
          <div
            key={key}
            className="flex items-center gap-3 rounded-xl border border-[#e5eeea] bg-white p-3.5 shadow-sm transition-shadow hover:shadow-md"
            style={key === "total" ? { borderColor: "#cfe3db" } : undefined}
          >
            <span
              className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg"
              style={{ backgroundColor: bg, color: accent }}
            >
              <Icon size={19} strokeWidth={2.2} />
            </span>
            <div className="min-w-0">
              <p className="truncate text-xs font-medium text-[#8aa096]">{label}</p>
              <p className="font-mono text-lg font-bold leading-tight text-[#10251d]">
                {value(key).toLocaleString("id-ID")}
              </p>
            </div>
          </div>
        ))}
      </div>

      {/* indikator live */}
      {live ? (
        <div className="flex justify-end">
          <span className="inline-flex items-center gap-1.5 rounded-full bg-emerald-50 px-2.5 py-1 text-xs font-semibold text-emerald-700">
            <span className="relative flex h-2 w-2">
              <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-75" />
              <span className="relative inline-flex h-2 w-2 rounded-full bg-emerald-500" />
            </span>
            Live
          </span>
        </div>
      ) : null}
    </div>
  );
}
