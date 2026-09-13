"use client";

// Halaman OLT — gaya tool monitoring: padat, fungsional, tanpa dekorasi.

import { useEffect, useMemo, useState, type FormEvent } from "react";
import { Activity, ChevronDown, ClipboardCopy, Cpu, Pencil, Plus, RefreshCw, TerminalSquare, Trash2, X } from "lucide-react";
import { useRouter } from "next/navigation";
import { clientAPI } from "@/lib/api/client";
import { type OLT, type OltCLITestInfo, type SystemInfo } from "@/lib/olts/types";
import { OnuTable } from "./onu-table";
import { OltHealthPanel } from "./olt-health-panel";

function buildSnmpScript(olt: OLT): string {
  const ip = "<IP-SERVER-BILLING>";
  // Script TANPA komentar — baris `!` kadang terpotong terminal dan
  // dieksekusi parsial. Semua penjelasan ada di bawah popup.

  if (olt.snmp_mode === "v2c") {
    return `enable
config
snmp-server community B1llingPub view AllView ro
snmp-server community B1llingPriv view AllView rw
snmp-server host ${ip} trap version 2c B1llingPub enable NOTIFICATIONS target-addr-name BILLINGTRAP udp-port 162
exit
write
show snmp config`;
  }

  return `enable
config
snmp-server group BILLINGGRP v3 auth read AllView write AllView notify AllView
snmp-server host ${ip} traps version 2c B1llingPub enable NOTIFICATIONS target-addr-name BILLINGTRAP udp-port 162
snmp-server user ${olt.v3_username || "billing"} BILLINGGRP auth sha <AUTH-PASS> priv aes-128 <PRIV-PASS>
exit
write
show snmp config`;
}

function Modal({ title, onClose, children }: { title: string; onClose: () => void; children: React.ReactNode }) {
  return (
    <div className="fixed inset-0 z-50 flex items-end justify-center bg-black/40 sm:items-center sm:p-4" role="dialog" aria-modal="true">
      <div className="flex h-[92vh] w-full flex-col rounded-t-lg bg-white shadow-xl sm:h-auto sm:max-h-[90vh] sm:max-w-xl sm:rounded-lg">
        <header className="flex items-center justify-between border-b border-[#e5eeea] px-4 py-2.5">
          <h2 className="text-sm font-bold">{title}</h2>
          <button className="rounded p-1 hover:bg-[#f0f7f4]" onClick={onClose} aria-label="Tutup"><X size={18} /></button>
        </header>
        <div className="flex-1 overflow-y-auto overscroll-contain p-4">{children}</div>
      </div>
    </div>
  );
}

export function OltManager({ initialOlts, loading, error }: { initialOlts: OLT[]; loading?: boolean; error?: string | null }) {
  const router = useRouter();
  const [olts, setOlts] = useState(initialOlts);
  const [selectedID, setSelectedID] = useState<string | null>(initialOlts[0]?.id ?? null);

  useEffect(() => {
    setOlts(initialOlts);
  }, [initialOlts]);
  const [formOpen, setFormOpen] = useState(false);
  const [scriptOpen, setScriptOpen] = useState(false);
  const [editOLT, setEditOLT] = useState<OLT | null>(null);
  const [showHealth, setShowHealth] = useState(false);
  const [copied, setCopied] = useState(false);
  // Busyness wajib scoped per OLT + per action. Busyness global membuat
  // operasi di OLT A melumpuhkan tombol di OLT B dan terkesan web macet.
  const [busy, setBusy] = useState<Record<string, string>>({});
  const [notice, setNotice] = useState<{ text: string; error?: boolean } | null>(null);

  const selected = olts.find((o) => o.id === selectedID) ?? null;

  useEffect(() => {
    if (!selected && olts.length > 0) setSelectedID(olts[0].id);
  }, [olts, selected]);

  // Bersihkan notice otomatis setelah beberapa detik supaya tidak mengaburkan UI.
  useEffect(() => {
    if (!notice) return;
    const t = window.setTimeout(() => setNotice(null), 7000);
    return () => window.clearTimeout(t);
  }, [notice]);

  function busyKey(oltID: string, action: string) { return `${oltID}:${action}`; }
  function isBusy(oltID: string, action: string) { return busy[busyKey(oltID, action)] === "1"; }
  function setBusyKey(oltID: string, action: string, value: boolean) {
    const k = busyKey(oltID, action);
    setBusy((prev) => {
      const next = { ...prev };
      if (value) next[k] = "1"; else delete next[k];
      return next;
    });
  }

  async function run(oltID: string, action: string, fn: () => Promise<string>, { refresh = true }: { refresh?: boolean } = {}) {
    const k = busyKey(oltID, action);
    if (busy[k] === "1") return;
    setBusyKey(oltID, action, true);
    setNotice(null);
    try {
      const msg = await fn();
      setNotice({ text: msg });
      if (refresh) router.refresh();
    } catch (error) {
      let msg = error instanceof Error ? error.message : "Terjadi kesalahan.";
      // Khusus APIError: abaikan stack-ish message raw HTTP; tampilkan message backend.
      if (error && typeof error === "object" && "message" in error) {
        msg = String(error.message);
      }
      setNotice({ text: msg, error: true });
    } finally {
      setBusyKey(oltID, action, false);
    }
  }

  function testOLT() {
    if (!selected) return;
    void run(selected.id, "test-snmp", async () => {
      const info = await clientAPI<SystemInfo>(`/api/v1/olts/${selected.id}/test`, { method: "POST" });
      return `SNMP ✅ tersambung: ${info.model_hint || "model tidak dikenal"} · uptime ${info.sys_uptime}`;
    });
  }

  function testCLI() {
    if (!selected) return;
    void run(selected.id, "test-cli", async () => {
      const info = await clientAPI<OltCLITestInfo>(`/api/v1/olts/${selected.id}/test-cli`, { method: "POST" });
      const proto = (info.protocol || "cli").toUpperCase();
      return `${proto} ✅ tersambung: ${info.username}@${info.host}:${info.port} · cmd: ${info.command}`;
    });
  }

  function syncONUs() {
    if (!selected) return;
    void run(selected.id, "sync", async () => {
      const r = await clientAPI<{ ok: boolean; message: string }>(`/api/v1/olts/${selected.id}/sync-onus`, { method: "POST" });
      return r.message || "Sinkronisasi ONU berjalan di background.";
    }, { refresh: false });
  }

  function deleteOLT(olt: OLT) {
    if (!window.confirm(`Hapus OLT ${olt.name}?`)) return;
    void run(olt.id, "del", async () => {
      await clientAPI(`/api/v1/olts/${olt.id}`, { method: "DELETE" });
      const remaining = olts.filter((o) => o.id !== olt.id);
      setOlts(remaining);
      setSelectedID(remaining[0]?.id ?? null);
      return `${olt.name} dihapus.`;
    });
  }

  function updateOLT(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!editOLT) return;
    const form = new FormData(event.currentTarget);
    void run(editOLT.id, "edit", async () => {
      const updated = await clientAPI<OLT>(`/api/v1/olts/${editOLT.id}`, {
        method: "PUT",
        body: JSON.stringify({
          name: form.get("name"),
          host: form.get("host"),
          port: Number(form.get("port")) || 161,
          model: form.get("model"),
          snmp_mode: form.get("snmp_mode"),
          community: form.get("community") || "",
          v3_username: form.get("v3_username") || "",
          v3_auth_protocol: form.get("v3_auth_protocol") || "sha",
          v3_auth_passphrase: form.get("v3_auth_passphrase") || "",
          v3_priv_protocol: form.get("v3_priv_protocol") || "aes",
          v3_priv_passphrase: form.get("v3_priv_passphrase") || "",
          cli_protocol: form.get("cli_protocol") || "ssh",
          cli_port: Number(form.get("cli_port")) || 22,
          cli_username: form.get("cli_username") || "",
          cli_password: form.get("cli_password") || "",
        }),
      });
      setOlts((current) => current.map((o) => (o.id === updated.id ? updated : o)));
      setEditOLT(null);
      return `✅ ${updated.name} diperbarui.`;
    });
  }

  async function copyScript(text: string) {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    } catch {
      window.prompt("Salin manual:", text);
    }
  }

  function submitForm(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    void run("new", "create", async () => {
      const created = await clientAPI<OLT>("/api/v1/olts", {
        method: "POST",
        body: JSON.stringify({
          name: form.get("name"),
          host: form.get("host"),
          port: Number(form.get("port")) || 161,
          model: form.get("model"),
          snmp_mode: form.get("snmp_mode"),
          community: form.get("community") || "",
          v3_username: form.get("v3_username") || "",
          v3_auth_protocol: form.get("v3_auth_protocol") || "sha",
          v3_auth_passphrase: form.get("v3_auth_passphrase") || "",
          v3_priv_protocol: form.get("v3_priv_protocol") || "aes",
          v3_priv_passphrase: form.get("v3_priv_passphrase") || "",
          cli_protocol: form.get("cli_protocol") || "ssh",
          cli_port: Number(form.get("cli_port")) || 22,
          cli_username: form.get("cli_username") || "",
          cli_password: form.get("cli_password") || "",
        }),
      });
      setOlts((current) => [created, ...current]);
      setSelectedID(created.id);
      setFormOpen(false);
      return `${created.name} ditambahkan. Klik "Script" untuk perintah konfigurasi SNMP di OLT.`;
    });
  }

  const isBusyAny = useMemo(() => Object.keys(busy).some((k) => busy[k] === "1"), [busy]);

  const inputCls = "mt-1 w-full rounded border border-[#d5e2dc] bg-white px-3 py-2 text-sm";
  const labelCls = "block text-xs font-semibold text-[#44554d]";
  const btn = "flex items-center gap-1.5 rounded border border-[#d5e2dc] bg-white px-2.5 py-1.5 text-xs font-semibold text-[#10251d] hover:border-[#2c7a5b] disabled:opacity-40";

  return (
    <>
      {notice ? (
        <div className={`sticky top-14 z-30 mb-3 rounded border px-3 py-2 text-sm font-medium ${notice.error ? "border-red-200 bg-red-50 text-red-800" : "border-emerald-200 bg-emerald-50 text-emerald-800"}`}>
          {notice.text}
        </div>
      ) : null}

      {error ? (
        <div className="mb-3 rounded border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-800">
          {error}
        </div>
      ) : null}

      {loading && olts.length === 0 ? (
        <div className="mb-3 rounded border border-[#dbe7e1] bg-white p-4 text-sm text-[#607067]">
          Memuat daftar OLT...
        </div>
      ) : null}

      {olts.length > 0 ? (
        <div className="mb-3 flex flex-col gap-2 rounded-lg border border-[#dbe7e1] bg-white p-3 sm:flex-row sm:items-center sm:justify-between">
          <label className="flex min-w-0 flex-1 items-center gap-2">
            <span className="text-xs font-semibold text-[#8aa096]">OLT</span>
            <select
              className="w-full rounded border border-[#d5e2dc] bg-white px-2.5 py-2 text-sm font-semibold"
              value={selectedID ?? ""}
              onChange={(e) => { setSelectedID(e.target.value); setNotice(null); }}
              aria-label="Pilih OLT"
            >
              {olts.map((olt) => (
                <option key={olt.id} value={olt.id}>{olt.name} — {olt.host}</option>
              ))}
            </select>
          </label>
          <div className="grid grid-cols-3 gap-1.5 sm:flex sm:flex-row">
            <button className={btn} onClick={() => setScriptOpen(true)} disabled={!selected}><TerminalSquare size={14} /> Script</button>
            <button className={btn} onClick={() => selected && setEditOLT(selected)} disabled={!selected}><Pencil size={14} /> Edit</button>
            <button className={btn} onClick={testOLT} disabled={isBusyAny || !selected}><Activity size={14} /> Tes SNMP</button>
            <button className={btn} onClick={testCLI} disabled={isBusyAny || !selected}><TerminalSquare size={14} /> Tes CLI</button>
            <button className={btn} onClick={syncONUs} disabled={isBusy(selected?.id ?? "", "sync") || !selected}>
              <RefreshCw size={14} className={isBusy(selected?.id ?? "", "sync") ? "animate-spin" : ""} /> Sync
            </button>
            <button className={`${btn} text-red-700 hover:border-red-300`} onClick={() => selected && deleteOLT(selected)} disabled={isBusyAny || !selected}>
              <Trash2 size={14} /> Hapus
            </button>
          </div>
        </div>
      ) : null}

      {selected ? (
        <section className="rounded-lg border border-[#dbe7e1] bg-white">
          {/* Info OLT: satu baris definisi list, bukan kartu */}
          <div className="border-b border-[#eef4f1] px-4 py-2.5 text-xs text-[#607067]">
            <span className="font-semibold text-[#10251d]">{selected.name}</span>
            {" · "}{selected.host}:{selected.port}
            {" · "}{selected.model}
            {" · SNMP "}{String(selected.snmp_mode || "v2c").toUpperCase()}
            {selected.cli_username ? ` · CLI ${selected.cli_protocol ?? "ssh"}` : ""}
            {selected.last_error ? <span className="ml-2 text-red-700">error: {selected.last_error}</span> : null}
          </div>

          <div className="p-4">
            {/* Detail OLT (health) — collapsible, di atas tabel ONU */}
            <button
              className="mb-3 flex w-full items-center justify-between rounded-lg border border-[#d5e2dc] bg-gradient-to-r from-white to-[#f6faf8] px-4 py-2.5 text-sm font-bold text-[#10251d] shadow-sm transition-colors hover:border-[#2c7a5b]"
              onClick={() => setShowHealth(!showHealth)}
              aria-expanded={showHealth}
            >
              <span className="flex items-center gap-2"><Cpu size={16} /> Detail OLT — Card, Resource &amp; SFP Diagnostics</span>
              <ChevronDown size={16} className={`transition-transform ${showHealth ? "rotate-180" : ""}`} />
            </button>
            {showHealth ? (
              <div className="mb-4">
                <OltHealthPanel olt={selected} />
              </div>
            ) : null}

            <h3 className="mb-3 text-sm font-bold text-[#10251d]">Monitoring ONU — {selected.name}</h3>
            <OnuTable olt={selected} />
          </div>
        </section>
      ) : null}

      {olts.length === 0 && !loading ? (
        <div className="rounded-lg border border-dashed border-[#d5e2dc] p-10 text-center">
          <p className="text-sm text-[#607067]">Belum ada OLT terdaftar.</p>
          <button className="primary-button mt-4" onClick={() => setFormOpen(true)}>
            <Plus size={16} /> Tambah OLT
          </button>
        </div>
      ) : null}

      {olts.length > 0 ? (
        <button
          className="primary-button fixed bottom-5 right-5 z-40 !rounded-full !px-4 !py-3 shadow-lg"
          onClick={() => setFormOpen(true)}
          title="Tambah OLT"
        >
          <Plus size={18} />
        </button>
      ) : null}

      {/* Form Tambah */}
      {formOpen ? (
        <Modal title="Tambah OLT" onClose={() => setFormOpen(false)}>
          <form className="grid grid-cols-1 gap-3 sm:grid-cols-2" onSubmit={submitForm}>
            <label className={`${labelCls} sm:col-span-2`}>Nama<input name="name" required maxLength={100} className={inputCls} placeholder="OLT-CORE-1" /></label>
            <label className={labelCls}>Host / IP<input name="host" required maxLength={253} className={inputCls} placeholder="10.10.10.5" inputMode="url" /></label>
            <label className={labelCls}>Port SNMP<input name="port" type="number" min={1} max={65535} defaultValue={161} className={inputCls} inputMode="numeric" /></label>
            <label className={labelCls}>Model<select name="model" className={inputCls}><option>ZTE-C320</option><option>ZTE-C300</option><option>ZTE-C600</option></select></label>
            <label className={labelCls}>Mode SNMP<select name="snmp_mode" className={inputCls} defaultValue="v3"><option value="v3">v3</option><option value="v2c">v2c</option></select></label>

            <fieldset className="sm:col-span-2">
              <legend className="mb-1 text-xs font-bold uppercase tracking-wide text-[#8aa096]">SNMP v3</legend>
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <label className={labelCls}>Username<input name="v3_username" className={inputCls} placeholder="billing" autoComplete="off" /></label>
                <label className={labelCls}>Auth<select name="v3_auth_protocol" className={inputCls} defaultValue="sha"><option value="sha">SHA</option><option value="sha256">SHA-256</option><option value="sha512">SHA-512</option><option value="md5">MD5</option></select></label>
                <label className={labelCls}>Auth passphrase<input name="v3_auth_passphrase" type="password" minLength={8} className={inputCls} autoComplete="new-password" /></label>
                <label className={labelCls}>Priv<select name="v3_priv_protocol" className={inputCls} defaultValue="aes"><option value="aes">AES-128</option><option value="aes192">AES-192</option><option value="aes256">AES-256</option><option value="des">DES</option></select></label>
                <label className={`${labelCls} sm:col-span-2`}>Priv passphrase<input name="v3_priv_passphrase" type="password" minLength={8} className={inputCls} autoComplete="new-password" /></label>
              </div>
            </fieldset>

            <fieldset className="sm:col-span-2">
              <legend className="mb-1 text-xs font-bold uppercase tracking-wide text-[#8aa096]">CLI (SSH/Telnet) — provisioning</legend>
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <label className={labelCls}>Protokol<select name="cli_protocol" className={inputCls} defaultValue="ssh"><option value="ssh">SSH</option><option value="telnet">Telnet</option></select></label>
                <label className={labelCls}>Port<input name="cli_port" type="number" min={1} max={65535} defaultValue={22} className={inputCls} inputMode="numeric" /></label>
                <label className={labelCls}>Username<input name="cli_username" className={inputCls} autoComplete="off" /></label>
                <label className={labelCls}>Password<input name="cli_password" type="password" className={inputCls} autoComplete="new-password" /></label>
              </div>
            </fieldset>

            <label className={`${labelCls} sm:col-span-2`}>Community (v2c)<input name="community" className={inputCls} placeholder="public" /></label>

            <div className="sticky bottom-0 mt-2 flex justify-end gap-2 bg-white pt-3 sm:col-span-2">
              <button type="button" className="secondary-button" onClick={() => setFormOpen(false)}>Batal</button>
              <button type="submit" className="primary-button" disabled={busy["new:create"] === "1"}>
                {busy["new:create"] === "1" ? "Menyimpan…" : "Simpan"}
              </button>
            </div>
          </form>
        </Modal>
      ) : null}

      {/* Modal Edit OLT */}
      {editOLT ? (
        <Modal title={`Edit OLT — ${editOLT.name}`} onClose={() => setEditOLT(null)}>
          <form key={editOLT.id} className="grid grid-cols-1 gap-3 sm:grid-cols-2" onSubmit={updateOLT}>
            <label className={`${labelCls} sm:col-span-2`}>Nama<input name="name" required maxLength={100} defaultValue={editOLT.name} className={inputCls} /></label>
            <label className={labelCls}>Host / IP<input name="host" required maxLength={253} defaultValue={editOLT.host} className={inputCls} /></label>
            <label className={labelCls}>Port SNMP<input name="port" type="number" min={1} max={65535} defaultValue={editOLT.port} className={inputCls} /></label>
            <label className={labelCls}>Model<select name="model" defaultValue={editOLT.model} className={inputCls}><option>ZTE-C320</option><option>ZTE-C300</option><option>ZTE-C600</option></select></label>
            <label className={labelCls}>Mode SNMP<select name="snmp_mode" defaultValue={editOLT.snmp_mode} className={inputCls}><option value="v3">v3</option><option value="v2c">v2c</option></select></label>

            <fieldset className="sm:col-span-2">
              <legend className="mb-1 text-xs font-bold uppercase tracking-wide text-[#8aa096]">SNMP v3 — kosongkan passphrase bila tidak diubah</legend>
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <label className={labelCls}>Username<input name="v3_username" defaultValue={editOLT.v3_username ?? ""} className={inputCls} autoComplete="off" /></label>
                <label className={labelCls}>Auth<select name="v3_auth_protocol" defaultValue={editOLT.v3_auth_protocol ?? "sha"} className={inputCls}><option value="sha">SHA</option><option value="sha256">SHA-256</option><option value="sha512">SHA-512</option><option value="md5">MD5</option></select></label>
                <label className={labelCls}>Auth passphrase baru<input name="v3_auth_passphrase" type="password" className={`${inputCls} placeholder:text-[#a8bcb2]`} placeholder="(tidak diubah)" autoComplete="new-password" /></label>
                <label className={labelCls}>Priv<select name="v3_priv_protocol" defaultValue={editOLT.v3_priv_protocol ?? "aes"} className={inputCls}><option value="aes">AES-128</option><option value="aes192">AES-192</option><option value="aes256">AES-256</option><option value="des">DES</option></select></label>
                <label className={`${labelCls} sm:col-span-2`}>Priv passphrase baru<input name="v3_priv_passphrase" type="password" className={`${inputCls} placeholder:text-[#a8bcb2]`} placeholder="(tidak diubah)" autoComplete="new-password" /></label>
              </div>
            </fieldset>

            <fieldset className="sm:col-span-2">
              <legend className="mb-1 text-xs font-bold uppercase tracking-wide text-[#8aa096]">CLI Hybrid</legend>
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <label className={labelCls}>Protokol<select name="cli_protocol" defaultValue={editOLT.cli_protocol ?? "ssh"} className={inputCls}><option value="ssh">SSH</option><option value="telnet">Telnet</option></select></label>
                <label className={labelCls}>Port<input name="cli_port" type="number" min={1} max={65535} defaultValue={editOLT.cli_port ?? 22} className={inputCls} /></label>
                <label className={labelCls}>Username<input name="cli_username" defaultValue={editOLT.cli_username ?? ""} className={inputCls} autoComplete="off" /></label>
                <label className={labelCls}>Password baru<input name="cli_password" type="password" className={`${inputCls} placeholder:text-[#a8bcb2]`} placeholder="(tidak diubah)" autoComplete="new-password" /></label>
              </div>
            </fieldset>

            <label className={`${labelCls} sm:col-span-2`}>Community (v2c)<input name="community" className={inputCls} placeholder="(tidak diubah)" autoComplete="off" /></label>

            <div className="sticky bottom-0 mt-2 flex justify-end gap-2 bg-white pt-3 sm:col-span-2">
              <button type="button" className="secondary-button" onClick={() => setEditOLT(null)}>Batal</button>
              <button type="submit" className="primary-button" disabled={isBusy(editOLT.id, "edit")}>
                {isBusy(editOLT.id, "edit") ? "Menyimpan…" : "Simpan Perubahan"}
              </button>
            </div>
          </form>
        </Modal>
      ) : null}

      {/* Popup Script SNMP */}
      {scriptOpen && selected ? (
        <Modal title={`Script SNMP — ${selected.name}`} onClose={() => setScriptOpen(false)}>
          <p className="mb-3 text-sm text-[#44554d]">
            Tempel ke console OLT (<b>{selected.host}</b>), ganti teks <code className="rounded bg-[#f0f5f2] px-1">&lt;...&gt;</code>.
          </p>
          <pre className="max-h-[40vh] overflow-auto whitespace-pre-wrap break-all rounded border border-[#22352c] bg-[#10251d] p-3 font-mono text-xs leading-relaxed text-emerald-50">{buildSnmpScript(selected)}</pre>
          <div className="mt-2 space-y-1 text-xs text-[#8aa096]">
            <p>• Ganti <code className="rounded bg-[#f0f5f2] px-1">&lt;AUTH-PASS&gt;</code> / <code className="rounded bg-[#f0f5f2] px-1">&lt;PRIV-PASS&gt;</code> dengan passphrase yang sama seperti form Tambah OLT.</p>
            <p>• Ganti <code className="rounded bg-[#f0f5f2] px-1">&lt;IP-SERVER-BILLING&gt;</code> dengan IP server ini.</p>
            <p>• Baris <code>snmp-server user</code>: jika error, firmware memakai menu berbeda — jalankan <code className="rounded bg-[#f0f5f2] px-1">snmp-server ?</code> untuk lihat opsi user yang tersedia.</p>
            <p>• Mode v2c: ganti community <code className="rounded bg-[#f0f5f2] px-1">B1llingPub</code> sesuai keinginan, lalu samakan di aplikasi.</p>
          </div>
          <div className="mt-3 flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
            <button type="button" className="secondary-button w-full sm:w-auto" onClick={() => setScriptOpen(false)}>Tutup</button>
            <button type="button" className="primary-button w-full sm:w-auto" onClick={() => copyScript(buildSnmpScript(selected))}>
              <ClipboardCopy size={15} /> {copied ? "Tersalin" : "Salin"}
            </button>
          </div>
        </Modal>
      ) : null}
    </>
  );
}
