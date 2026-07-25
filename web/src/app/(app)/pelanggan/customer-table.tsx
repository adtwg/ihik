"use client";

import { startTransition, useEffect, useState, type FormEvent } from "react";
import { flexRender, getCoreRowModel, useReactTable, type ColumnDef, type SortingState } from "@tanstack/react-table";
import { Archive, ArrowDown, ArrowUp, ChevronLeft, ChevronRight, MoreHorizontal, Plus, Search, X } from "lucide-react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { clientAPI } from "@/lib/api/client";
import type { Customer, CustomerPage } from "@/lib/customers/types";

const columns: ColumnDef<Customer>[] = [
  { accessorKey: "customer_number", header: "Nomor" },
  { accessorKey: "name", header: "Nama pelanggan" },
  { accessorKey: "phone", header: "Telepon", cell: ({ getValue }) => getValue<string>() || "-", enableSorting: false },
  { accessorKey: "email", header: "Email", cell: ({ getValue }) => getValue<string>() || "-", enableSorting: false },
  { accessorKey: "created_at", header: "Dibuat", cell: ({ getValue }) => new Date(getValue<string>()).toLocaleDateString("id-ID") },
  { id: "status", header: "Status", enableSorting: false, cell: ({ row }) => <span className={`status-badge ${row.original.archived_at ? "archived" : ""}`}>{row.original.archived_at ? "Archived" : "Aktif"}</span> },
];

export function CustomerTable({ data }: { data: CustomerPage }) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const [search, setSearch] = useState(searchParams.get("search") ?? "");
  const [formOpen, setFormOpen] = useState(false);
  const sorting: SortingState = [{ id: data.sort, desc: data.order === "desc" }];

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

  const table = useReactTable({
    data: data.items,
    columns,
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    manualSorting: true,
    rowCount: data.filtered_total,
    state: { pagination: { pageIndex: data.page - 1, pageSize: data.page_size }, sorting },
    onSortingChange: (updater) => {
      const next = typeof updater === "function" ? updater(sorting) : updater;
      const first = next[0];
      updateQuery({ sort: first?.id ?? "created_at", order: first?.desc ? "desc" : "asc", page: "1" });
    },
  });
  const pageCount = Math.max(1, Math.ceil(data.filtered_total / data.page_size));

  async function archiveCustomer(customer: Customer) {
    if (!window.confirm(`Arsipkan ${customer.name}? Histori keuangan tetap tersimpan.`)) return;
    await clientAPI(`/api/v1/customers/${customer.id}/archive`, { method: "POST" });
    router.refresh();
  }

  return (
    <>
      <section className="table-shell">
        <div className="table-toolbar">
          <div className="relative w-full max-w-[380px]"><Search className="absolute left-3 top-3 text-[#607067]" size={18} /><input className="search-input pl-10" value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Cari nomor, nama, telepon, email" aria-label="Cari pelanggan" /></div>
          <div className="flex gap-2"><label className="sr-only" htmlFor="archive-filter">Status arsip</label><select id="archive-filter" className="secondary-button" value={searchParams.get("archived") ?? "active"} onChange={(event) => updateQuery({ archived: event.target.value === "include" ? "include" : null, page: "1" })}><option value="active">Aktif</option><option value="include">Semua</option></select><button className="primary-button" onClick={() => setFormOpen(true)}><Plus size={18} /> Tambah</button></div>
        </div>
        <div className="data-table-wrap">
          <table className="data-table">
            <thead>{table.getHeaderGroups().map((group) => <tr key={group.id}>{group.headers.map((header) => <th key={header.id}>{header.isPlaceholder ? null : header.column.getCanSort() ? <button className="sort-button" onClick={header.column.getToggleSortingHandler()}>{flexRender(header.column.columnDef.header, header.getContext())}{header.column.getIsSorted() === "asc" ? <ArrowUp size={13} /> : header.column.getIsSorted() === "desc" ? <ArrowDown size={13} /> : null}</button> : flexRender(header.column.columnDef.header, header.getContext())}</th>)}<th aria-label="Aksi" /></tr>)}</thead>
            <tbody>{table.getRowModel().rows.map((row) => <tr key={row.id}>{row.getVisibleCells().map((cell) => <td key={cell.id}>{flexRender(cell.column.columnDef.cell, cell.getContext())}</td>)}<td className="w-14"><button className="icon-button" title="Arsipkan pelanggan" aria-label={`Arsipkan ${row.original.name}`} disabled={Boolean(row.original.archived_at)} onClick={() => archiveCustomer(row.original)}><Archive size={17} /></button></td></tr>)}</tbody>
          </table>
        </div>
        <div className="mobile-records">{data.items.map((customer) => <article className="mobile-record" key={customer.id}><div className="mobile-record-head"><div><strong>{customer.name}</strong><div className="mt-1 text-xs text-[#607067]">{customer.customer_number}</div></div><button className="icon-button" aria-label={`Aksi ${customer.name}`} onClick={() => archiveCustomer(customer)} disabled={Boolean(customer.archived_at)}><MoreHorizontal size={19} /></button></div><div className="flex items-center justify-between gap-3 text-sm"><span className="truncate text-[#607067]">{customer.phone || customer.email || "Kontak belum diisi"}</span><span className={`status-badge ${customer.archived_at ? "archived" : ""}`}>{customer.archived_at ? "Archived" : "Aktif"}</span></div></article>)}</div>
        {data.items.length === 0 && <div className="p-10 text-center text-sm text-[#607067]">Tidak ada pelanggan yang cocok.</div>}
        <footer className="table-footer"><span>Menampilkan {data.items.length} dari {data.filtered_total.toLocaleString("id-ID")} pelanggan</span><div className="pagination"><select className="secondary-button" value={data.page_size} onChange={(event) => updateQuery({ page_size: event.target.value, page: "1" })} aria-label="Baris per halaman">{[10,25,50,100].map((size) => <option key={size} value={size}>{size}</option>)}</select><button className="icon-button" disabled={data.page <= 1} onClick={() => updateQuery({ page: String(data.page - 1) })} aria-label="Halaman sebelumnya"><ChevronLeft size={18} /></button><span>{data.page} / {pageCount}</span><button className="icon-button" disabled={data.page >= pageCount} onClick={() => updateQuery({ page: String(data.page + 1) })} aria-label="Halaman berikutnya"><ChevronRight size={18} /></button></div></footer>
      </section>
      {formOpen && <CustomerForm onClose={() => setFormOpen(false)} onSaved={() => { setFormOpen(false); router.refresh(); }} />}
    </>
  );
}

function CustomerForm({ onClose, onSaved }: { onClose: () => void; onSaved: () => void }) {
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setPending(true); setError("");
    const form = new FormData(event.currentTarget);
    try {
      await clientAPI("/api/v1/customers", { method: "POST", body: JSON.stringify({ name: form.get("name"), phone: form.get("phone"), email: form.get("email"), address: form.get("address") }) });
      onSaved();
    } catch (cause) { setError(cause instanceof Error ? cause.message : "Pelanggan belum dapat disimpan."); }
    finally { setPending(false); }
  }
  return <div className="modal-backdrop" role="dialog" aria-modal="true" aria-labelledby="customer-form-title"><form className="modal" onSubmit={submit}><header className="modal-header"><h2 id="customer-form-title">Tambah pelanggan</h2><button className="icon-button" type="button" onClick={onClose} aria-label="Tutup"><X size={20} /></button></header><div className="modal-body">{error && <div className="error-box" role="alert">{error}</div>}<div className="field"><label htmlFor="customer-name">Nama pelanggan</label><input id="customer-name" name="name" required maxLength={200} autoFocus /></div><div className="grid gap-4 sm:grid-cols-2"><div className="field"><label htmlFor="customer-phone">Telepon</label><input id="customer-phone" name="phone" maxLength={50} /></div><div className="field"><label htmlFor="customer-email">Email</label><input id="customer-email" name="email" type="email" maxLength={320} /></div></div><div className="field"><label htmlFor="customer-address">Alamat pemasangan</label><textarea id="customer-address" name="address" maxLength={2000} /></div></div><footer className="modal-footer"><button type="button" className="secondary-button" onClick={onClose}>Batal</button><button className="primary-button" disabled={pending}>{pending ? "Menyimpan..." : "Simpan pelanggan"}</button></footer></form></div>;
}