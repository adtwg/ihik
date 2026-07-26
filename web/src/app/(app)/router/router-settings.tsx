"use client";

import { useState, type FormEvent } from "react";
import { useRouter } from "next/navigation";
import { CheckCircle2, PlugZap, Save, XCircle } from "lucide-react";
import { clientAPI } from "@/lib/api/client";
import { formatDateTime } from "@/lib/format";
import type { RouterConfig, RouterTestResult } from "@/lib/router/types";

export function RouterSettings({ config }: { config: RouterConfig }) {
  const router = useRouter();
  const [pending, setPending] = useState(false);
  const [testing, setTesting] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [testResult, setTestResult] = useState<RouterTestResult | null>(null);
  const [useTLS, setUseTLS] = useState(Boolean(config.use_tls));

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setPending(true); setError(""); setMessage(""); setTestResult(null);
    const form = new FormData(event.currentTarget);
    try {
      await clientAPI("/api/v1/router", { method: "PUT", body: JSON.stringify({
        name: form.get("name"),
        host: form.get("host"),
        api_port: Number(form.get("api_port") || 0),
        use_tls: form.get("use_tls") === "on",
        api_username: form.get("api_username"),
        api_password: form.get("api_password") || "",
      }) });
      setMessage("Pengaturan router tersimpan.");
      router.refresh();
    } catch (cause) { setError(cause instanceof Error ? cause.message : "Pengaturan belum dapat disimpan."); }
    finally { setPending(false); }
  }

  async function test() {
    setTesting(true); setError(""); setMessage(""); setTestResult(null);
    try {
      const result = await clientAPI<RouterTestResult>("/api/v1/router/test", { method: "POST" });
      setTestResult(result);
      router.refresh();
    } catch (cause) { setError(cause instanceof Error ? cause.message : "Router tidak dapat dihubungi."); }
    finally { setTesting(false); }
  }

  return (
    <div className="dashboard-grid">
      <section className="panel">
        <div className="panel-header"><h2>Koneksi API</h2></div>
        <form className="panel-body grid gap-4" onSubmit={save}>
          {error && <div className="error-box" role="alert">{error}</div>}
          {message && <div className="rounded-md bg-[#edf7f2] p-3 text-sm font-semibold text-[#096b4c]">{message}</div>}
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="field"><label htmlFor="router-name">Nama router</label><input id="router-name" name="name" maxLength={100} defaultValue={config.name ?? "Router utama"} /></div>
            <div className="field"><label htmlFor="router-host">Alamat (IP / hostname)</label><input id="router-host" name="host" required maxLength={253} defaultValue={config.host ?? ""} placeholder="mis. 192.168.88.1" /></div>
          </div>
          <div className="grid gap-4 sm:grid-cols-3">
            <div className="field"><label htmlFor="router-port">Port API</label><input id="router-port" name="api_port" type="number" min={1} max={65535} defaultValue={config.api_port || (useTLS ? 8729 : 8728)} /></div>
            <div className="field"><label htmlFor="router-user">Username API</label><input id="router-user" name="api_username" required maxLength={100} defaultValue={config.api_username ?? ""} placeholder="mis. billing-api" /></div>
            <div className="field"><label htmlFor="router-password">Password API</label><input id="router-password" name="api_password" type="password" maxLength={200} placeholder={config.configured ? "(tidak diubah)" : ""} required={!config.configured} /></div>
          </div>
          <label className="flex items-center gap-2 text-sm"><input type="checkbox" name="use_tls" checked={useTLS} onChange={(event) => setUseTLS(event.target.checked)} /> Gunakan API-SSL (port 8729)</label>
          <p className="text-xs text-[#607067]">Buat user API khusus di MikroTik dengan grup <strong>full</strong> (dibutuhkan untuk baca secret dan isolir): <code>/user add name=billing-api group=full password=…</code></p>
          <div className="flex flex-wrap gap-2">
            <button className="primary-button" disabled={pending}><Save size={17} /> {pending ? "Menyimpan..." : "Simpan"}</button>
            <button className="secondary-button" type="button" disabled={testing || !config.configured} onClick={test}><PlugZap size={17} /> {testing ? "Menguji..." : "Tes koneksi"}</button>
          </div>
        </form>
      </section>
      <section className="panel">
        <div className="panel-header"><h2>Status</h2></div>
        <div className="panel-body grid gap-3 text-sm">
          {!config.configured && <p className="text-[#607067]">Router belum dikonfigurasi. Simpan pengaturan lalu jalankan tes koneksi.</p>}
          {config.configured && <>
            <div className="flex items-center gap-2">{config.last_error ? <XCircle size={18} className="text-[#b3261e]" /> : <CheckCircle2 size={18} className="text-[#096b4c]" />}<strong>{config.last_error ? "Gangguan koneksi" : "Terkonfigurasi"}</strong></div>
            {config.identity && <p>Identitas: <strong>{config.identity}</strong>{config.routeros_version ? ` · RouterOS ${config.routeros_version}` : ""}</p>}
            {config.last_connected_at && <p>Terakhir terhubung: {formatDateTime(config.last_connected_at)}</p>}
            {config.last_error && <p className="text-[#b3261e]">{config.last_error}</p>}
          </>}
          {testResult && <div className="rounded-md bg-[#edf7f2] p-3">
            <p className="font-semibold text-[#096b4c]">Koneksi berhasil</p>
            <p>Identitas: {testResult.identity || "-"} · RouterOS {testResult.routeros_version || "-"}</p>
            <p>{testResult.secret_count.toLocaleString("id-ID")} secret PPPoE · {testResult.profile_count.toLocaleString("id-ID")} profile</p>
            <p className="mt-1 text-xs text-[#607067]">Lanjutkan ke menu Pelanggan lalu klik “Sync PPPoE”.</p>
          </div>}
          <div className="rounded-md bg-[#f4f7f5] p-3 text-xs text-[#607067]">
            <p className="mb-1 font-bold uppercase">Cara kerja Sync PPPoE</p>
            <p>Profile menjadi Paket — harga dibaca dari nama profil berpola <strong>150K</strong> → Rp150.000. Secret menjadi Pelanggan (nama diambil dari comment, atau username bila kosong) lengkap dengan layanan dan akun PPPoE-nya. Secret yang disabled tercatat sebagai terisolir.</p>
          </div>
        </div>
      </section>
    </div>
  );
}
