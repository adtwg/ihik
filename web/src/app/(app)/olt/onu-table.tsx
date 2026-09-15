"use client";

// Tabel ONU — expandable row: tiap baris punya panah, klik untuk detail
// trafik realtime + chart intraday. Trafik tidak ditampilkan semua ONU.

import { Fragment, useCallback, useEffect, useMemo, useState } from "react";
import { AlertTriangle, ChevronDown, ChevronLeft, ChevronRight, ChevronRightIcon, Power, RefreshCw, RotateCcw, Search, Trash2 } from "lucide-react";
import { clientAPI } from "@/lib/api/client";
import type {
  OLT,
  ONUPaged,
  ONU,
  ONUActionResponse,
  ONUOneSyncResponse,
} from "@/lib/olts/types";
import { normalizeStatusKey } from "@/lib/olts/status";
import { OnuStatsBar, type OnuStats } from "./onu-stats";
import { OnuTrafficDetail } from "./onu-traffic-detail";
import { ProvisionPanel } from "./provision-panel";

function fmtDbm(v: number): string {
  if (!v) return "—";
  return v.toFixed(2);
}

function sleep(ms: number) {
  return new Promise<void>((resolve) => setTimeout(resolve, ms));
}

const statusPill: Record<string, { label: string; cls: string; dot: string }> = {
  working: { label: "Online", cls: "bg-emerald-50 text-emerald-700 border-emerald-200", dot: "bg-emerald-500" },
  los: { label: "LOS", cls: "bg-red-50 text-red-700 border-red-200", dot: "bg-red-500" },
  logging: { label: "Logging", cls: "bg-amber-50 text-amber-700 border-amber-200", dot: "bg-amber-500" },
  sync_mib: { label: "Sync MIB", cls: "bg-amber-50 text-amber-700 border-amber-200", dot: "bg-amber-500" },
  dying_gasp: { label: "Dying Gasp", cls: "bg-amber-50 text-amber-700 border-amber-200", dot: "bg-amber-500" },
  auth_failed: { label: "Auth Gagal", cls: "bg-red-50 text-red-700 border-red-200", dot: "bg-red-500" },
  offlined: { label: "Offline", cls: "bg-slate-50 text-slate-600 border-slate-200", dot: "bg-slate-400" },
  unknown: { label: "Unknown", cls: "bg-slate-50 text-slate-600 border-slate-200", dot: "bg-slate-300" },
};

function rxCell(v: number) {
  if (!v) return <span className="text-[#a8bcb2]">—</span>;
  const color = v > -25 ? "text-emerald-700" : v > -28 ? "text-amber-600" : "text-red-600";
  const bar = v > -25 ? "bg-emerald-500" : v > -28 ? "bg-amber-500" : "bg-red-500";
  return (
    <span className="inline-flex items-center gap-1.5">
      <span className={`inline-block h-1.5 w-1.5 rounded-full ${bar}`} />
      <span className={`font-mono text-xs font-medium ${color}`}>{v.toFixed(2)}</span>
    </span>
  );
}

function resolveOnuRef(onu: ONU): { pon: string; onuID: number } | null {
  const label = (onu.onu_number || "").trim();
  const m = label.match(/^(\d+\/\d+\/\d+):(\d+)$/);
  if (m) return { pon: m[1], onuID: Number(m[2]) };

  const idx = (onu.index || "").trim();
  const parts = idx.split(".");
  if (parts.length >= 2) {
    const base = Number(parts[0]);
    const onuID = Number(parts[parts.length - 1]);
    if (Number.isFinite(base) && Number.isFinite(onuID) && base > 0xffff && onuID > 0) {
      // Encoding ZTE .1082: 0x11 | shelf | slot | pon (slot di bits 8-15).
      const shelf = Math.floor(base / 65536) % 256;
      let slot = Math.floor(base / 256) % 256;
      let port = base % 256;
      if (port === 0) {
        slot = Math.floor(base / 65536) % 256;
        port = Math.floor(base / 256) % 256;
      }
      if (shelf > 0 && slot > 0 && port > 0) {
        return { pon: `${shelf}/${slot}/${port}`, onuID };
      }
    }
  }
  // Fallback: jika index langsung berbentuk gpon-onu_1/1/1:7
  const raw = idx.toLowerCase();
  const mm = raw.match(/gpon-onu_(\d+\/\d+\/\d+):(\d+)/);
  if (mm) return { pon: mm[1], onuID: Number(mm[2]) };
  return null;
}

const ONLINE_STATUS = new Set(["working", "logging", "sync_mib", "online", "ready"]);

function statusAllowsOpticalUI(raw?: string): boolean {
  return ONLINE_STATUS.has(normalizeStatusKey(raw));
}

function needsOpticalBackfill(onu: ONU): boolean {
  if (!statusAllowsOpticalUI(onu.status)) return false;
  return !onu.rx_power_dbm || !onu.tx_power_dbm;
}

function applyOnuPatch(prev: ONU, updated: Partial<ONU>): ONU {
  const status = updated.status || prev.status;
  const showOptical = statusAllowsOpticalUI(status);
  return {
    ...prev,
    status,
    name: updated.name ?? prev.name,
    serial_number: updated.serial_number ?? prev.serial_number,
    rx_power_dbm: showOptical ? (updated.rx_power_dbm ?? prev.rx_power_dbm ?? 0) : 0,
    tx_power_dbm: showOptical ? (updated.tx_power_dbm ?? prev.tx_power_dbm ?? 0) : 0,
    distance_m: updated.distance_m ?? prev.distance_m,
  };
}

const pageSizeOptions = [25, 50, 100, 200];

export function OnuTable({ olt }: { olt: OLT }) {
  const [data, setData] = useState<ONUPaged>({ items: [], total: 0, page: 1 });
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(25);
  const [searchInput, setSearchInput] = useState("");
  const [search, setSearch] = useState("");
  const [sort, setSort] = useState("index");
  const [order, setOrder] = useState<"asc" | "desc">("asc");
  const [ponFilter, setPonFilter] = useState("");
  const [ponFilterInput, setPonFilterInput] = useState("Semua PON");
  const [ponDropdownOpen, setPonDropdownOpen] = useState(false);
  const [ponDropdownSearch, setPonDropdownSearch] = useState("");
  const [loading, setLoading] = useState(false);
  const [syncAllBusy, setSyncAllBusy] = useState(false);
  const [tableError, setTableError] = useState<string | null>(null);
  const [liveTraffic, setLiveTraffic] = useState(false);
  const [stats, setStats] = useState<OnuStats | null>(null);
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const [actionBusy, setActionBusy] = useState<string | null>(null);
  const [actionMessage, setActionMessage] = useState<{ text: string; error?: boolean } | null>(null);
  const [autoBackfillBusy, setAutoBackfillBusy] = useState(false);

  const ponOptions = useMemo(() => {
    const rows = stats?.pon_ports ?? [];
    return [...rows].sort((a, b) => a.port.localeCompare(b.port, "en"));
  }, [stats]);
  const filteredPonOptions = useMemo(() => {
    const q = ponDropdownSearch.trim().toLowerCase();
    if (!q) return ponOptions;
    return ponOptions.filter((row) => row.port.toLowerCase().includes(q));
  }, [ponDropdownSearch, ponOptions]);

  const load = useCallback(async (silent = false, signal?: AbortSignal): Promise<ONUPaged | null> => {
    if (!silent) setLoading(true);
    if (!silent) setTableError(null);
    try {
      const query = new URLSearchParams({
        page: String(page), page_size: String(pageSize),
        search, sort, order,
        _ts: String(Date.now()),
      });
      if (ponFilter) query.set("pon", ponFilter);
      const result = await clientAPI<ONUPaged>(`/api/v1/olts/${olt.id}/onus-paged?${query.toString()}`, { signal });
      const normalized = {
        items: Array.isArray(result?.items) ? result.items : [],
        total: Number(result?.total ?? 0) || 0,
        page: Number(result?.page ?? page) || page,
      };
      setData(normalized);
      return normalized;
    } catch (error) {
      const isAbort = error instanceof Error && (error.name === "AbortError" || /aborted/i.test(error.message));
      if (silent && isAbort) {
        return null;
      }
      if (signal?.aborted) {
        return null;
      }
      const msg = isAbort
        ? "Request timeout. Data sedang sinkron, coba refresh lagi."
        : (error instanceof Error ? error.message : "Gagal memuat data ONU.");
      if (!silent) setTableError(msg);
      return null;
    } finally {
      if (!silent) setLoading(false);
    }
  }, [olt.id, page, pageSize, search, sort, order, ponFilter]);

  useEffect(() => {
    const abortController = new AbortController();
    void load(false, abortController.signal);
    return () => abortController.abort();
  }, [load]);

  useEffect(() => {
    const abortController = new AbortController();
    const timer = setTimeout(() => {
      const next = searchInput.trim();
      if (search !== next) {
        setSearch(next);
        setPage(1);
      }
    }, 220);
    return () => {
      clearTimeout(timer);
      abortController.abort();
    };
  }, [searchInput, search]);

  useEffect(() => {
    setPonFilterInput(ponFilter || "Semua PON");
  }, [ponFilter]);

  useEffect(() => {
    if (!liveTraffic) return;
    const abortController = new AbortController();
    const tick = async () => {
      try {
        await clientAPI(`/api/v1/olts/${olt.id}/refresh-traffic`, { method: "POST" });
        await load(true, abortController.signal);
      } catch { /* ignore */ }
    };
    void tick();
    const timer = setInterval(tick, 5000);
    return () => {
      clearInterval(timer);
      abortController.abort();
    };
  }, [liveTraffic, olt.id, load]);

  useEffect(() => {
    if (loading || syncAllBusy || autoBackfillBusy || actionBusy !== null || tableError) return;
    if (!data.items.length) return;
    const candidates = data.items.filter(needsOpticalBackfill);
    if (candidates.length === 0) return;
    const timer = setTimeout(() => {
      void backfillVisibleOptical(data.items, 4, true);
    }, 600);
    return () => clearTimeout(timer);
  }, [data.items, loading, syncAllBusy, autoBackfillBusy, actionBusy, tableError]);

  function applyPonFilter(value: string) {
    setPonFilter(value);
    setPage(1);
    setPonDropdownOpen(false);
    setPonDropdownSearch("");
  }

  function toggleSort(column: string) {
    if (sort === column) setOrder(order === "asc" ? "desc" : "asc");
    else { setSort(column); setOrder("asc"); }
    setPage(1);
  }

  async function backfillVisibleOptical(items: ONU[], maxItems = 8, quiet = false) {
    if (autoBackfillBusy) return;
    const candidates = items.filter(needsOpticalBackfill).slice(0, maxItems);
    if (candidates.length === 0) return;

    setAutoBackfillBusy(true);
    try {
      if (!quiet) {
        setActionMessage({ text: `Melengkapi redaman ${candidates.length} ONU online di halaman ini...` });
      }
      for (const row of candidates) {
        const ref = resolveOnuRef(row);
        if (!ref) continue;
        try {
          const r = await clientAPI<ONUOneSyncResponse>(`/api/v1/olts/${olt.id}/onu-sync`, {
            method: "POST",
            body: JSON.stringify({ index: row.index, pon: ref.pon, onu_id: ref.onuID }),
          });
          const updated = r.result;
          if (updated) {
            setData((prev) => ({
              ...prev,
              items: prev.items.map((x) => x.index === row.index ? applyOnuPatch(x, {
                status: updated.status,
                rx_power_dbm: updated.rx_power_dbm,
                tx_power_dbm: updated.tx_power_dbm,
                distance_m: updated.distance_m,
                name: updated.name,
              }) : x),
            }));
          }
        } catch {
          // lanjut ke ONU berikutnya; jangan hentikan batch
        }
        await sleep(450);
      }
      await load(true);
    } finally {
      setAutoBackfillBusy(false);
    }
  }

  async function syncAllONUs() {
    if (!window.confirm("Jalankan sync redaman semua ONU di background?")) return;
    setSyncAllBusy(true);
    setActionMessage(null);
    try {
      const r = await clientAPI<{ ok: boolean; message: string }>(`/api/v1/olts/${olt.id}/sync-optical`, { method: "POST" });
      setActionMessage({ text: r.message || "Sync redaman semua ONU berjalan di background." });

      // Poll cepat 3x setelah trigger supaya nilai yang sudah masuk dari
      // background langsung tampil tanpa refresh manual.
      const abortController = new AbortController();
      for (const waitMs of [2000, 4500, 9000]) {
        await sleep(waitMs);
        await load(true, abortController.signal);
      }
      abortController.abort();
    } catch (error) {
      setActionMessage({ text: error instanceof Error ? error.message : "Sync redaman semua ONU gagal.", error: true });
    } finally {
      setSyncAllBusy(false);
      const abortController = new AbortController();
      void load(true, abortController.signal);
    }
  }

  async function syncOneONU(onu: ONU) {
    const ref = resolveOnuRef(onu);
    if (!ref) {
      setActionMessage({ text: `Format ONU ${onu.index} tidak dikenali untuk sync per-ONU.`, error: true });
      return;
    }
    const busyKey = `${onu.index}:sync`;
    setActionBusy(busyKey);
    setActionMessage(null);
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 22000);
    try {
      const payload: Record<string, string | number> = { index: onu.index, pon: ref.pon, onu_id: ref.onuID };
      const r = await clientAPI<ONUOneSyncResponse>(`/api/v1/olts/${olt.id}/onu-sync`, {
        method: "POST",
        body: JSON.stringify(payload),
        signal: controller.signal,
      });
      const updated = r.result;
      if (updated) {
        setData((prev) => ({
          ...prev,
          items: prev.items.map((row) => row.index === onu.index ? applyOnuPatch(row, {
            status: updated.status,
            rx_power_dbm: updated.rx_power_dbm,
            tx_power_dbm: updated.tx_power_dbm,
            distance_m: updated.distance_m,
            name: updated.name,
          }) : row),
        }));
      }
      const method = updated?.method ? ` via ${String(updated.method).toUpperCase()}` : "";
      const label = updated ? `${updated.pon}:${updated.onu_id}` : (onu.onu_number || onu.index);
      setActionMessage({ text: r.message || `Sync ONU ${label} berhasil${method}.` });
    } catch (error) {
      setActionMessage({ text: error instanceof Error ? error.message : "Sync ONU gagal.", error: true });
    } finally {
      clearTimeout(timer);
      setActionBusy(null);
    }
  }

  async function onuAction(
    onu: ONU,
    action: "reboot" | "reset" | "delete" | "disable" | "enable",
    confirmText: string,
  ) {
    if (!window.confirm(confirmText)) return;
    const ref = resolveOnuRef(onu);
    const busyKey = `${onu.index}:${action}`;
    setActionBusy(busyKey);
    setActionMessage(null);
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 20000);
    try {
      const payload: Record<string, string | number> = { index: onu.index };
      if (ref) {
        payload.pon = ref.pon;
        payload.onu_id = ref.onuID;
      }
      const r = await clientAPI<ONUActionResponse>(`/api/v1/olts/${olt.id}/onus/${action}`, {
        method: "POST",
        body: JSON.stringify(payload),
        signal: controller.signal,
      });
      const via = r.method ? ` via ${String(r.method).toUpperCase()}` : "";
      const fb = r.fallback ? " (fallback)" : "";
      setActionMessage({ text: r.message || `Aksi ${action} berhasil${via}${fb}.` });
      const abortController = new AbortController();
      await sleep(800);
      await load(true, abortController.signal);
      abortController.abort();
    } catch (error) {
      setActionMessage({ text: error instanceof Error ? error.message : `Aksi ${action} gagal.`, error: true });
    } finally {
      clearTimeout(timer);
      setActionBusy(null);
    }
  }

  function toggleExpand(index: string) {
    setExpanded((prev) => (prev[index] ? {} : { [index]: true }));
  }

  const pageCount = Math.max(1, Math.ceil(data.total / pageSize));
  const th = "px-3 py-2 text-left text-[11px] font-bold uppercase tracking-wider text-[#8aa096]";
  const td = "px-3 py-2.5";

  return (
    <div>
      <OnuStatsBar olt={olt} stats={stats} setStats={setStats} live={liveTraffic} />
      <ProvisionPanel olt={olt} />

      {/* Toolbar */}
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <div className="flex flex-wrap items-center gap-2">
          <div className="relative">
            <Search className="pointer-events-none absolute left-3 top-2.5 text-[#8aa096]" size={16} />
            <input
              className="w-64 rounded-lg border border-[#d5e2dc] bg-white py-2 pl-9 pr-3 text-sm shadow-sm placeholder:text-[#a8bcb2] focus:border-[#2c7a5b] focus:outline-none"
              value={searchInput}
              onChange={(event) => setSearchInput(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") {
                  const next = searchInput.trim();
                  setSearch(next);
                  setPage(1);
                }
              }}
              placeholder="Cari nama / serial…"
            />
          </div>

          <div className="relative">
            <button
              type="button"
              className="inline-flex h-[42px] w-52 items-center justify-between rounded-lg border border-[#d5e2dc] bg-white px-3 text-sm shadow-sm hover:border-[#2c7a5b] focus:border-[#2c7a5b] focus:outline-none"
              onClick={() => setPonDropdownOpen((v) => !v)}
              aria-label="Filter Port PON"
              aria-expanded={ponDropdownOpen}
            >
              <span className="truncate text-left">{ponFilterInput}</span>
              <ChevronDown size={16} className={`transition-transform ${ponDropdownOpen ? "rotate-180" : ""}`} />
            </button>

            {ponDropdownOpen ? (
              <>
                <button
                  type="button"
                  className="fixed inset-0 z-20 cursor-default"
                  onClick={() => setPonDropdownOpen(false)}
                  aria-label="Tutup daftar filter PON"
                />
                <div className="absolute left-0 top-[calc(100%+6px)] z-30 w-72 rounded-xl border border-[#d5e2dc] bg-white p-2 shadow-xl">
                  <input
                    className="w-full rounded-lg border border-[#d5e2dc] bg-white px-3 py-2 text-sm shadow-sm placeholder:text-[#a8bcb2] focus:border-[#2c7a5b] focus:outline-none"
                    value={ponDropdownSearch}
                    onChange={(event) => setPonDropdownSearch(event.target.value)}
                    placeholder="Cari port PON..."
                    autoFocus
                  />
                  <div className="mt-2 max-h-64 overflow-auto rounded-lg border border-[#eef3f0]">
                    <button
                      type="button"
                      className={`flex w-full items-center justify-between px-3 py-2 text-left text-sm ${ponFilter === "" ? "bg-[#2c7a5b] text-white" : "hover:bg-[#f6faf8]"}`}
                      onClick={() => {
                        setPonFilterInput("Semua PON");
                        applyPonFilter("");
                      }}
                    >
                      <span>Semua PON</span>
                      <span className={`font-mono text-xs ${ponFilter === "" ? "text-white/90" : "text-[#8aa096]"}`}>{stats?.total ?? data.total}</span>
                    </button>
                    {filteredPonOptions.length === 0 ? (
                      <div className="px-3 py-2 text-xs text-[#8aa096]">Port tidak ditemukan.</div>
                    ) : (
                      filteredPonOptions.map((port) => (
                        <button
                          key={port.port}
                          type="button"
                          className={`flex w-full items-center justify-between px-3 py-2 text-left text-sm ${ponFilter === port.port ? "bg-[#2c7a5b] text-white" : "hover:bg-[#f6faf8]"}`}
                          onClick={() => {
                            setPonFilterInput(port.port);
                            applyPonFilter(port.port);
                          }}
                        >
                          <span className="font-mono">{port.port}</span>
                          <span className={`font-mono text-xs ${ponFilter === port.port ? "text-white/90" : "text-[#8aa096]"}`}>{port.count}</span>
                        </button>
                      ))
                    )}
                  </div>
                </div>
              </>
            ) : null}
          </div>
        </div>
        <select
          className="rounded-lg border border-[#d5e2dc] bg-white px-2.5 py-2 text-sm shadow-sm focus:border-[#2c7a5b] focus:outline-none"
          value={pageSize}
          onChange={(event) => { setPageSize(Number(event.target.value)); setPage(1); }}
          aria-label="Baris per halaman"
        >
          {pageSizeOptions.map((size) => <option key={size} value={size}>{size} baris</option>)}
        </select>
        <button
          className="inline-flex items-center gap-1.5 rounded-lg border border-[#d5e2dc] bg-white px-3 py-2 text-sm font-medium text-[#44554d] shadow-sm hover:border-[#2c7a5b] disabled:opacity-40"
          onClick={() => {
            const abortController = new AbortController();
            void load(false, abortController.signal);
            return () => abortController.abort();
          }} disabled={loading}
        >
          <RefreshCw size={15} className={loading ? "animate-spin" : ""} /> Refresh
        </button>
        <button
          className="inline-flex items-center gap-2 rounded-lg border border-[#d7e5dd] bg-white px-3 py-2 text-xs font-semibold text-[#2f4f42] shadow-sm hover:bg-[#f7fbf9] disabled:opacity-60"
          onClick={() => void syncAllONUs()} disabled={syncAllBusy || loading}
          title="Sync redaman semua ONU"
        >
          <RefreshCw size={15} className={syncAllBusy ? "animate-spin" : ""} /> Sync Redaman
        </button>
        <button
          aria-pressed={liveTraffic}
          className={
            "inline-flex items-center gap-1.5 rounded-lg border px-3 py-2 text-sm font-medium shadow-sm " +
            (liveTraffic
              ? "border-[#2c7a5b] bg-[#eaf5ef] text-[#1d5c43]"
              : "border-[#d5e2dc] bg-white text-[#44554d] hover:border-[#2c7a5b]")
          }
          onClick={() => setLiveTraffic((v) => !v)}
          title="Polling trafik tiap 5 detik pada panel detail ONU"
        >
          <span className={liveTraffic ? "relative flex h-2 w-2" : "h-2 w-2 rounded-full bg-[#b9c7c0]"}>
            {liveTraffic && <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-[#2c7a5b] opacity-60" />}
            {liveTraffic && <span className="relative inline-flex h-2 w-2 rounded-full bg-[#2c7a5b]" />}
          </span>
          Trafik Live
        </button>
      </div>

      {tableError ? (
        <div className="mb-2 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-xs font-semibold text-red-700">
          {tableError}
        </div>
      ) : null}

      {actionMessage ? (
        <div className={`mb-3 inline-flex items-center gap-2 rounded-lg border px-3 py-2 text-xs font-semibold ${actionMessage.error ? "border-red-200 bg-red-50 text-red-700" : "border-emerald-200 bg-emerald-50 text-emerald-700"}`}>
          <AlertTriangle size={14} />
          {actionMessage.text}
        </div>
      ) : null}

      {/* Tabel */}
      <div className="overflow-x-auto rounded-xl border border-[#e5eeea] shadow-sm">
        <table className="w-full min-w-[720px] text-sm">
          <thead className="bg-[#f6faf8]">
            <tr>
              {([
                ["index", "ONU"], ["name", "Nama"], [null, "Serial"],
                ["status", "Status"], ["rx", "Rx (dBm)"], [null, "Tx (dBm)"],
                [null, "Jarak"], [null, ""],
              ] as const).map(([key, label]) => (
                <th key={label} className={th}>
                  {key ? (
                    <button className="inline-flex items-center gap-0.5 hover:text-[#2c7a5b]" onClick={() => toggleSort(key)}>
                      {label}{sort === key ? <span className="text-[#2c7a5b]">{order === "asc" ? "↑" : "↓"}</span> : null}
                    </button>
                  ) : label}
                </th>
              ))}
            </tr>
          </thead>
          <tbody className="divide-y divide-[#f0f5f2] bg-white">
            {data.items.map((onu: ONU) => {
              const statusKey = normalizeStatusKey(onu.status);
              const st = statusPill[statusKey] ?? statusPill.unknown;
              const showOptical = statusAllowsOpticalUI(onu.status);
              const isOpen = !!expanded[onu.index];
              return (
                <Fragment key={onu.index}>
                  <tr className="transition-colors hover:bg-[#f8fbfa]">
                    <td className={`${td} font-mono text-xs font-semibold text-[#10251d]`}>
                      <button
                        className="mr-2 inline-flex items-center rounded p-0.5 text-[#8aa096] hover:bg-[#eef7f3] hover:text-[#2c7a5b]"
                        onClick={() => {
                          toggleExpand(onu.index);
                        }}
                        aria-label={isOpen ? "Tutup detail" : "Buka detail"}
                      >
                        {isOpen ? <ChevronDown size={14} /> : <ChevronRightIcon size={14} />}
                      </button>
                      {onu.onu_number || onu.index}
                    </td>
                    <td className={`${td} max-w-[160px] truncate font-medium`} title={onu.name}>{onu.name || <span className="text-[#a8bcb2]">—</span>}</td>
                    <td className={`${td} font-mono text-xs text-[#607067]`}>{onu.serial_number || "—"}</td>
                    <td className={td}>
                      <span className={`inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-xs font-semibold ${st.cls}`}>
                        <span className={`h-1.5 w-1.5 rounded-full ${st.dot}`} />
                        {st.label}
                      </span>
                    </td>
                    <td className={td}>{showOptical ? rxCell(onu.rx_power_dbm) : <span className="text-[#a8bcb2]">—</span>}</td>
                    <td className={`${td} font-mono text-xs text-[#44554d]`}>{showOptical ? fmtDbm(onu.tx_power_dbm) : "—"}</td>
                    <td className={`${td} font-mono text-xs text-[#607067]`}>{onu.distance_m ? `${onu.distance_m.toFixed(0)} m` : "—"}</td>
                    <td className={td}>
                      <div className="flex items-center gap-0.5">
                        <button
                          className="inline-flex items-center gap-1 rounded-md border border-[#d5e2dc] bg-white px-2 py-1 text-[11px] font-semibold text-[#1d5c43] transition-colors hover:border-[#2c7a5b] hover:bg-[#eef7f3] disabled:opacity-40"
                          title="Sync ONU ini (status + redaman + jarak)"
                          disabled={actionBusy === `${onu.index}:sync`}
                          onClick={() => { void syncOneONU(onu); }}
                        ><RefreshCw size={13} className={actionBusy === `${onu.index}:sync` ? "animate-spin" : ""} />Sync</button>
                        <button
                          className="rounded-md p-1.5 text-[#607067] transition-colors hover:bg-[#eef7f3] hover:text-[#1d5c43] disabled:opacity-40"
                          title="Reset ONU (prefer SNMP)"
                          disabled={actionBusy === `${onu.index}:reset`}
                          onClick={() => onuAction(onu, "reset", `Reset ONU ${onu.name || onu.index}?`) }
                        ><Power size={15} /></button>
                        <button
                          className="rounded-md p-1.5 text-[#607067] transition-colors hover:bg-[#fef7ed] hover:text-amber-700 disabled:opacity-40"
                          title="Reboot ONU (prefer SNMP)"
                          disabled={actionBusy === `${onu.index}:reboot`}
                          onClick={() => onuAction(onu, "reboot", `Reboot ONU ${onu.name || onu.index}?`) }
                        ><RotateCcw size={15} /></button>
                        <button
                          className="rounded-md p-1.5 text-[#607067] transition-colors hover:bg-[#fef2f2] hover:text-red-600 disabled:opacity-40"
                          title="Hapus Config ONU (CLI no onu)"
                          disabled={actionBusy === `${onu.index}:delete`}
                          onClick={() => onuAction(onu, "delete", `Hapus config ONU ${onu.name || onu.index}? Ini akan remove ONU dari OLT.`) }
                        ><Trash2 size={15} /></button>
                      </div>
                    </td>
                  </tr>
                  {isOpen && (
                    <tr key={`${onu.index}-detail`} className="bg-[#f8fbfa]">
                      <td colSpan={8} className="px-3 py-3">
                        <OnuTrafficDetail
                          olt={olt}
                          onu={onu}
                          live={liveTraffic}
                          onLiveDetail={(patch) => {
                            setData((prev) => ({
                              ...prev,
                              items: prev.items.map((row) => row.index === onu.index ? applyOnuPatch(row, patch) : row),
                            }));
                          }}
                        />
                      </td>
                    </tr>
                  )}
                </Fragment>
              );
            })}
            {data.items.length === 0 && !loading ? (
              <tr><td colSpan={8} className="py-10 text-center text-sm text-[#8aa096]">Tidak ada data. Jalankan <b>Sync ONU</b> terlebih dahulu.</td></tr>
            ) : null}
          </tbody>
        </table>
      </div>

      {/* Pagination */}
      <div className="mt-3 flex flex-wrap items-center justify-between gap-2 text-sm text-[#607067]">
        <span>{data.total.toLocaleString("id-ID")} ONU · halaman {page}/{pageCount}{loading ? " · memuat…" : ""}</span>
        <div className="flex items-center gap-1">
          <button className="rounded-lg border border-[#d5e2dc] bg-white px-2.5 py-1.5 shadow-sm transition hover:border-[#2c7a5b] disabled:opacity-40" disabled={page <= 1} onClick={() => setPage(1)}>«</button>
          <button className="rounded-lg border border-[#d5e2dc] bg-white p-1.5 shadow-sm transition hover:border-[#2c7a5b] disabled:opacity-40" disabled={page <= 1} onClick={() => setPage(page - 1)} aria-label="Sebelumnya"><ChevronLeft size={15} /></button>
          <button className="rounded-lg border border-[#d5e2dc] bg-white p-1.5 shadow-sm transition hover:border-[#2c7a5b] disabled:opacity-40" disabled={page >= pageCount} onClick={() => setPage(page + 1)} aria-label="Berikutnya"><ChevronRight size={15} /></button>
          <button className="rounded-lg border border-[#d5e2dc] bg-white px-2.5 py-1.5 shadow-sm transition hover:border-[#2c7a5b] disabled:opacity-40" disabled={page >= pageCount} onClick={() => setPage(pageCount)}>»</button>
        </div>
      </div>
    </div>
  );
}
