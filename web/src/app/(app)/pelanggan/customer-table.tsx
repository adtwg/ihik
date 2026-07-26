"use client";

import { startTransition, useEffect, useState, type FormEvent } from "react";
import { Archive, ArrowDown, ArrowUp, ChevronLeft, ChevronRight, Play, Plus, RefreshCw, Search, ShieldOff, Wifi, X } from "lucide-react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { clientAPI } from "@/lib/api/client";
import { formatIDR } from "@/lib/format";
import type { ConnectionStatus, Customer, CustomerPage, SyncSummary } from "@/lib/customers/types";
import type { Plan } from "@/lib/plans/types";

const serviceStatusLabels: Record<string, { label: string; className: string }> = {
  active: { label: "Aktif", className: "" },
  isolated: { label: "Terisolir", className: "danger" },
  pending_provisioning: { label: "Menunggu", className: "warning" },
  provisioning_failed: { label: "Gagal", className: "danger" },
};

const invoiceStatusLabels: Record<string, { label: string; className: string }> = {
  paid: { label: "Lunas", className: "" },
  partial: { label: "Sebagian", className: "info" },
  unpaid: { label: "Belum bayar", className: "warning" },
  overdue: { label: "Jatuh tempo", className: "danger" },
};

const sortableColumns = [
  { id: "customer_number", label: "Nomor" },
  { id: "name", label: "Pelanggan" },
] as const;

export function CustomerTable({ data, plans }: { data: CustomerPage; plans: Plan[] }) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const [search, setSearch] = useState(searchParams.get("search") ?? "");
  const [formOpen, setFormOpen] = useState(false);
  const [syncOpen, setSyncOpen] = useState(false);
  const [connectionTarget, setConnectionTarget] = useState<Customer | null>(null);

  function updateQuery(changes: Record<string, string | null>) {
    const next = new URLSearchParams(searchParams.toString());
    for (const [key, value] of Object.entries(changes)) value ? next.set(key, value) : next.delete(key);
    startTransition(() => router.replace(`${pathname}?${next.toString()}`));
  }

  useEffect(() => {
    if (search === (searchParams.get("search") ?? "")) return;
    const timer = window.setTimeout(() => updateQuery({ search: search || null, page: "1" }), 350);
    return () => window.clearTimeout(timer);
  }, [search, searchParams]);

  function toggleSort(column: string) {
    const nextOrder = data.sort === column && data.order === "asc" ? "desc" : "asc";
    updateQuery({ sort: column, order: nextOrder, page: "1" });
  }

  async function archiveCustomer(customer: Customer) {
    if (!window.confirm(`Arsipkan ${customer.name}? Histori keuangan tetap tersimpan.`)) return;
    await clientAPI(`/api/v1/customers/${customer.id}/archive`, { method: "POST" });
    router.refresh();
  }

  async function serviceAction(customer: Customer, action: "isolate" | "restore", confirmText: string) {
    if (!customer.service_id || !window.confirm(confirmText)) return;
    try {
      await clientAPI(`/api/v1/services/${customer.service_id}/${action}`, { method: "POST" });
      router.refresh();
    } catch (cause) {
      window.alert(cause instanceof Error ? cause.message : "Aksi gagal dijalankan.");
    }
  }

  const pageCount = Math.max(1, Math.ceil(data.filtered_total / data.page_size));

  function serviceBadge(customer: Customer) {
    if (!customer.service_status) return <span className="status-badge archived">Tanpa layanan</span>;
    const meta = serviceStatusLabels[customer.service_status] ?? { label: customer.service_status, className: "info" };
    return <span className={`status-badge ${meta.className}`}>{meta.label}</span>;
  }

  function billingBadge(customer: Customer) {
    if (!customer.last_invoice_number) return <span className="text-xs text-[#607067]">Belum ada tagihan</span>;
    const meta = invoiceStatusLabels[customer.last_invoice_status ?? ""] ?? { label: customer.last_invoice_status ?? "-", className: "info" };
    return (
      <div className="grid gap-1">
        <span className={`status-badge ${meta.className}`}>{meta.label}</span>
        {Number(customer.outstanding) > 0 && <span className="text-xs text-[#b3261e]">Sisa {formatIDR(customer.outstanding)}{customer.open_invoices > 1 ? ` · ${customer.open_invoices} tagihan` : ""}</span>}
      </div>
    );
  }

  return (
    <>
      <section className="table-shell">
        <div className="table-toolbar">
          <div className="relative w-full max-w-[380px]"><Search className="absolute left-3 top-3 text-[#607067]" size={18} /><input className="search-input pl-10" value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Cari nama, nomor, telepon, PPPoE" aria-label="Cari pelanggan" /></div>
          <div className="flex flex-wrap gap-2">
            <select className="secondary-button" value={searchParams.get("status") ?? ""} onChange={(event) => updateQuery({ status: event.target.value || null, page: "1" })} aria-label="Filter status"><option value="">Semua status</option><option value="active">Aktif</option><option value="isolated">Terisolir</option><option value="unpaid">Ada tunggakan</option><option value="no_service">Tanpa layanan</option></select>
            <button className="secondary-button" onClick={() => setSyncOpen(true)}><RefreshCw size={17} /> Sync PPPoE</button>
            <button className="primary-button" onClick={() => setFormOpen(true)}><Plus size={18} /> Tambah</button>
          </div>
        </div>
        <div className="data-table-wrap">
          <table className="data-table">
            <thead><tr>
              {sortableColumns.map((column) => <th key={column.id}><button className="sort-button" onClick={() => toggleSort(column.id)}>{column.label}{data.sort === column.id ? (data.order === "asc" ? <ArrowUp size={13} /> : <ArrowDown size={13} />) : null}</button></th>)}
              <th>Paket</th><th>PPPoE</th><th>Layanan</th><th>Tagihan</th><th aria-label="Aksi" />
            </tr></thead>
            <tbody>{data.items.map((customer) => <tr key={customer.id}>
              <td>{customer.customer_number}</td>
              <td><div>{customer.name}</div><div className="text-xs text-[#607067]">{customer.phone || customer.email || "-"}</div></td>
              <td>{customer.package_name ? <div><div>{customer.package_name}</div>{customer.package_price && <div className="text-xs text-[#607067]">{formatIDR(customer.package_price)}/bln</div>}</div> : <span className="text-xs text-[#607067]">-</span>}</td>
              <td>{customer.pppoe_username || <span className="text-xs text-[#607067]">-</span>}</td>
              <td>{serviceBadge(customer)}</td>
              <td>{billingBadge(customer)}</td>
              <td className="w-36"><div className="flex gap-1">
                {customer.pppoe_username && <button className="icon-button" title="Cek koneksi" aria-label={`Cek koneksi ${customer.name}`} onClick={() => setConnectionTarget(customer)}><Wifi size={17} /></button>}
                {customer.service_status === "active" && <button className="icon-button" title="Isolir layanan" aria-label={`Isolir ${customer.name}`} onClick={() => serviceAction(customer, "isolate", `Isolir ${customer.name}? Internet pelanggan akan diputus.`)}><ShieldOff size={17} /></button>}
                {customer.service_status === "isolated" && <button className="icon-button" title="Pulihkan layanan" aria-label={`Pulihkan ${customer.name}`} onClick={() => serviceAction(customer, "restore", `Pulihkan layanan ${customer.name}?`)}><Play size={17} /></button>}
                <button className="icon-button" title="Arsipkan pelanggan" aria-label={`Arsipkan ${customer.name}`} disabled={Boolean(customer.archived_at)} onClick={() => archiveCustomer(customer)}><Archive size={17} /></button>
              </div></td>
            </tr>)}</tbody>
          </table>
        </div>
        <div className="mobile-records">{data.items.map((customer) => <article className="mobile-record" key={customer.id}>
          <div className="mobile-record-head"><div><strong>{customer.name}</strong><div className="mt-1 text-xs text-[#607067]">{customer.customer_number}{customer.package_name ? ` · ${customer.package_name}` : ""}</div></div>{serviceBadge(customer)}</div>
          <div className="flex items-center justify-between gap-3 text-sm">
            <span className="truncate text-[#607067]">{Number(customer.outstanding) > 0 ? `Sisa ${formatIDR(customer.outstanding)}` : customer.last_invoice_number ? "Lunas" : "Belum ada tagihan"}</span>
            <div className="flex gap-1">
              {customer.pppoe_username && <button className="icon-button" aria-label={`Cek koneksi ${customer.name}`} onClick={() => setConnectionTarget(customer)}><Wifi size={17} /></button>}
              {customer.service_status === "active" && <button className="icon-button" aria-label={`Isolir ${customer.name}`} onClick={() => serviceAction(customer, "isolate", `Isolir ${customer.name}?`)}><ShieldOff size={17} /></button>}
              {customer.service_status === "isolated" && <button className="icon-button" aria-label={`Pulihkan ${customer.name}`} onClick={() => serviceAction(customer, "restore", `Pulihkan ${customer.name}?`)}><Play size={17} /></button>}
            </div>
          </div>
        </article>)}</div>
        {data.items.length === 0 && <div className="p-10 text-center text-sm text-[#607067]">Tidak ada pelanggan yang cocok. Tambahkan manual atau jalankan Sync PPPoE.</div>}
        <footer className="table-footer"><span>Menampilkan {data.items.length} dari {data.filtered_total.toLocaleString("id-ID")} pelanggan</span><div className="pagination"><select className="secondary-button" value={data.page_size} onChange={(event) => updateQuery({ page_size: event.target.value, page: "1" })} aria-label="Baris per halaman">{[10,25,50,100].map((size) => <option key={size} value={size}>{size}</option>)}</select><button className="icon-button" disabled={data.page <= 1} onClick={() => updateQuery({ page: String(data.page - 1) })} aria-label="Halaman sebelumnya"><ChevronLeft size={18} /></button><span>{data.page} / {pageCount}</span><button className="icon-button" disabled={data.page >= pageCount} onClick={() => updateQuery({ page: String(data.page + 1) })} aria-label="Halaman berikutnya"><ChevronRight size={18} /></button></div></footer>
      </section>
      {formOpen && <CustomerForm plans={plans} onClose={() => setFormOpen(false)} onSaved={() => { setFormOpen(false); router.refresh(); }} />}
      {syncOpen && <SyncModal onClose={() => setSyncOpen(false)} onDone={() => { setSyncOpen(false); router.refresh(); }} />}
      {connectionTarget && <ConnectionModal customer={connectionTarget} onClose={() => setConnectionTarget(null)} />}
    </>
  );
}

function CustomerForm({ plans, onClose, onSaved }: { plans: Plan[]; onClose: () => void; onSaved: () => void }) {
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setPending(true); setError("");
    const form = new FormData(event.currentTarget);
    try {
      await clientAPI("/api/v1/customers", { method: "POST", body: JSON.stringify({ name: form.get("name"), phone: form.get("phone"), email: form.get("email"), address: form.get("address"), package_id: form.get("package_id") || "" }) });
      onSaved();
    } catch (cause) { setError(cause instanceof Error ? cause.message : "Pelanggan belum dapat disimpan."); }
    finally { setPending(false); }
  }
  return <div className="modal-backdrop" role="dialog" aria-modal="true" aria-labelledby="customer-form-title"><form className="modal" onSubmit={submit}>
    <header className="modal-header"><h2 id="customer-form-title">Tambah pelanggan</h2><button className="icon-button" type="button" onClick={onClose} aria-label="Tutup"><X size={20} /></button></header>
    <div className="modal-body">
      {error && <div className="error-box" role="alert">{error}</div>}
      <div className="field"><label htmlFor="customer-name">Nama pelanggan</label><input id="customer-name" name="name" required maxLength={200} autoFocus /></div>
      <div className="field"><label htmlFor="customer-package">Paket layanan</label><select id="customer-package" name="package_id"><option value="">Tanpa paket (hanya data pelanggan)</option>{plans.map((plan) => <option key={plan.id} value={plan.id}>{plan.name} — {formatIDR(plan.price)}/bln</option>)}</select></div>
      <div className="grid gap-4 sm:grid-cols-2">
        <div className="field"><label htmlFor="customer-phone">Telepon</label><input id="customer-phone" name="phone" maxLength={50} /></div>
        <div className="field"><label htmlFor="customer-email">Email</label><input id="customer-email" name="email" type="email" maxLength={320} /></div>
      </div>
      <div className="field"><label htmlFor="customer-address">Alamat pemasangan</label><textarea id="customer-address" name="address" maxLength={2000} /></div>
      <p className="text-xs text-[#607067]">Jika paket dipilih, layanan langsung aktif dan ikut diterbitkan tagihannya dari menu Tagihan.</p>
    </div>
    <footer className="modal-footer"><button type="button" className="secondary-button" onClick={onClose}>Batal</button><button className="primary-button" disabled={pending}>{pending ? "Menyimpan..." : "Simpan pelanggan"}</button></footer>
  </form></div>;
}

function SyncModal({ onClose, onDone }: { onClose: () => void; onDone: () => void }) {
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [summary, setSummary] = useState<SyncSummary | null>(null);
  async function run() {
    setPending(true); setError("");
    try {
      const result = await clientAPI<SyncSummary>("/api/v1/customers/sync-pppoe", { method: "POST" });
      setSummary(result);
    } catch (cause) { setError(cause instanceof Error ? cause.message : "Sinkronisasi gagal."); }
    finally { setPending(false); }
  }
  return <div className="modal-backdrop" role="dialog" aria-modal="true" aria-labelledby="sync-modal-title"><div className="modal">
    <header className="modal-header"><h2 id="sync-modal-title">Sync pelanggan dari PPPoE</h2><button className="icon-button" type="button" onClick={onClose} aria-label="Tutup"><X size={20} /></button></header>
    <div className="modal-body">
      {error && <div className="error-box" role="alert">{error}</div>}
      {summary ? (
        <div className="grid gap-2 text-sm">
          <p><strong>{summary.secrets_seen.toLocaleString("id-ID")}</strong> secret dan <strong>{summary.profiles_seen.toLocaleString("id-ID")}</strong> profile dibaca dari router.</p>
          <p>Pelanggan baru: <strong>{summary.customers_created.toLocaleString("id-ID")}</strong> · Akun tertaut baru: <strong>{summary.accounts_linked.toLocaleString("id-ID")}</strong> · Diperbarui: <strong>{summary.accounts_updated.toLocaleString("id-ID")}</strong></p>
          <p>Paket baru: <strong>{summary.packages_created.toLocaleString("id-ID")}</strong> · Harga diperbarui: <strong>{summary.packages_updated.toLocaleString("id-ID")}</strong>{summary.packages_without_price > 0 ? <span className="text-[#8a6a10]"> · Tanpa pola harga (cek manual): <strong>{summary.packages_without_price.toLocaleString("id-ID")}</strong></span> : null}</p>
          {(summary.services_isolated > 0 || summary.services_restored > 0) && <p>Status disesuaikan: {summary.services_isolated.toLocaleString("id-ID")} terisolir, {summary.services_restored.toLocaleString("id-ID")} dipulihkan.</p>}
          {summary.secrets_skipped > 0 && <p className="text-[#8a6a10]">Dilewati (profil tidak dikenal): {summary.secrets_skipped.toLocaleString("id-ID")}</p>}
        </div>
      ) : (
        <div className="grid gap-2 text-sm">
          <p>Sinkronisasi akan membaca seluruh <strong>PPP secret</strong> dan <strong>profile</strong> dari router MikroTik lalu:</p>
          <ul className="list-disc pl-5 text-[#3d4a44]">
            <li>Profile menjadi Paket; harga otomatis dari nama berpola <strong>150K</strong> → Rp150.000.</li>
            <li>Secret menjadi Pelanggan + layanan + akun PPPoE (nama dari comment secret).</li>
            <li>Secret disabled dicatat sebagai layanan terisolir.</li>
          </ul>
          <p className="text-xs text-[#607067]">Aman diulang kapan pun — data yang sudah tertaut hanya diperbarui, tidak diduplikasi.</p>
        </div>
      )}
    </div>
    <footer className="modal-footer">
      {summary ? <button className="primary-button" onClick={onDone}>Selesai</button> : <>
        <button type="button" className="secondary-button" onClick={onClose}>Batal</button>
        <button className="primary-button" disabled={pending} onClick={run}><RefreshCw size={17} /> {pending ? "Menyinkronkan..." : "Mulai sinkronisasi"}</button>
      </>}
    </footer>
  </div></div>;
}

function ConnectionModal({ customer, onClose }: { customer: Customer; onClose: () => void }) {
  const [status, setStatus] = useState<ConnectionStatus | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  useEffect(() => {
    let cancelled = false;
    setLoading(true); setError(""); setStatus(null);
    clientAPI<ConnectionStatus>(`/api/v1/customers/${customer.id}/connection`)
      .then((payload) => { if (!cancelled) setStatus(payload); })
      .catch((cause) => { if (!cancelled) setError(cause instanceof Error ? cause.message : "Status koneksi belum dapat dimuat."); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [customer.id]);
  return <div className="modal-backdrop" role="dialog" aria-modal="true" aria-labelledby="connection-modal-title"><div className="modal">
    <header className="modal-header"><h2 id="connection-modal-title">Koneksi {customer.name}</h2><button className="icon-button" type="button" onClick={onClose} aria-label="Tutup"><X size={20} /></button></header>
    <div className="modal-body">
      {error && <div className="error-box" role="alert">{error}</div>}
      {loading && <p className="text-sm text-[#607067]">Memeriksa router…</p>}
      {status && <div className="grid gap-2 text-sm">
        <p><span className={`status-badge ${status.online ? "" : "danger"}`}>{status.online ? "ONLINE" : "OFFLINE"}</span>{status.secret_disabled && <span className="status-badge warning" style={{ marginLeft: 8 }}>Secret dinonaktifkan</span>}</p>
        <p>Username PPPoE: <strong>{status.username}</strong></p>
        {status.online && <>
          {status.address && <p>IP address: <strong>{status.address}</strong></p>}
          {status.uptime && <p>Durasi online: <strong>{status.uptime}</strong></p>}
          {status.caller_id && <p>Perangkat (caller ID): <strong>{status.caller_id}</strong></p>}
        </>}
        {!status.online && !status.secret_disabled && <p className="text-xs text-[#607067]">Pelanggan tidak sedang terhubung. Periksa perangkat pelanggan (modem/router) atau kabel.</p>}
      </div>}
    </div>
    <footer className="modal-footer"><button className="primary-button" onClick={onClose}>Tutup</button></footer>
  </div></div>;
}
