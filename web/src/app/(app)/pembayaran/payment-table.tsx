"use client";

import { startTransition, useEffect, useState, type FormEvent } from "react";
import { ArrowDown, ArrowUp, Ban, ChevronLeft, ChevronRight, Printer, Search, X } from "lucide-react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { clientAPI } from "@/lib/api/client";
import { formatIDR, formatDateTime } from "@/lib/format";
import type { Payment, PaymentPage } from "@/lib/payments/types";

const sortableColumns = [
  { id: "payment_number", label: "Nomor" },
  { id: "amount", label: "Jumlah" },
  { id: "received_at", label: "Diterima" },
] as const;

export function PaymentTable({ data }: { data: PaymentPage }) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const [search, setSearch] = useState(searchParams.get("search") ?? "");
  const [voidTarget, setVoidTarget] = useState<Payment | null>(null);

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

  const pageCount = Math.max(1, Math.ceil(data.filtered_total / data.page_size));

  return (
    <>
      <section className="table-shell">
        <div className="table-toolbar">
          <div className="relative w-full max-w-[380px]"><Search className="absolute left-3 top-3 text-[#607067]" size={18} /><input className="search-input pl-10" value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Cari nomor, kuitansi, pelanggan" aria-label="Cari pembayaran" /></div>
          <div className="flex gap-2">
            <select className="secondary-button" value={searchParams.get("method") ?? ""} onChange={(event) => updateQuery({ method: event.target.value || null, page: "1" })} aria-label="Filter metode"><option value="">Semua metode</option><option value="cash">Tunai</option><option value="manual_transfer">Transfer manual</option></select>
            <select className="secondary-button" value={searchParams.get("status") ?? ""} onChange={(event) => updateQuery({ status: event.target.value || null, page: "1" })} aria-label="Filter status"><option value="">Semua status</option><option value="posted">Tercatat</option><option value="voided">Dibatalkan</option></select>
          </div>
        </div>
        <div className="data-table-wrap">
          <table className="data-table">
            <thead><tr>
              {sortableColumns.map((column) => <th key={column.id}><button className="sort-button" onClick={() => toggleSort(column.id)}>{column.label}{data.sort === column.id ? (data.order === "asc" ? <ArrowUp size={13} /> : <ArrowDown size={13} />) : null}</button></th>)}
              <th>Kuitansi</th><th>Tagihan</th><th>Pelanggan</th><th>Metode</th><th>Status</th><th aria-label="Aksi" />
            </tr></thead>
            <tbody>{data.items.map((payment) => <tr key={payment.id}>
              <td>{payment.payment_number}</td>
              <td>{formatIDR(payment.amount)}</td>
              <td>{formatDateTime(payment.received_at)}</td>
              <td>{payment.receipt_number}</td>
              <td>{payment.invoice_number}</td>
              <td><div>{payment.customer_name}</div><div className="text-xs text-[#607067]">{payment.customer_number}</div></td>
              <td>{payment.method === "cash" ? "Tunai" : "Transfer"}</td>
              <td><span className={`status-badge ${payment.status === "voided" ? "archived" : ""}`}>{payment.status === "voided" ? "Dibatalkan" : "Tercatat"}</span></td>
              <td className="w-24"><div className="flex gap-1">
                <button className="icon-button" title="Cetak kuitansi" aria-label={`Cetak ${payment.receipt_number}`} onClick={() => window.open(`/pembayaran/${payment.id}/cetak`, "_blank", "noopener")}><Printer size={17} /></button>
                {payment.status === "posted" && <button className="icon-button" title="Batalkan pembayaran" aria-label={`Batalkan ${payment.payment_number}`} onClick={() => setVoidTarget(payment)}><Ban size={17} /></button>}
              </div></td>
            </tr>)}</tbody>
          </table>
        </div>
        <div className="mobile-records">{data.items.map((payment) => <article className="mobile-record" key={payment.id}>
          <div className="mobile-record-head"><div><strong>{payment.customer_name}</strong><div className="mt-1 text-xs text-[#607067]">{payment.payment_number} · {payment.invoice_number}</div></div><span className={`status-badge ${payment.status === "voided" ? "archived" : ""}`}>{payment.status === "voided" ? "Batal" : "Tercatat"}</span></div>
          <div className="flex items-center justify-between gap-3 text-sm"><span className="text-[#607067]">{formatIDR(payment.amount)} · {payment.method === "cash" ? "Tunai" : "Transfer"}</span>{payment.status === "posted" && <button className="icon-button" aria-label={`Batalkan ${payment.payment_number}`} onClick={() => setVoidTarget(payment)}><Ban size={17} /></button>}</div>
        </article>)}</div>
        {data.items.length === 0 && <div className="p-10 text-center text-sm text-[#607067]">Belum ada pembayaran yang cocok.</div>}
        <footer className="table-footer"><span>Menampilkan {data.items.length} dari {data.filtered_total.toLocaleString("id-ID")} pembayaran</span><div className="pagination"><select className="secondary-button" value={data.page_size} onChange={(event) => updateQuery({ page_size: event.target.value, page: "1" })} aria-label="Baris per halaman">{[10,25,50,100].map((size) => <option key={size} value={size}>{size}</option>)}</select><button className="icon-button" disabled={data.page <= 1} onClick={() => updateQuery({ page: String(data.page - 1) })} aria-label="Halaman sebelumnya"><ChevronLeft size={18} /></button><span>{data.page} / {pageCount}</span><button className="icon-button" disabled={data.page >= pageCount} onClick={() => updateQuery({ page: String(data.page + 1) })} aria-label="Halaman berikutnya"><ChevronRight size={18} /></button></div></footer>
      </section>
      {voidTarget && <VoidForm payment={voidTarget} onClose={() => setVoidTarget(null)} onSaved={() => { setVoidTarget(null); router.refresh(); }} />}
    </>
  );
}

function VoidForm({ payment, onClose, onSaved }: { payment: Payment; onClose: () => void; onSaved: () => void }) {
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setPending(true); setError("");
    const form = new FormData(event.currentTarget);
    try {
      await clientAPI(`/api/v1/payments/${payment.id}/void`, { method: "POST", body: JSON.stringify({ reason: form.get("reason") }) });
      onSaved();
    } catch (cause) { setError(cause instanceof Error ? cause.message : "Pembayaran belum dapat dibatalkan."); }
    finally { setPending(false); }
  }
  return <div className="modal-backdrop" role="dialog" aria-modal="true" aria-labelledby="void-form-title"><form className="modal" onSubmit={submit}>
    <header className="modal-header"><h2 id="void-form-title">Batalkan pembayaran</h2><button className="icon-button" type="button" onClick={onClose} aria-label="Tutup"><X size={20} /></button></header>
    <div className="modal-body">
      {error && <div className="error-box" role="alert">{error}</div>}
      <p className="text-sm"><strong>{payment.payment_number}</strong> — {formatIDR(payment.amount)} untuk {payment.invoice_number}. Status tagihan akan dihitung ulang.</p>
      <div className="field"><label htmlFor="void-reason">Alasan pembatalan</label><textarea id="void-reason" name="reason" required maxLength={500} autoFocus placeholder="mis. salah input jumlah" /></div>
    </div>
    <footer className="modal-footer"><button type="button" className="secondary-button" onClick={onClose}>Batal</button><button className="primary-button" disabled={pending}>{pending ? "Memproses..." : "Batalkan pembayaran"}</button></footer>
  </form></div>;
}
