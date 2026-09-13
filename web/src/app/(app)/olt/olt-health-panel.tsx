"use client";

// Panel Detail OLT: kesehatan fisik (card/CPU/mem/temp/fan/PSU/SFP).
// Polling 15 detik, sinkron dengan cache server — OLT tidak dibebani.
// Loading: skeleton shimmer; error: inline, tidak crash halaman.

import { useCallback, useEffect, useState } from "react";
import { Activity, Cpu, Fan, Gauge, Thermometer, Zap } from "lucide-react";
import { clientAPI } from "@/lib/api/client";
import type { OLT } from "@/lib/olts/types";

type CardInfo = {
  slot: number; type?: string; status?: string; role?: string;
  cpu_percent: number; mem_percent: number; temp_c: number;
};
type FanInfo = { id: number; speed_rpm: number };
type PsuInfo = { id: number; voltage: number };
type SfpInfo = {
  index: number; label?: string; rx_power_dbm: number; tx_power_dbm: number;
  bias_ma: number; voltage_v: number; temp_c: number;
  model?: string; vendor?: string; wavelength_nm?: number;
  in_bps?: number; out_bps?: number;
};
type Health = { cards: CardInfo[]; fans?: FanInfo[]; psus?: PsuInfo[]; sfps: SfpInfo[] };

function fmtMbps(bps: number): string {
  if (!bps || bps <= 0) return "0";
  const m = bps / 1e6;
  return m >= 100 ? m.toFixed(0) : m.toFixed(1);
}

function SkeletonCard() {
  return (
    <div className="animate-pulse rounded-xl border border-[#e5eeea] bg-white p-4 shadow-sm">
      <div className="mb-3 h-4 w-32 rounded bg-[#eef4f1]" />
      <div className="space-y-2">
        <div className="h-3 w-full rounded bg-[#f0f5f2]" />
        <div className="h-3 w-5/6 rounded bg-[#f0f5f2]" />
        <div className="h-3 w-4/6 rounded bg-[#f0f5f2]" />
      </div>
    </div>
  );
}

function TempBadge({ value }: { value: number }) {
  if (!value) return <span className="text-[#a8bcb2]">—</span>;
  const color = value > 65 ? "text-red-600 bg-red-50" : value > 55 ? "text-amber-600 bg-amber-50" : "text-emerald-700 bg-emerald-50";
  return (
    <span className={`inline-flex items-center gap-1 rounded px-1.5 py-0.5 font-mono text-xs font-semibold ${color}`}>
      <Thermometer size={11} />{value.toFixed(0)}°C
    </span>
  );
}

function PercentBar({ label, value, icon: Icon }: { label: string; value: number; icon: React.ComponentType<{ size?: number }> }) {
  const color = value > 85 ? "bg-red-500" : value > 70 ? "bg-amber-500" : "bg-emerald-500";
  return (
    <div>
      <div className="mb-1 flex items-center justify-between text-xs">
        <span className="flex items-center gap-1 text-[#607067]"><Icon size={12} /> {label}</span>
        <span className="font-mono font-semibold">{value.toFixed(0)}%</span>
      </div>
      <div className="h-1.5 w-full overflow-hidden rounded-full bg-[#eef4f1]">
        <div className={`h-full rounded-full transition-all ${color}`} style={{ width: `${Math.min(100, value)}%` }} />
      </div>
    </div>
  );
}

export function OltHealthPanel({ olt }: { olt: OLT }) {
  const [health, setHealth] = useState<Health | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    try {
      setError(null);
      const result = await clientAPI<Health>(`/api/v1/olts/${olt.id}/health`);
      setHealth(result);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Gagal memuat.");
    } finally {
      setLoading(false);
    }
  }, [olt.id]);

  // Load awal + polling ringan tiap 15 detik (selaras cache server)
  useEffect(() => {
    void load();
    const timer = setInterval(() => void load(), 15000);
    return () => clearInterval(timer);
  }, [load]);

  if (loading && !health) {
    return (
      <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
        <SkeletonCard /><SkeletonCard />
      </div>
    );
  }

  if (error) {
    return (
      <div className="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-800">
        {error}
      </div>
    );
  }

  if (!health) return null;

  return (
    <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
      {/* ── Cards & Resource ── */}
      <div className="rounded-xl border border-[#e5eeea] bg-white p-4 shadow-sm">
        <h4 className="mb-3 flex items-center gap-2 text-sm font-bold text-[#10251d]">
          <Cpu size={15} /> Card &amp; Resource
        </h4>
        <div className="space-y-3">
          {health.cards.map((card) => (
            <div key={card.slot} className="rounded-lg border border-[#f0f5f2] p-3">
              <div className="mb-2 flex flex-wrap items-center justify-between gap-1.5">
                <span className="text-sm font-bold">Slot {card.slot}{card.type ? ` — ${card.type}` : ""}</span>
                <span className={`rounded-full px-2 py-0.5 text-xs font-semibold ${
                  card.status === "InService" ? "bg-emerald-50 text-emerald-700"
                  : card.status === "Standby" ? "bg-sky-50 text-sky-700"
                  : "bg-slate-100 text-slate-600"}`}>
                  {card.status}{card.role ? ` (${card.role})` : ""}
                </span>
              </div>
              {(card.cpu_percent > 0 || card.mem_percent > 0) ? (
                <div className="grid grid-cols-2 gap-x-4 gap-y-2">
                  <PercentBar label="CPU" value={card.cpu_percent} icon={Cpu} />
                  <PercentBar label="Memory" value={card.mem_percent} icon={Gauge} />
                </div>
              ) : null}
              {card.temp_c > 0 ? (
                <div className="mt-2 flex items-center gap-2 text-xs text-[#607067]">
                  <Thermometer size={12} /> Suhu: <TempBadge value={card.temp_c} />
                </div>
              ) : null}
            </div>
          ))}
          {health.fans && health.fans.length > 0 ? (
            <div className="flex flex-wrap gap-2 rounded-lg bg-[#f6faf8] p-2.5 text-xs">
              <Fan size={13} className="self-center text-[#607067]" />
              {health.fans.map((fan) => (
                <span key={fan.id} className="rounded bg-white px-2 py-1 font-mono shadow-sm">
                  Fan{fan.id}: {fan.speed_rpm.toLocaleString("id-ID")} RPM
                </span>
              ))}
            </div>
          ) : null}
          {health.psus && health.psus.length > 0 ? (
            <div className="flex flex-wrap gap-2 rounded-lg bg-[#f6faf8] p-2.5 text-xs">
              <Zap size={13} className="self-center text-[#607067]" />
              {health.psus.map((psu) => (
                <span key={psu.id} className="rounded bg-white px-2 py-1 font-mono shadow-sm">
                  PSU{psu.id}: {psu.voltage.toFixed(1)}V
                </span>
              ))}
            </div>
          ) : null}
          {health.cards.length === 0 && (!health.fans || health.fans.length === 0) ? (
            <p className="text-sm text-[#8aa096]">Firmware tidak mengekspos data card/resource.</p>
          ) : null}
        </div>
      </div>

      {/* ── SFP / PON Diagnostics ── */}
      <div className="rounded-xl border border-[#e5eeea] bg-white p-4 shadow-sm">
        <h4 className="mb-3 flex items-center gap-2 text-sm font-bold text-[#10251d]">
          <Activity size={15} /> SFP / PON Diagnostics
        </h4>
        {health.sfps.length === 0 ? (
          <p className="text-sm text-[#8aa096]">Tidak ada data SFP terbaca.</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-xs">
              <thead>
                <tr className="border-b border-[#eef4f1] text-left text-[#8aa096]">
                  <th className="py-1.5 pr-3 font-semibold">PON</th>
                  <th className="px-2 py-1.5 text-right font-semibold">Rx</th>
                  <th className="px-2 py-1.5 text-right font-semibold">Tx</th>
                  <th className="px-2 py-1.5 text-right font-semibold">Bias</th>
                  <th className="px-2 py-1.5 text-right font-semibold">Temp</th>
                  <th className="px-2 py-1.5 text-right font-semibold">Trafik</th>
                  {health.sfps.some((s) => s.model) ? <th className="px-2 py-1.5 font-semibold">Model</th> : null}
                </tr>
              </thead>
              <tbody className="divide-y divide-[#f0f5f2]">
                {health.sfps.map((sfp) => (
                  <tr key={sfp.index}>
                    <td className="py-1.5 pr-3 font-mono font-semibold">{sfp.label || sfp.index}</td>
                    <td className={`px-2 py-1.5 text-right font-mono ${sfp.rx_power_dbm > -8 ? "text-red-600" : sfp.rx_power_dbm < -28 ? "text-amber-600" : "text-emerald-700"}`}>
                      {sfp.rx_power_dbm.toFixed(1)}
                    </td>
                    <td className="px-2 py-1.5 text-right font-mono text-[#44554d]">{sfp.tx_power_dbm.toFixed(1)}</td>
                    <td className="px-2 py-1.5 text-right font-mono text-[#44554d]">{sfp.bias_ma.toFixed(0)}mA</td>
                    <td className="px-2 py-1.5 text-right"><TempBadge value={sfp.temp_c} /></td>
                    <td className="px-2 py-1.5 text-right font-mono text-[#44554d] whitespace-nowrap" title="Downstream / upstream agregat port (Mbps)">
                      ↓{fmtMbps(sfp.in_bps ?? 0)} <span className="text-[#8aa096]">·</span> ↑{fmtMbps(sfp.out_bps ?? 0)}
                    </td>
                    {health.sfps.some((s) => s.model) ? (
                      <td className="max-w-[110px] truncate px-2 py-1.5 text-[#607067]" title={`${sfp.vendor ?? ""} ${sfp.model ?? ""}`}>
                        {sfp.model || "—"}
                      </td>
                    ) : null}
                  </tr>
                ))}
              </tbody>
            </table>
            <p className="mt-2 text-[10px] text-[#8aa096]">Nilai Rx/Tx dalam dBm · Bias mA · Suhu °C</p>
          </div>
        )}
      </div>
    </div>
  );
}
