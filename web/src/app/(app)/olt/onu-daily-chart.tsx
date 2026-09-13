"use client";

// Grafik trafik ONU — gaya Grafana, tanpa dekorasi.
// Utama: intraday (sampel bps per menit, naik-turun). Sekunder: tabel
// peak harian 30 hari.

import { useCallback, useEffect, useState } from "react";
import { X } from "lucide-react";
import { clientAPI } from "@/lib/api/client";
import type { OLT } from "@/lib/olts/types";

type DailyRow = {
  day: string;
  in_bps_peak: number;
  out_bps_peak: number;
  in_bytes_total: number;
  out_bytes_total: number;
  samples: number;
};

type SampleRow = { t: number; in_bps: number; out_bps: number };

function fmt(v: number): string {
  if (v >= 1e9) return `${(v / 1e9).toFixed(1)}G`;
  if (v >= 1e6) return `${(v / 1e6).toFixed(1)}M`;
  if (v >= 1e3) return `${(v / 1e3).toFixed(0)}K`;
  return `${Math.round(v)}`;
}

function hhmm(unixSec: number): string {
  const d = new Date(unixSec * 1000);
  return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}

const W = 640;
const H = 220;
const PAD_L = 48;
const PAD_B = 24;
const PAD_T = 10;

function Sparkline({ points, maxV, color }: { points: { x: number; y: number }[]; maxV: number; color: string }) {
  if (points.length < 2) return null;
  const d = points.map((p, i) => `${i === 0 ? "M" : "L"}${p.x.toFixed(1)},${p.y.toFixed(1)}`).join(" ");
  const area = `${d} L${points[points.length - 1].x.toFixed(1)},${(H - PAD_B).toFixed(1)} L${points[0].x.toFixed(1)},${(H - PAD_B).toFixed(1)} Z`;
  return (
    <>
      <path d={area} fill={color} opacity={0.07} />
      <path d={d} fill="none" stroke={color} strokeWidth="1.5" />
    </>
  );
}

export function OnuDailyChart({ olt, index, onClose }: { olt: OLT; index: string; onClose: () => void }) {
  const [rows, setRows] = useState<DailyRow[]>([]);
  const [intra, setIntra] = useState<SampleRow[]>([]);
  const [minutes, setMinutes] = useState(180);
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const query = new URLSearchParams({ index, days: "30" });
      const result = await clientAPI<{ items: DailyRow[] }>(`/api/v1/olts/${olt.id}/onus/daily?${query.toString()}`);
      setRows(result.items ?? []);
    } catch { /* ignore */ }
    try {
      const q2 = new URLSearchParams({ index, minutes: String(minutes) });
      const r2 = await clientAPI<{ items: SampleRow[] }>(`/api/v1/olts/${olt.id}/onus/intraday?${q2.toString()}`);
      setIntra(r2.items ?? []);
    } catch { /* ignore */ } finally {
      setLoading(false);
    }
  }, [olt.id, index, minutes]);

  useEffect(() => { void load(); }, [load]);

  // Refresh otomatis saat jendela intraday (tidur ringan 30 dtk).
  useEffect(() => {
    if (minutes > 30) return;
    const timer = setInterval(() => void load(), 30000);
    return () => clearInterval(timer);
  }, [load, minutes]);

  const plotW = W - PAD_L - 10;
  const plotH = H - PAD_B - PAD_T;

  // Series intraday: sumbang per titik, x = waktu relatif.
  const maxIntra = Math.max(1, ...intra.map((r) => Math.max(r.in_bps, r.out_bps)));
  const t0 = intra.length ? intra[0].t : 0;
  const t1 = intra.length ? intra[intra.length - 1].t : 1;
  const span = Math.max(1, t1 - t0);
  const toPts = (key: "in_bps" | "out_bps") =>
    intra.map((r) => ({ x: PAD_L + ((r.t - t0) / span) * plotW, y: PAD_T + plotH - (r[key] / maxIntra) * plotH }));

  // Fallback harian (beberapa hari).
  const maxDaily = Math.max(1, ...rows.map((r) => Math.max(r.in_bps_peak, r.out_bps_peak)));
  const step = rows.length > 1 ? plotW / (rows.length - 1) : plotW;
  const toDaily = (key: "in_bps_peak" | "out_bps_peak") =>
    rows.map((r, i) => ({ x: PAD_L + i * step, y: PAD_T + plotH - (r[key] / maxDaily) * plotH }));

  const showIntra = intra.length >= 2;
  const showDaily = !showIntra && rows.length >= 2;
  const axisMax = showIntra ? maxIntra : maxDaily;
  const gridlines = [0.25, 0.5, 0.75, 1].map((f) => ({
    y: PAD_T + plotH - f * plotH,
    label: fmt(axisMax * f),
  }));

  return (
    <div className="fixed inset-0 z-50 flex items-end justify-center bg-black/40 sm:items-center sm:p-4" role="dialog" aria-modal="true">
      <div className="flex h-[85vh] w-full flex-col rounded-t-lg bg-white shadow-xl sm:h-auto sm:max-h-[85vh] sm:max-w-3xl sm:rounded-lg">
        <header className="flex items-center justify-between border-b border-[#e5eeea] px-4 py-2.5">
          <div>
            <h2 className="text-sm font-bold">Trafik — {index}</h2>
            <p className="text-xs text-[#8aa096]">Sampel per tick trafik live · puncak {fmt(showIntra ? maxIntra : maxDaily)} bps</p>
          </div>
          <button className="rounded-lg p-1.5 text-[#8aa096] hover:bg-[#f1f7f4]" onClick={onClose} aria-label="Tutup"><X size={16} /></button>
        </header>
        <div className="flex-1 overflow-y-auto px-4 py-3">
          <div className="mb-2 flex items-center gap-2 text-xs">
            <span className="flex items-center gap-1.5"><span className="inline-block h-0.5 w-4 bg-emerald-600" /> unduh</span>
            <span className="flex items-center gap-1.5"><span className="inline-block h-0.5 w-4 bg-sky-600" /> unggah</span>
            <div className="ml-auto flex gap-1">
              {[15, 60, 180, 720].map((m) => (
                <button
                  key={m}
                  onClick={() => setMinutes(m)}
                  className={"rounded-md px-2 py-1 " + (minutes === m ? "bg-[#eaf5ef] font-semibold text-[#1d5c43]" : "text-[#8aa096] hover:bg-[#f6faf8]")}
                >
                  {m >= 60 ? `${m / 60}j` : `${m}m`}
                </button>
              ))}
            </div>
          </div>

          {loading && !intra.length ? (
            <div className="flex h-40 items-center justify-center text-xs text-[#8aa096]">Memuat…</div>
          ) : !showIntra && !showDaily ? (
            <div className="flex h-40 items-center justify-center text-center text-xs text-[#8aa096]">
              Belum ada sampel. Nyalakan <b>Trafik Live</b> di tabel ONU — grafik intraday terisi otomatis tiap beberapa detik.
            </div>
          ) : (
            <svg viewBox={`0 0 ${W} ${H}`} className="w-full" style={{ maxHeight: 260 }}>
              {gridlines.map((g) => (
                <g key={g.y}>
                  <line x1={PAD_L} y1={g.y} x2={W - 10} y2={g.y} stroke="#eef4f1" strokeWidth="1" />
                  <text x={PAD_L - 6} y={g.y + 3} textAnchor="end" fontSize="9" fill="#8aa096">{g.label}</text>
                </g>
              ))}
              {showIntra ? (
                <>
                  <Sparkline points={toPts("in_bps")} maxV={maxIntra} color="#059669" />
                  <Sparkline points={toPts("out_bps")} maxV={maxIntra} color="#0284c7" />
                  {intra.map((r, i) =>
                    i % Math.max(1, Math.ceil(intra.length / 8)) === 0 ? (
                      <text key={r.t} x={PAD_L + ((r.t - t0) / span) * plotW} y={H - 8} textAnchor="middle" fontSize="9" fill="#8aa096">{hhmm(r.t)}</text>
                    ) : null
                  )}
                </>
              ) : null}
              {showDaily ? (
                <>
                  <Sparkline points={toDaily("in_bps_peak")} maxV={maxDaily} color="#059669" />
                  <Sparkline points={toDaily("out_bps_peak")} maxV={maxDaily} color="#0284c7" />
                  {rows.map((r, i) =>
                    i % Math.max(1, Math.ceil(rows.length / 8)) === 0 ? (
                      <text key={r.day} x={PAD_L + i * step} y={H - 8} textAnchor="middle" fontSize="9" fill="#8aa096">{r.day.slice(5)}</text>
                    ) : null
                  )}
                </>
              ) : null}
            </svg>
          )}

          <table className="mt-4 w-full text-xs">
            <thead>
              <tr className="border-b border-[#e5eeea] text-left text-[#8aa096]">
                <th className="py-1.5 font-medium">Tanggal</th>
                <th className="py-1.5 text-right font-medium">Unduh peak</th>
                <th className="py-1.5 text-right font-medium">Unggah peak</th>
                <th className="py-1.5 text-right font-medium">Sampel</th>
              </tr>
            </thead>
            <tbody>
              {rows.slice().reverse().map((r) => (
                <tr key={r.day} className="border-b border-[#f1f7f4]">
                  <td className="py-1.5 font-mono">{r.day}</td>
                  <td className="py-1.5 text-right font-mono">{fmt(r.in_bps_peak)}</td>
                  <td className="py-1.5 text-right font-mono">{fmt(r.out_bps_peak)}</td>
                  <td className="py-1.5 text-right font-mono">{r.samples}</td>
                </tr>
              ))}
              {!rows.length && (
                <tr><td colSpan={4} className="py-3 text-center text-[#8aa096]">Belum ada data harian.</td></tr>
              )}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}
