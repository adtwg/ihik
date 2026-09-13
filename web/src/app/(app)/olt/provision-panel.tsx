"use client";

// Panel provisioning ONU via CLI hybrid (SSH/Telnet):
// 1. Deteksi ONU unconfigured secara bulk
// 2. Tampilkan SN + port + rekomendasi ONU ID kosong
// 3. Provision ONU dari hasil scan

import { useState } from "react";
import { Radar, UserPlus } from "lucide-react";
import { clientAPI } from "@/lib/api/client";
import type { OLT } from "@/lib/olts/types";

type Unconfigured = {
  serial_number: string;
  pon_port?: string;
  onu_id_hint?: number;
  suggested_onu_id?: number;
};

type PortSummary = {
  pon_port: string;
  occupied_onu_ids?: number[];
  empty_onu_ids?: number[];
  next_onu_id?: number;
  unconfigured_count?: number;
};

type DetectResponse = {
  items: Unconfigured[];
  ports?: PortSummary[];
  total_unconfigured?: number;
};

export function ProvisionPanel({ olt }: { olt: OLT }) {
  const [open, setOpen] = useState(false);
  const [pon, setPon] = useState("");
  const [busy, setBusy] = useState<string | null>(null);
  const [message, setMessage] = useState<{ text: string; error?: boolean } | null>(null);
  const [unconfigured, setUnconfigured] = useState<Unconfigured[] | null>(null);
  const [portSummaries, setPortSummaries] = useState<PortSummary[]>([]);
  const [form, setForm] = useState({ onu_id: "1", type: "ZTE-F670L", serial: "", description: "" });

  async function detect() {
    setBusy("detect");
    setMessage(null);
    try {
      const query = new URLSearchParams();
      const trimmedPON = pon.trim();
      if (trimmedPON) query.set("pon", trimmedPON);
      const suffix = query.toString();
      const result = await clientAPI<DetectResponse>(
        `/api/v1/olts/${olt.id}/uncfg${suffix ? `?${suffix}` : ""}`,
      );
      const items = result.items ?? [];
      const ports = result.ports ?? [];
      const total = result.total_unconfigured ?? items.length;
      setUnconfigured(items);
      setPortSummaries(ports);
      setMessage({ text: `${total} ONU uncfg terdeteksi${trimmedPON ? ` di ${trimmedPON}` : " (bulk semua PON)"}.` });
    } catch (error) {
      setMessage({ text: error instanceof Error ? error.message : "Gagal mendeteksi.", error: true });
    } finally {
      setBusy(null);
    }
  }

  async function provision(serialOverride?: string, source?: Unconfigured) {
    const serial = (serialOverride || form.serial).trim().toUpperCase();
    const rawOnuID = (form.onu_id || "").trim();
    const fullOnuRef = rawOnuID.match(/^(\d+\/\d+\/\d+)\s*:\s*(\d+)$/);
    const parsedPON = fullOnuRef?.[1]?.trim() || "";
    const parsedOnuID = fullOnuRef ? Number(fullOnuRef[2]) : Number(rawOnuID);

    const targetPON = (source?.pon_port || pon || parsedPON || "").trim();
    const targetOnuID = Number(source?.suggested_onu_id || source?.onu_id_hint || parsedOnuID);

    if (!serial) {
      setMessage({ text: "Serial wajib diisi.", error: true });
      return;
    }
    if (!targetPON) {
      setMessage({ text: "Port PON wajib untuk provision manual. Isi Port PON atau format ONU ID lengkap (contoh: 1/1/1:7).", error: true });
      return;
    }
    if (!Number.isFinite(targetOnuID) || targetOnuID < 1) {
      setMessage({ text: "ONU ID tidak valid.", error: true });
      return;
    }

    if (!window.confirm(`Provision ONU ${serial} di ${targetPON} onu-id ${targetOnuID}?`)) return;

    setBusy("provision");
    setMessage(null);
    try {
      await clientAPI(`/api/v1/olts/${olt.id}/provision-onu`, {
        method: "POST",
        body: JSON.stringify({
          pon: targetPON,
          onu_id: targetOnuID,
          type: form.type,
          serial,
          description: form.description,
        }),
      });
      setPon(targetPON);
      setForm((prev) => ({ ...prev, serial, onu_id: String(targetOnuID) }));
      setMessage({ text: `ONU ${serial} berhasil diprovision di ${targetPON}:${targetOnuID}.` });
      setUnconfigured((current) =>
        current ? current.filter((item) => !(item.serial_number === serial && (item.pon_port || "") === targetPON)) : current,
      );
    } catch (error) {
      setMessage({ text: error instanceof Error ? error.message : "Gagal provision.", error: true });
    } finally {
      setBusy(null);
    }
  }

  return (
    <div className="mb-3 rounded-xl border border-[#dbe7e1] bg-[#f6faf8] p-3 shadow-sm">
      <button className="flex items-center gap-2 text-sm font-bold text-[#10251d]" onClick={() => setOpen(!open)}>
        <UserPlus size={16} /> Bulk Config ONU (CLI {olt.snmp_mode === "v2c" ? "SSH/Telnet" : olt.cli_protocol ?? "SSH"})
        {open ? " ▲" : " ▼"}
      </button>

      {open ? (
        <div className="mt-3 space-y-3">
          <div className="flex flex-wrap items-end gap-2">
            <label className="text-xs font-semibold">Port PON (opsional)
              <input
                className="search-input mt-1 block w-40"
                value={pon}
                onChange={(e) => setPon(e.target.value)}
                placeholder="kosongkan = semua"
              />
            </label>
            <button className="primary-button" onClick={detect} disabled={busy !== null}>
              <Radar size={16} /> {busy === "detect" ? "Mendeteksi…" : "Cek ONU Uncfg (Bulk)"}
            </button>
          </div>

          {message ? (
            <div className={`rounded-md px-3 py-2 text-sm font-semibold ${message.error ? "bg-red-100 text-red-800" : "bg-emerald-100 text-emerald-800"}`}>
              {message.text}
            </div>
          ) : null}

          {portSummaries.length > 0 ? (
            <div className="rounded-md border border-[#d5e2dc] bg-white p-3">
              <p className="mb-2 text-sm font-bold text-[#10251d]">Ringkasan slot ONU ID kosong per port</p>
              <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 xl:grid-cols-3">
                {portSummaries.map((port) => {
                  const empty = port.empty_onu_ids ?? [];
                  const preview = empty.slice(0, 12).join(", ");
                  const more = empty.length > 12 ? ` (+${empty.length - 12} lagi)` : "";
                  return (
                    <div key={port.pon_port} className="rounded-lg border border-[#e5eeea] bg-[#fbfefd] p-2.5 text-xs">
                      <div className="font-mono font-bold text-[#17382d]">{port.pon_port}</div>
                      <div className="mt-1 text-[#44554d]">ONU uncfg: <b>{port.unconfigured_count ?? 0}</b></div>
                      <div className="text-[#44554d]">Next ONU ID: <b>{port.next_onu_id ?? "-"}</b></div>
                      <div className="mt-1 text-[#607067]">Kosong: {preview || "-"}{more}</div>
                    </div>
                  );
                })}
              </div>
            </div>
          ) : null}

          {unconfigured && unconfigured.length > 0 ? (
            <div className="rounded-md border border-amber-300 bg-amber-50 p-3">
              <p className="mb-2 text-sm font-bold text-amber-900">ONU unconfigured terdeteksi:</p>
              <div className="overflow-x-auto">
                <table className="w-full min-w-[560px] text-xs">
                  <thead>
                    <tr className="text-left text-amber-900">
                      <th className="px-2 py-1">SN</th>
                      <th className="px-2 py-1">Port</th>
                      <th className="px-2 py-1">Hint ID</th>
                      <th className="px-2 py-1">Saran ID Kosong</th>
                      <th className="px-2 py-1">Aksi</th>
                    </tr>
                  </thead>
                  <tbody>
                    {unconfigured.map((item, idx) => (
                      <tr key={`${item.serial_number}-${item.pon_port || "-"}-${idx}`} className="border-t border-amber-200">
                        <td className="px-2 py-1 font-mono font-semibold">{item.serial_number}</td>
                        <td className="px-2 py-1 font-mono">{item.pon_port || "-"}</td>
                        <td className="px-2 py-1">{item.onu_id_hint ?? "-"}</td>
                        <td className="px-2 py-1 font-semibold">{item.suggested_onu_id ?? "-"}</td>
                        <td className="px-2 py-1">
                          <button
                            className="primary-button !py-1 !text-xs"
                            onClick={() => {
                              const nextID = item.suggested_onu_id || item.onu_id_hint || Number(form.onu_id) || 1;
                              setPon(item.pon_port || pon);
                              setForm((f) => ({ ...f, serial: item.serial_number, onu_id: String(nextID) }));
                              void provision(item.serial_number, item);
                            }}
                            disabled={busy !== null}
                          >
                            Daftarkan
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              <p className="mt-2 text-xs text-amber-800">Klik "Daftarkan" untuk provision otomatis per baris memakai port + ONU ID kosong yang disarankan.</p>
            </div>
          ) : null}

          <div className="grid grid-cols-1 gap-2 sm:grid-cols-5">
            <label className="text-xs font-semibold">ONU ID (angka atau full 1/1/1:7)<input className="search-input mt-1 w-full" value={form.onu_id} onChange={(e) => setForm({ ...form, onu_id: e.target.value })} placeholder="contoh: 7 atau 1/1/1:7" /></label>
            <label className="text-xs font-semibold">Tipe ONU<input className="search-input mt-1 w-full" value={form.type} onChange={(e) => setForm({ ...form, type: e.target.value })} /></label>
            <label className="text-xs font-semibold sm:col-span-2">Serial (mis. ZTEGC12345678)<input className="search-input mt-1 w-full font-mono" value={form.serial} onChange={(e) => setForm({ ...form, serial: e.target.value.toUpperCase() })} /></label>
            <label className="text-xs font-semibold">Deskripsi<input className="search-input mt-1 w-full" value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} placeholder="Nama pelanggan" /></label>
          </div>
          <button className="primary-button" onClick={() => provision()} disabled={busy !== null}>
            <UserPlus size={16} /> {busy === "provision" ? "Memprovision…" : "Provision ONU Manual"}
          </button>
        </div>
      ) : null}
    </div>
  );
}
