"use client";

import { startTransition, useEffect, useState, type FormEvent } from "react";
import { Archive, ArrowDown, ArrowUp, ChevronLeft, ChevronRight, Pencil, Plus, Search, X } from "lucide-react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { clientAPI } from "@/lib/api/client";
import { formatIDR } from "@/lib/format";
import type { Plan, PlanPage } from "@/lib/plans/types";

const sortableColumns = [
  { id: "code", label: "Kode" },
  { id: "name", label: "Nama paket" },
] as const;

export function PlanTable({ data }: { data: PlanPage }) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const [search, setSearch] = useState(searchParams.get("search") ?? "");
  const [editing, setEditing] = useState<Plan | null>(null);
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

  async function archivePlan(plan: Plan) {
    if (!window.confirm(`Arsipkan paket ${plan.name}? Layanan berjalan tidak terpengaruh.`)) return;
    await clientAPI(`/api/v1/plans/${plan.id}/archive`, { method: "POST" });
    router.refresh();
  }

  const pageCount = Math.max(1, Math.ceil(data.filtered_total / data.page_size));

  return (
    <>
      <section className="table-shell">
        <div className="table-toolbar">
          <div className="relative w-full max-w-[380px]"><Search className="absolute left-3 top-3 text-[#607067]" size={18} /><input className="search-input pl-10" value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Cari kode atau nama paket" aria-label="Cari paket" /></div>
          <div className="flex gap-2">
            <select className="secondary-button" value={searchParams.get("archived") ?? "active"} onChange={(event) => updateQuery({ archived: event.target.value === "include" ? "include" : null, page: "1" })} aria-label="Status arsip"><option value="active">Aktif</option><option value="include">Semua</option></select>
            <button className="primary-button" onClick={() => setFormOpen(true)}><Plus size={18} /> Tambah</button>
          </div>
        </div>
        <div className="data-table-wrap">
          <table className="data-table">
            <thead><tr>
              {sortableColumns.map((column) => <th key={column.id}><button className="sort-button" onClick={() => toggleSort(column.id)}>{column.label}{data.sort === column.id ? (data.order === "asc" ? <ArrowUp size={13} /> : <ArrowDown size={13} />) : null}</button></th>)}
              <th>Harga / bulan</th><th>Pajak</th><th>Layanan</th><th>Status</th><th aria-label="Aksi" />
            </tr></thead>
            <tbody>{data.items.map((plan) => <tr key={plan.id}>
              <td>{plan.code}</td>
              <td>{plan.name}</td>
              <td>{formatIDR(plan.price)}</td>
              <td>{Number(plan.tax_percent).toLocaleString("id-ID")}%</td>
              <td>{plan.service_count.toLocaleString("id-ID")}</td>
              <td><span className={`status-badge ${plan.archived_at ? "archived" : plan.active ? "" : "warning"}`}>{plan.archived_at ? "Arsip" : plan.active ? "Aktif" : "Nonaktif"}</span></td>
              <td className="w-24"><div className="flex gap-1">
                <button className="icon-button" title="Ubah paket" aria-label={`Ubah ${plan.name}`} disabled={Boolean(plan.archived_at)} onClick={() => setEditing(plan)}><Pencil size={17} /></button>
                <button className="icon-button" title="Arsipkan paket" aria-label={`Arsipkan ${plan.name}`} disabled={Boolean(plan.archived_at)} onClick={() => archivePlan(plan)}><Archive size={17} /></button>
              </div></td>
            </tr>)}</tbody>
          </table>
        </div>
        <div className="mobile-records">{data.items.map((plan) => <article className="mobile-record" key={plan.id}>
          <div className="mobile-record-head"><div><strong>{plan.name}</strong><div className="mt-1 text-xs text-[#607067]">{plan.code}</div></div><button className="icon-button" aria-label={`Ubah ${plan.name}`} disabled={Boolean(plan.archived_at)} onClick={() => setEditing(plan)}><Pencil size={17} /></button></div>
          <div className="flex items-center justify-between gap-3 text-sm"><span className="text-[#607067]">{formatIDR(plan.price)} / bln</span><span className={`status-badge ${plan.archived_at ? "archived" : plan.active ? "" : "warning"}`}>{plan.archived_at ? "Arsip" : plan.active ? "Aktif" : "Nonaktif"}</span></div>
        </article>)}</div>
        {data.items.length === 0 && <div className="p-10 text-center text-sm text-[#607067]">Belum ada paket. Tambahkan paket pertama Anda.</div>}
        <footer className="table-footer"><span>Menampilkan {data.items.length} dari {data.filtered_total.toLocaleString("id-ID")} paket</span><div className="pagination"><select className="secondary-button" value={data.page_size} onChange={(event) => updateQuery({ page_size: event.target.value, page: "1" })} aria-label="Baris per halaman">{[10,25,50,100].map((size) => <option key={size} value={size}>{size}</option>)}</select><button className="icon-button" disabled={data.page <= 1} onClick={() => updateQuery({ page: String(data.page - 1) })} aria-label="Halaman sebelumnya"><ChevronLeft size={18} /></button><span>{data.page} / {pageCount}</span><button className="icon-button" disabled={data.page >= pageCount} onClick={() => updateQuery({ page: String(data.page + 1) })} aria-label="Halaman berikutnya"><ChevronRight size={18} /></button></div></footer>
      </section>
      {(formOpen || editing) && <PlanForm plan={editing} onClose={() => { setFormOpen(false); setEditing(null); }} onSaved={() => { setFormOpen(false); setEditing(null); router.refresh(); }} />}
    </>
  );
}

function PlanForm({ plan, onClose, onSaved }: { plan: Plan | null; onClose: () => void; onSaved: () => void }) {
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setPending(true); setError("");
    const form = new FormData(event.currentTarget);
    const price = Number(form.get("price"));
    const taxPercent = Number(form.get("tax_percent") || 0);
    try {
      if (plan) {
        await clientAPI(`/api/v1/plans/${plan.id}`, { method: "PUT", body: JSON.stringify({ name: form.get("name"), price, tax_percent: taxPercent, active: form.get("active") === "on" }) });
      } else {
        await clientAPI("/api/v1/plans", { method: "POST", body: JSON.stringify({ code: form.get("code"), name: form.get("name"), price, tax_percent: taxPercent }) });
      }
      onSaved();
    } catch (cause) { setError(cause instanceof Error ? cause.message : "Paket belum dapat disimpan."); }
    finally { setPending(false); }
  }
  return <div className="modal-backdrop" role="dialog" aria-modal="true" aria-labelledby="plan-form-title"><form className="modal" onSubmit={submit}>
    <header className="modal-header"><h2 id="plan-form-title">{plan ? "Ubah paket" : "Tambah paket"}</h2><button className="icon-button" type="button" onClick={onClose} aria-label="Tutup"><X size={20} /></button></header>
    <div className="modal-body">
      {error && <div className="error-box" role="alert">{error}</div>}
      {!plan && <div className="field"><label htmlFor="plan-code">Kode paket</label><input id="plan-code" name="code" required maxLength={50} pattern="[A-Za-z0-9][A-Za-z0-9_-]{1,49}" placeholder="mis. HOME-10M" autoFocus /></div>}
      <div className="field"><label htmlFor="plan-name">Nama paket</label><input id="plan-name" name="name" required maxLength={200} defaultValue={plan?.name ?? ""} placeholder="mis. Home 10 Mbps" /></div>
      <div className="grid gap-4 sm:grid-cols-2">
        <div className="field"><label htmlFor="plan-price">Harga per bulan (Rp)</label><input id="plan-price" name="price" type="number" min={0} step="0.01" required defaultValue={plan ? Number(plan.price) : ""} /></div>
        <div className="field"><label htmlFor="plan-tax">Pajak (%)</label><input id="plan-tax" name="tax_percent" type="number" min={0} max={100} step="0.01" defaultValue={plan ? Number(plan.tax_percent) : 0} /></div>
      </div>
      {plan && <div className="field"><label className="flex items-center gap-2"><input type="checkbox" name="active" defaultChecked={plan.active} /> Paket aktif (dapat dipakai layanan baru)</label></div>}
      {plan && <p className="text-xs text-[#607067]">Perubahan harga berlaku mulai hari ini; tagihan yang sudah terbit tidak berubah.</p>}
    </div>
    <footer className="modal-footer"><button type="button" className="secondary-button" onClick={onClose}>Batal</button><button className="primary-button" disabled={pending}>{pending ? "Menyimpan..." : "Simpan paket"}</button></footer>
  </form></div>;
}
