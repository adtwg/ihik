"use client";

// Skema fisik chassis OLT: card per slot + status port. Data-driven dari
// /api/v1/olts/{id}/chassis (SNMP health + fallback CLI "show card").

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Cpu, MemoryStick, RefreshCw, Server, Thermometer } from "lucide-react";
import { clientAPI } from "@/lib/api/client";
import { type ChassisCard, type ChassisPort, type ChassisView as Chassis, type OLT } from "@/lib/olts/types";
import { ChassisFrontPanel } from "./chassis-front-panel";

// Jumlah slot minimal per keluarga chassis untuk memberi kesan rak penuh.
// Perkiraan; slot kosong hanya pengisi visual, card nyata tetap dari data.
const FAMILY_BASE_SLOTS: Record<string, number> = {
  C320: 4,
  C300: 20,
  C220: 10,
  C600: 16,
};

function cardStatusStyle(status?: string): { dot: string; text: string; label: string } {
  switch ((status || "").toLowerCase()) {
    case "inservice":
      return { dot: "bg-emerald-500", text: "text-emerald-700", label: "Running" };
    case "standby":
      return { dot: "bg-amber-500", text: "text-amber-700", label: "Standby" };
    case "offline":
      return { dot: "bg-red-500", text: "text-red-700", label: "Offline" };
    default:
      return { dot: "bg-slate-300", text: "text-slate-500", label: status || "—" };
  }
}

function portStyle(status: string): { cls: string; title: string } {
  switch (status) {
    case "online":
      return { cls: "bg-emerald-500 border-emerald-600 text-white", title: "Online" };
    case "los":
      return { cls: "bg-red-500 border-red-600 text-white", title: "LOS" };
    case "offline":
      return { cls: "bg-red-500 border-red-600 text-white", title: "Card / semua ONU offline" };
    case "idle":
      return { cls: "bg-sky-400 border-sky-500 text-white", title: "SFP terpasang, tanpa ONU" };
    default:
      return { cls: "bg-slate-100 border-slate-200 text-slate-400", title: "Belum terbaca" };
  }
}

function MiniBar({ label, value, icon: Icon }: { label: string; value: number; icon: React.ComponentType<{ size?: number }> }) {
  const color = value > 85 ? "bg-red-500" : value > 70 ? "bg-amber-500" : "bg-emerald-500";
  return (
    <div>
      <div className="mb-0.5 flex items-center justify-between text-[10px] text-[#607067]">
        <span className="flex items-center gap-1"><Icon size={10} /> {label}</span>
        <span className="font-mono font-semibold">{value > 0 ? `${value.toFixed(0)}%` : "—"}</span>
      </div>
      <div className="h-1 w-full overflow-hidden rounded-full bg-[#eef4f1]">
        <div className={`h-full rounded-full ${color}`} style={{ width: `${Math.min(100, value)}%` }} />
      </div>
    </div>
  );
}

function PortCell({ port }: { port: ChassisPort }) {
  const style = portStyle(port.status);
  const title = `Port ${port.port} · ${style.title}${port.onu_total ? ` · ONU ${port.onu_online}/${port.onu_total}` : ""}${port.rx_dbm ? ` · Rx ${port.rx_dbm.toFixed(1)} dBm` : ""}`;
  return (
    <div
      title={title}
      className={`flex h-9 flex-col items-center justify-center rounded border text-[9px] font-bold leading-none ${style.cls}`}
    >
      <span>{port.port}</span>
      {port.onu_total > 0 && <span className="mt-0.5 text-[8px] font-semibold opacity-90">{port.onu_online}/{port.onu_total}</span>}
    </div>
  );
}

function CardBlade({ card }: { card: ChassisCard }) {
  const st = cardStatusStyle(card.status);
  return (
    <div className={`flex w-full flex-col rounded-lg border bg-white p-2.5 shadow-sm ${card.is_control ? "border-indigo-200 bg-indigo-50/40" : "border-[#e5eeea]"}`}>
      <div className="mb-2 flex items-center justify-between">
        <div className="flex items-center gap-1.5">
          <span className="rounded bg-[#10251d] px-1.5 py-0.5 font-mono text-[10px] font-bold text-white">S{card.slot}</span>
          <span className="text-xs font-bold text-[#10251d]">{card.type || "—"}</span>
          {card.is_control && <span className="rounded bg-indigo-100 px-1.5 py-0.5 text-[9px] font-bold text-indigo-700">CTRL</span>}
        </div>
        <span className="flex items-center gap-1">
          <span className={`h-2 w-2 rounded-full ${st.dot} ${st.label === "Running" ? "animate-pulse" : ""}`} />
          <span className={`text-[10px] font-bold ${st.text}`}>{st.label}</span>
        </span>
      </div>

      {card.is_control ? (
        <div className="space-y-1.5">
          <MiniBar label="CPU" value={card.cpu_percent} icon={Cpu} />
          <MiniBar label="MEM" value={card.mem_percent} icon={MemoryStick} />
          <div className="flex items-center justify-between text-[10px] text-[#607067]">
            <span className="flex items-center gap-1"><Thermometer size={10} /> Suhu</span>
            <span className="font-mono font-semibold">{card.temp_c > 0 ? `${card.temp_c.toFixed(0)}°C` : "—"}</span>
          </div>
          {card.role && <div className="text-[10px] text-[#607067]">Peran: <span className="font-semibold">{card.role}</span></div>}
        </div>
      ) : card.ports && card.ports.length > 0 ? (
        <div className="grid grid-cols-4 gap-1">
          {card.ports.map((p) => <PortCell key={p.port} port={p} />)}
        </div>
      ) : (
        <div className="rounded border border-dashed border-[#e5eeea] py-3 text-center text-[10px] text-[#a8bcb2]">
          {card.temp_c > 0 && <span className="mr-2">{card.temp_c.toFixed(0)}°C</span>}
          Tidak ada port terdeteksi
        </div>
      )}
    </div>
  );
}

function EmptyBay({ slot }: { slot: number }) {
  return (
    <div className="flex min-h-[96px] w-full flex-col items-center justify-center rounded-lg border border-dashed border-[#e5eeea] bg-[#f7faf9] p-2.5">
      <span className="rounded bg-slate-200 px-1.5 py-0.5 font-mono text-[10px] font-bold text-slate-500">S{slot}</span>
      <span className="mt-1 text-[10px] text-[#a8bcb2]">Belum terdeteksi</span>
    </div>
  );
}

function Legend() {
  const items = [
    { c: "bg-emerald-500", l: "Online / Running" },
    { c: "bg-amber-500", l: "Standby" },
    { c: "bg-red-500", l: "Offline / LOS terkonfirmasi" },
    { c: "bg-sky-400", l: "SFP tanpa ONU" },
    { c: "bg-slate-200", l: "Belum terbaca" },
  ];
  return (
    <div className="flex flex-wrap items-center gap-3 text-[11px] text-[#607067]">
      {items.map((i) => (
        <span key={i.l} className="flex items-center gap-1.5">
          <span className={`h-2.5 w-2.5 rounded-full ${i.c}`} /> {i.l}
        </span>
      ))}
    </div>
  );
}

function ChassisSkeleton() {
  return (
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">
      {[0, 1, 2, 3].map((i) => (
        <div key={i} className="h-28 animate-pulse rounded-lg border border-[#e5eeea] bg-white" />
      ))}
    </div>
  );
}

export function ChassisView({ olts, loading, error }: { olts: OLT[]; loading?: boolean; error?: string | null }) {
  const [selectedID, setSelectedID] = useState<string | null>(olts[0]?.id ?? null);
  const [data, setData] = useState<Chassis | null>(null);
  const [fetching, setFetching] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const requestRef = useRef<AbortController | null>(null);

  useEffect(() => {
    if (!selectedID && olts.length > 0) setSelectedID(olts[0].id);
  }, [olts, selectedID]);

  const load = useCallback(async () => {
    if (!selectedID) return;
    requestRef.current?.abort();
    const controller = new AbortController();
    requestRef.current = controller;
    const timer = setTimeout(() => controller.abort(), 28000);
    setFetching(true);
    setLoadError(null);
    try {
      const res = await clientAPI<Chassis>(`/api/v1/olts/${selectedID}/chassis`, { signal: controller.signal, timeoutMs: 28000 });
      if (requestRef.current !== controller || controller.signal.aborted) return;
      setData(res);
    } catch (e) {
      if (requestRef.current !== controller) return;
      setLoadError(controller.signal.aborted ? "Timeout membaca chassis. Snapshot terakhir belum diperbarui." : e instanceof Error ? e.message : "Gagal memuat perangkat OLT.");
    } finally {
      clearTimeout(timer);
      if (requestRef.current === controller) {
        requestRef.current = null;
        setFetching(false);
      }
    }
  }, [selectedID]);

  useEffect(() => {
    setData(null);
    void load();
    return () => {
      requestRef.current?.abort();
      requestRef.current = null;
    };
  }, [load]);

  const bays = useMemo(() => {
    if (!data) return [];
    const bySlot = new Map<number, ChassisCard>();
    let maxSlot = 0;
    for (const c of data.cards) {
      bySlot.set(c.slot, c);
      if (c.slot > maxSlot) maxSlot = c.slot;
    }
    const base = FAMILY_BASE_SLOTS[data.family] ?? 0;
    const total = Math.max(maxSlot, base);
    const list: Array<{ slot: number; card: ChassisCard | null }> = [];
    for (let s = 1; s <= total; s++) list.push({ slot: s, card: bySlot.get(s) ?? null });
    return list;
  }, [data]);

  if (loading) return <ChassisSkeleton />;
  if (error) return <div className="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-800">{error}</div>;
  if (olts.length === 0) return <div className="rounded-lg border border-[#e5eeea] bg-white px-3 py-6 text-center text-sm text-[#607067]">Belum ada OLT terdaftar.</div>;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <label className="text-sm font-semibold text-[#10251d]">OLT:</label>
        <select
          aria-label="Pilih OLT"
          className="max-w-full rounded-md border border-[#d7dfda] bg-white px-3 py-1.5 text-sm"
          value={selectedID ?? ""}
          onChange={(e) => setSelectedID(e.target.value)}
        >
          {olts.map((o) => (
            <option key={o.id} value={o.id}>{o.name} · {o.model}</option>
          ))}
        </select>
        <button
          className="secondary-button"
          onClick={() => void load()}
          disabled={fetching}
        >
          <RefreshCw size={15} className={fetching ? "animate-spin" : ""} /> Muat ulang
        </button>
      </div>

      {loadError && <div className="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-800">{loadError}</div>}

      {data && <ChassisFrontPanel key={selectedID} data={data} />}

      {fetching && !data ? (
        <ChassisSkeleton />
      ) : data ? (
        <div className="min-w-0 py-4">
          <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              <Server size={18} className="text-[#096b4c]" />
              <span className="text-base font-bold text-[#10251d]">ZTE {data.model}</span>
              {data.family !== "unknown" && <span className="rounded bg-[#eef4f1] px-2 py-0.5 text-xs font-semibold text-[#096b4c]">{data.family}</span>}
            </div>
            <span className="rounded bg-slate-100 px-2 py-0.5 text-[11px] font-semibold text-slate-600">sumber: {data.source}</span>
          </div>

          {bays.length === 0 ? (
            <div className="rounded-lg border border-dashed border-[#e5eeea] py-8 text-center text-sm text-[#607067]">
              Data card tidak tersedia. OLT mungkin tidak mengekspos daftar card via SNMP maupun CLI.
            </div>
          ) : (
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5">
              {bays.map((b) => (
                <div key={b.slot}>{b.card ? <CardBlade card={b.card} /> : <EmptyBay slot={b.slot} />}</div>
              ))}
            </div>
          )}

          <div className="mt-4 border-t border-[#eef4f1] pt-3">
            <Legend />
          </div>
        </div>
      ) : null}
    </div>
  );
}
