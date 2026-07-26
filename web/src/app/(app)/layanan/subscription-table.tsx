"use client";

import { startTransition, useEffect, useState, type FormEvent } from "react";
import { Archive, ArrowDown, ArrowUp, ChevronLeft, ChevronRight, Play, Plus, Search, ShieldOff, X } from "lucide-react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { clientAPI } from "@/lib/api/client";
import { formatIDR, formatDate } from "@/lib/format";
import type { Subscription, SubscriptionPage } from "@/lib/subscriptions/types";
import type { Customer } from "@/lib/customers/types";
import type { Plan } from "@/lib/plans/types";

const statusLabels: Record<string, { label: string; className: string }> = {
  active: { label: "Aktif", className: "" },
  isolated: { label: "Terisolir", className: "danger" },
  pending_provisioning: { label: "Menunggu", className: "warning" },
  provisioning_failed: { label: "Gagal", className: "danger" },
  archived: { label: "Arsip", className: "archived" },
};

const sortableColumns = [
  { id: "service_number", label: "Nomor layanan" },
  { id: "customer_name", label: "Pelanggan" },
] as const;

export function SubscriptionTable({ data, customers, plans }: { data: SubscriptionPage; customers: Customer[]; plans: Plan[] }) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const [search, setSearch] = useState(searchParams.get("search") ?? "");
  const [formOpen, setFormOpen] = useState(false);

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

  async function transition(subscription: Subscription, action: "isolate" | "restore" | "archive", confirmText: string) {
    if (!window.confirm(confirmText)) return;
    await clientAPI(`/api/v1/services/${subscription.id}/${action}`, { method: "POST" });
    router.refresh();
  }

  const pageCount = Math.max(1, Math.ceil(data.filtered_total / data.page_size));

  function statusBadge(status: string) {
    const meta = statusLabels[status] ?? { label: status, className: "info" };
    return <span className={`status-badge ${meta.className}`}>{meta.label}</span>;
  }

  return (
    <>
      <section className="table-shell">
        <div className="table-toolbar">
          <div className="relative w-full max-w-[380px]"><Search className="absolute left-3 top-3 text-[#607067]" size={18} /><input className="search-input pl-10" value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Cari nomor, pelanggan, paket" aria-label="Cari layanan" /></div>
          <div className="flex gap-2">
            <select className="secondary-button" value={searchParams.get("status") ?? ""} onChange={(event) => updateQuery({ status: event.target.value || null, page: "1" })} aria-label="Filter status"><option value="">Semua status</option><option value="active">Aktif</option><option value="isolated">Terisolir</option><option value="pending_provisioning">Menunggu</option></select>
            <button className="primary-button" onClick={() => setFormOpen(true)}><Plus size={18} /> Tambah</button>
          </div>
        </div>
        <div className="data-table-wrap">
          <table className="data-table">
            <thead><tr>
              {sortableColumns.map((column) => <th key={column.id}><button className="sort-button" onClick={() => toggleSort(column.id)}>{column.label}{data.sort === column.id ? (data.order === "asc" ? <ArrowUp size={13} /> : <ArrowDown size={13} />) : null}</button></th>)}
              <th>Paket</th><th>Harga</th><th>Aktif sejak</th><th>Status</th><th aria-label="Aksi" />
            </tr></thead>
            <tbody>{data.items.map((subscription) => <tr key={subscription.id}>
              <td>{subscription.service_number}</td>
              <td><div>{subscription.customer_name}</div><div className="text-xs text-[#607067]">{subscription.customer_number}</div></td>
              <td>{subscription.package_name}</td>
              <td>{formatIDR(subscription.price)}</td>
              <td>{formatDate(subscription.activated_at)}</td>
              <td>{statusBadge(subscription.status)}</td>
              <td className="w-28"><div className="flex gap-1">
                {subscription.status === "active" && <button className="icon-button" title="Isolir layanan" aria-label={`Isolir ${subscription.service_number}`} onClick={() => transition(subscription, "isolate", `Isolir ${subscription.service_number}? Pelanggan tidak dapat memakai internet.`)}><ShieldOff size={17} /></button>}
                {subscription.status === "isolated" && <button className="icon-button" title="Pulihkan layanan" aria-label={`Pulihkan ${subscription.service_number}`} onClick={() => transition(subscription, "restore", `Pulihkan ${subscription.service_number}?`)}><Play size={17} /></button>}
                <button className="icon-button" title="Arsipkan layanan" aria-label={`Arsipkan ${subscription.service_number}`} disabled={Boolean(subscription.archived_at)} onClick={() => transition(subscription, "archive", `Arsipkan ${subscription.service_number}? Tagihan baru tidak akan dibuat lagi.`)}><Archive size={17} /></button>
              </div></td>
            </tr>)}</tbody>
          </table>
        </div>
        <div className="mobile-records">{data.items.map((subscription) => <article className="mobile-record" key={subscription.id}>
          <div className="mobile-record-head"><div><strong>{subscription.customer_name}</strong><div className="mt-1 text-xs text-[#607067]">{subscription.service_number} · {subscription.package_name}</div></div>{statusBadge(subscription.status)}</div>
          <div className="flex items-center justify-between gap-3 text-sm"><span className="text-[#607067]">{formatIDR(subscription.price)} / bln</span><div className="flex gap-1">
            {subscription.status === "active" && <button className="icon-button" aria-label={`Isolir ${subscription.service_number}`} onClick={() => transition(subscription, "isolate", `Isolir ${subscription.service_number}?`)}><ShieldOff size={17} /></button>}
            {subscription.status === "isolated" && <button className="icon-button" aria-label={`Pulihkan ${subscription.service_number}`} onClick={() => transition(subscription, "restore", `Pulihkan ${subscription.service_number}?`)}><Play size={17} /></button>}
          </div></div>
        </article>)}</div>
        {data.items.length === 0 && <div className="p-10 text-center text-sm text-[#607067]">Belum ada layanan yang cocok.</div>}
        <footer className="table-footer"><span>Menampilkan {data.items.length} dari {data.filtered_total.toLocaleString("id-ID")} layanan</span><div className="pagination"><select className="secondary-button" value={data.page_size} onChange={(event) => updateQuery({ page_size: event.target.value, page: "1" })} aria-label="Baris per halaman">{[10,25,50,100].map((size) => <option key={size} value={size}>{size}</option>)}</select><button className="icon-button" disabled={data.page <= 1} onClick={() => updateQuery({ page: String(data.page - 1) })} aria-label="Halaman sebelumnya"><ChevronLeft size={18} /></button><span>{data.page} / {pageCount}</span><button className="icon-button" disabled={data.page >= pageCount} onClick={() => updateQuery({ page: String(data.page + 1) })} aria-label="Halaman berikutnya"><ChevronRight size={18} /></button></div></footer>
      </section>
      {formOpen && <SubscriptionForm customers={customers} plans={plans} onClose={() => setFormOpen(false)} onSaved={() => { setFormOpen(false); router.refresh(); }} />}
    </>
  );
}

function SubscriptionForm({ customers, plans, onClose, onSaved }: { customers: Customer[]; plans: Plan[]; onClose: () => void; onSaved: () => void }) {
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setPending(true); setError("");
    const form = new FormData(event.currentTarget);
    try {
      await clientAPI("/api/v1/services", { method: "POST", body: JSON.stringify({ customer_id: form.get("customer_id"), package_id: form.get("package_id") }) });
      onSaved();
    } catch (cause) { setError(cause instanceof Error ? cause.message : "Layanan belum dapat disimpan."); }
    finally { setPending(false); }
  }
  return <div className="modal-backdrop" role="dialog" aria-modal="true" aria-labelledby="subscription-form-title"><form className="modal" onSubmit={submit}>
    <header className="modal-header"><h2 id="subscription-form-title">Tambah layanan</h2><button className="icon-button" type="button" onClick={onClose} aria-label="Tutup"><X size={20} /></button></header>
    <div className="modal-body">
      {error && <div className="error-box" role="alert">{error}</div>}
      <div className="field"><label htmlFor="subscription-customer">Pelanggan</label><select id="subscription-customer" name="customer_id" required autoFocus><option value="">Pilih pelanggan…</option>{customers.map((customer) => <option key={customer.id} value={customer.id}>{customer.customer_number} — {customer.name}</option>)}</select></div>
      <div className="field"><label htmlFor="subscription-plan">Paket</label><select id="subscription-plan" name="package_id" required><option value="">Pilih paket…</option>{plans.map((plan) => <option key={plan.id} value={plan.id}>{plan.name} — {formatIDR(plan.price)}/bln</option>)}</select></div>
      <p className="text-xs text-[#607067]">Layanan langsung berstatus aktif. Tagihan dibuat dari menu Tagihan per periode.</p>
    </div>
    <footer className="modal-footer"><button type="button" className="secondary-button" onClick={onClose}>Batal</button><button className="primary-button" disabled={pending}>{pending ? "Menyimpan..." : "Simpan layanan"}</button></footer>
  </form></div>;
}
