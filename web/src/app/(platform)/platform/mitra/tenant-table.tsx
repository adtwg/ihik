"use client";

import { startTransition, useEffect, useState, type FormEvent } from "react";
import { ArrowDown, ArrowUp, Boxes, ChevronLeft, ChevronRight, Plus, Power, Search, X } from "lucide-react";
import { impersonateTenant } from "./actions";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { clientAPI } from "@/lib/api/client";
import { formatDate } from "@/lib/format";
import type { Tenant, TenantPage } from "@/lib/tenants/types";

const sortableColumns = [
  { id: "code", label: "Kode" },
  { id: "name", label: "Nama mitra" },
  { id: "created_at", label: "Dibuat" },
] as const;

export function TenantTable({ data }: { data: TenantPage }) {
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

  async function toggleActive(tenant: Tenant) {
    const nextActive = !tenant.active;
    if (!window.confirm(`${nextActive ? "Aktifkan" : "Nonaktifkan"} mitra ${tenant.name}?`)) return;
    await clientAPI(`/api/v1/platform/tenants/${tenant.id}/active`, { method: "POST", body: JSON.stringify({ active: nextActive }) });
    router.refresh();
  }

  const pageCount = Math.max(1, Math.ceil(data.filtered_total / data.page_size));

  return (
    <>
      <section className="table-shell">
        <div className="table-toolbar">
          <div className="relative w-full max-w-[380px]"><Search className="absolute left-3 top-3 text-[#607067]" size={18} /><input className="search-input pl-10" value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Cari kode atau nama mitra" aria-label="Cari mitra" /></div>
          <button className="primary-button" onClick={() => setFormOpen(true)}><Plus size={18} /> Tambah mitra</button>
        </div>
        <div className="data-table-wrap">
          <table className="data-table">
            <thead><tr>
              {sortableColumns.map((column) => <th key={column.id}><button className="sort-button" onClick={() => toggleSort(column.id)}>{column.label}{data.sort === column.id ? (data.order === "asc" ? <ArrowUp size={13} /> : <ArrowDown size={13} />) : null}</button></th>)}
              <th>Pengguna</th><th>Pelanggan</th><th>Status</th><th aria-label="Aksi" />
            </tr></thead>
            <tbody>{data.items.map((tenant) => <tr key={tenant.id}>
              <td>{tenant.code}</td>
              <td>{tenant.name}</td>
              <td>{formatDate(tenant.created_at)}</td>
              <td>{tenant.user_count.toLocaleString("id-ID")}</td>
              <td>{tenant.customer_count.toLocaleString("id-ID")}</td>
              <td><span className={`status-badge ${tenant.active ? "" : "danger"}`}>{tenant.active ? "Aktif" : "Nonaktif"}</span></td>
              <td className="w-24"><div className="flex items-center gap-1">
                <form action={impersonateTenant}><input type="hidden" name="tenant_id" value={tenant.id} /><button className="icon-button" title={tenant.active ? "Kelola mitra ini" : "Mitra nonaktif"} aria-label={`Kelola ${tenant.name}`} disabled={!tenant.active}><Boxes size={17} /></button></form>
                <button className="icon-button" title={tenant.active ? "Nonaktifkan mitra" : "Aktifkan mitra"} aria-label={`${tenant.active ? "Nonaktifkan" : "Aktifkan"} ${tenant.name}`} onClick={() => toggleActive(tenant)}><Power size={17} /></button>
              </div></td>
            </tr>)}</tbody>
          </table>
        </div>
        <div className="mobile-records">{data.items.map((tenant) => <article className="mobile-record" key={tenant.id}>
          <div className="mobile-record-head"><div><strong>{tenant.name}</strong><div className="mt-1 text-xs text-[#607067]">{tenant.code}</div></div><span className={`status-badge ${tenant.active ? "" : "danger"}`}>{tenant.active ? "Aktif" : "Nonaktif"}</span></div>
          <div className="flex items-center justify-between gap-3 text-sm"><span className="text-[#607067]">{tenant.user_count} pengguna · {tenant.customer_count} pelanggan</span><button className="icon-button" aria-label={`${tenant.active ? "Nonaktifkan" : "Aktifkan"} ${tenant.name}`} onClick={() => toggleActive(tenant)}><Power size={17} /></button></div>
        </article>)}</div>
        {data.items.length === 0 && <div className="p-10 text-center text-sm text-[#607067]">Belum ada mitra terdaftar.</div>}
        <footer className="table-footer"><span>Menampilkan {data.items.length} dari {data.filtered_total.toLocaleString("id-ID")} mitra</span><div className="pagination"><select className="secondary-button" value={data.page_size} onChange={(event) => updateQuery({ page_size: event.target.value, page: "1" })} aria-label="Baris per halaman">{[10,25,50,100].map((size) => <option key={size} value={size}>{size}</option>)}</select><button className="icon-button" disabled={data.page <= 1} onClick={() => updateQuery({ page: String(data.page - 1) })} aria-label="Halaman sebelumnya"><ChevronLeft size={18} /></button><span>{data.page} / {pageCount}</span><button className="icon-button" disabled={data.page >= pageCount} onClick={() => updateQuery({ page: String(data.page + 1) })} aria-label="Halaman berikutnya"><ChevronRight size={18} /></button></div></footer>
      </section>
      {formOpen && <TenantForm onClose={() => setFormOpen(false)} onSaved={() => { setFormOpen(false); router.refresh(); }} />}
    </>
  );
}

function TenantForm({ onClose, onSaved }: { onClose: () => void; onSaved: () => void }) {
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setPending(true); setError("");
    const form = new FormData(event.currentTarget);
    try {
      await clientAPI("/api/v1/platform/tenants", { method: "POST", body: JSON.stringify({ code: form.get("code"), name: form.get("name"), admin_username: form.get("admin_username"), admin_password: form.get("admin_password") }) });
      onSaved();
    } catch (cause) { setError(cause instanceof Error ? cause.message : "Mitra belum dapat disimpan."); }
    finally { setPending(false); }
  }
  return <div className="modal-backdrop" role="dialog" aria-modal="true" aria-labelledby="tenant-form-title"><form className="modal" onSubmit={submit}>
    <header className="modal-header"><h2 id="tenant-form-title">Tambah mitra</h2><button className="icon-button" type="button" onClick={onClose} aria-label="Tutup"><X size={20} /></button></header>
    <div className="modal-body">
      {error && <div className="error-box" role="alert">{error}</div>}
      <div className="grid gap-4 sm:grid-cols-2">
        <div className="field"><label htmlFor="tenant-code">Kode mitra</label><input id="tenant-code" name="code" required maxLength={50} pattern="[a-z0-9][a-z0-9-]{1,49}" placeholder="mis. net-maju" autoFocus /></div>
        <div className="field"><label htmlFor="tenant-name">Nama mitra</label><input id="tenant-name" name="name" required maxLength={200} placeholder="mis. Net Maju Bersama" /></div>
      </div>
      <div className="grid gap-4 sm:grid-cols-2">
        <div className="field"><label htmlFor="tenant-admin">Username admin</label><input id="tenant-admin" name="admin_username" required maxLength={64} pattern="[a-z0-9][a-z0-9._-]{2,63}" placeholder="mis. admin.netmaju" /></div>
        <div className="field"><label htmlFor="tenant-password">Password admin</label><input id="tenant-password" name="admin_password" type="password" required minLength={12} maxLength={128} /></div>
      </div>
      <p className="text-xs text-[#607067]">Password minimal 12 karakter. Akun ini menjadi login admin mitra pada aplikasi.</p>
    </div>
    <footer className="modal-footer"><button type="button" className="secondary-button" onClick={onClose}>Batal</button><button className="primary-button" disabled={pending}>{pending ? "Menyimpan..." : "Simpan mitra"}</button></footer>
  </form></div>;
}
