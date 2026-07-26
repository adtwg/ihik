"use client";

import { startTransition, useEffect, useState, type FormEvent } from "react";
import { ArrowDown, ArrowUp, Ban, ChevronLeft, ChevronRight, Eye, FilePlus2, HandCoins, Search, X } from "lucide-react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { clientAPI } from "@/lib/api/client";
import { formatIDR, formatDate } from "@/lib/format";
import type { GenerateResult, Invoice, InvoiceDetail, InvoicePage } from "@/lib/invoices/types";

const statusLabels: Record<string, { label: string; className: string }> = {
  unpaid: { label: "Belum dibayar", className: "warning" },
  partial: { label: "Sebagian", className: "info" },
  paid: { label: "Lunas", className: "" },
  overdue: { label: "Jatuh tempo", className: "danger" },
  void: { label: "Batal", className: "archived" },
};

const sortableColumns = [
  { id: "invoice_number", label: "Nomor tagihan" },
  { id: "customer_name", label: "Pelanggan" },
  { id: "due_at", label: "Jatuh tempo" },
  { id: "total", label: "Total" },
] as const;

function statusBadge(status: string) {
  const meta = statusLabels[status] ?? { label: status, className: "info" };
  return <span className={`status-badge ${meta.className}`}>{meta.label}</span>;
}

function outstanding(invoice: Invoice): number {
  return Number(invoice.total) - Number(invoice.paid_amount);
}

export function InvoiceTable({ data }: { data: InvoicePage }) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const [search, setSearch] = useState(searchParams.get("search") ?? "");
  const [generateOpen, setGenerateOpen] = useState(false);
  const [payTarget, setPayTarget] = useState<Invoice | null>(null);
  const [detailTarget, setDetailTarget] = useState<Invoice | null>(null);

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

  async function voidInvoice(invoice: Invoice) {
    if (!window.confirm(`Batalkan tagihan ${invoice.invoice_number}? Hanya bisa jika belum ada pembayaran.`)) return;
    await clientAPI(`/api/v1/invoices/${invoice.id}/void`, { method: "POST" });
    router.refresh();
  }

  const pageCount = Math.max(1, Math.ceil(data.filtered_total / data.page_size));
  const payable = (invoice: Invoice) => invoice.status === "unpaid" || invoice.status === "partial" || invoice.status === "overdue";

  return (
    <>
      <section className="table-shell">
        <div className="table-toolbar">
          <div className="relative w-full max-w-[380px]"><Search className="absolute left-3 top-3 text-[#607067]" size={18} /><input className="search-input pl-10" value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Cari nomor tagihan atau pelanggan" aria-label="Cari tagihan" /></div>
          <div className="flex flex-wrap gap-2">
            <input className="secondary-button" type="month" value={searchParams.get("period") ?? ""} onChange={(event) => updateQuery({ period: event.target.value || null, page: "1" })} aria-label="Filter periode" />
            <select className="secondary-button" value={searchParams.get("status") ?? ""} onChange={(event) => updateQuery({ status: event.target.value || null, page: "1" })} aria-label="Filter status"><option value="">Semua status</option><option value="unpaid">Belum dibayar</option><option value="partial">Sebagian</option><option value="overdue">Jatuh tempo</option><option value="paid">Lunas</option><option value="void">Batal</option></select>
            <button className="primary-button" onClick={() => setGenerateOpen(true)}><FilePlus2 size={18} /> Terbitkan</button>
          </div>
        </div>
        <div className="data-table-wrap">
          <table className="data-table">
            <thead><tr>
              {sortableColumns.map((column) => <th key={column.id}><button className="sort-button" onClick={() => toggleSort(column.id)}>{column.label}{data.sort === column.id ? (data.order === "asc" ? <ArrowUp size={13} /> : <ArrowDown size={13} />) : null}</button></th>)}
              <th>Periode</th><th>Dibayar</th><th>Status</th><th aria-label="Aksi" />
            </tr></thead>
            <tbody>{data.items.map((invoice) => <tr key={invoice.id}>
              <td>{invoice.invoice_number}</td>
              <td><div>{invoice.customer_name}</div><div className="text-xs text-[#607067]">{invoice.service_number}</div></td>
              <td>{formatDate(invoice.due_at)}</td>
              <td>{formatIDR(invoice.total)}</td>
              <td>{new Date(invoice.period_start).toLocaleDateString("id-ID", { month: "short", year: "numeric" })}</td>
              <td>{formatIDR(invoice.paid_amount)}</td>
              <td>{statusBadge(invoice.status)}</td>
              <td className="w-32"><div className="flex gap-1">
                <button className="icon-button" title="Lihat rincian" aria-label={`Rincian ${invoice.invoice_number}`} onClick={() => setDetailTarget(invoice)}><Eye size={17} /></button>
                {payable(invoice) && <button className="icon-button" title="Terima pembayaran" aria-label={`Bayar ${invoice.invoice_number}`} onClick={() => setPayTarget(invoice)}><HandCoins size={17} /></button>}
                {payable(invoice) && Number(invoice.paid_amount) === 0 && <button className="icon-button" title="Batalkan tagihan" aria-label={`Batalkan ${invoice.invoice_number}`} onClick={() => voidInvoice(invoice)}><Ban size={17} /></button>}
              </div></td>
            </tr>)}</tbody>
          </table>
        </div>
        <div className="mobile-records">{data.items.map((invoice) => <article className="mobile-record" key={invoice.id}>
          <div className="mobile-record-head"><div><strong>{invoice.customer_name}</strong><div className="mt-1 text-xs text-[#607067]">{invoice.invoice_number}</div></div>{statusBadge(invoice.status)}</div>
          <div className="flex items-center justify-between gap-3 text-sm"><span className="text-[#607067]">{formatIDR(invoice.total)} · tempo {formatDate(invoice.due_at)}</span><div className="flex gap-1">
            <button className="icon-button" aria-label={`Rincian ${invoice.invoice_number}`} onClick={() => setDetailTarget(invoice)}><Eye size={17} /></button>
            {payable(invoice) && <button className="icon-button" aria-label={`Bayar ${invoice.invoice_number}`} onClick={() => setPayTarget(invoice)}><HandCoins size={17} /></button>}
          </div></div>
        </article>)}</div>
        {data.items.length === 0 && <div className="p-10 text-center text-sm text-[#607067]">Belum ada tagihan. Gunakan tombol Terbitkan untuk membuat tagihan periode berjalan.</div>}
        <footer className="table-footer"><span>Menampilkan {data.items.length} dari {data.filtered_total.toLocaleString("id-ID")} tagihan</span><div className="pagination"><select className="secondary-button" value={data.page_size} onChange={(event) => updateQuery({ page_size: event.target.value, page: "1" })} aria-label="Baris per halaman">{[10,25,50,100].map((size) => <option key={size} value={size}>{size}</option>)}</select><button className="icon-button" disabled={data.page <= 1} onClick={() => updateQuery({ page: String(data.page - 1) })} aria-label="Halaman sebelumnya"><ChevronLeft size={18} /></button><span>{data.page} / {pageCount}</span><button className="icon-button" disabled={data.page >= pageCount} onClick={() => updateQuery({ page: String(data.page + 1) })} aria-label="Halaman berikutnya"><ChevronRight size={18} /></button></div></footer>
      </section>
      {generateOpen && <GenerateForm onClose={() => setGenerateOpen(false)} onDone={() => { setGenerateOpen(false); router.refresh(); }} />}
      {payTarget && <PaymentForm invoice={payTarget} onClose={() => setPayTarget(null)} onSaved={() => { setPayTarget(null); router.refresh(); }} />}
      {detailTarget && <InvoiceDetailModal invoice={detailTarget} onClose={() => setDetailTarget(null)} />}
    </>
  );
}

function GenerateForm({ onClose, onDone }: { onClose: () => void; onDone: () => void }) {
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [result, setResult] = useState<GenerateResult | null>(null);
  const currentPeriod = new Date().toISOString().slice(0, 7);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setPending(true); setError("");
    const form = new FormData(event.currentTarget);
    try {
      const outcome = await clientAPI<GenerateResult>("/api/v1/invoices/generate", { method: "POST", body: JSON.stringify({ period: form.get("period"), due_days: Number(form.get("due_days") || 14) }) });
      setResult(outcome);
    } catch (cause) { setError(cause instanceof Error ? cause.message : "Tagihan belum dapat diterbitkan."); }
    finally { setPending(false); }
  }
  return <div className="modal-backdrop" role="dialog" aria-modal="true" aria-labelledby="generate-form-title"><form className="modal" onSubmit={submit}>
    <header className="modal-header"><h2 id="generate-form-title">Terbitkan tagihan</h2><button className="icon-button" type="button" onClick={onClose} aria-label="Tutup"><X size={20} /></button></header>
    <div className="modal-body">
      {error && <div className="error-box" role="alert">{error}</div>}
      {result ? (
        <div className="grid gap-2 text-sm">
          <p><strong>Periode {result.period}</strong></p>
          <p>Tagihan baru diterbitkan: <strong>{result.created.toLocaleString("id-ID")}</strong></p>
          <p>Sudah punya tagihan (dilewati): <strong>{result.skipped.toLocaleString("id-ID")}</strong></p>
          {result.without_price > 0 && <p className="text-[#8a6a10]">Layanan tanpa harga paket berlaku (dilewati): <strong>{result.without_price.toLocaleString("id-ID")}</strong></p>}
        </div>
      ) : (
        <>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="field"><label htmlFor="generate-period">Periode</label><input id="generate-period" name="period" type="month" required defaultValue={currentPeriod} /></div>
            <div className="field"><label htmlFor="generate-due">Jatuh tempo (hari)</label><input id="generate-due" name="due_days" type="number" min={1} max={60} defaultValue={14} /></div>
          </div>
          <p className="text-xs text-[#607067]">Satu tagihan per layanan aktif/terisolir. Proses ini aman diulang: layanan yang sudah punya tagihan periode tersebut dilewati.</p>
        </>
      )}
    </div>
    <footer className="modal-footer">
      {result ? <button type="button" className="primary-button" onClick={onDone}>Selesai</button> : <>
        <button type="button" className="secondary-button" onClick={onClose}>Batal</button>
        <button className="primary-button" disabled={pending}>{pending ? "Memproses..." : "Terbitkan"}</button>
      </>}
    </footer>
  </form></div>;
}

function PaymentForm({ invoice, onClose, onSaved }: { invoice: Invoice; onClose: () => void; onSaved: () => void }) {
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const remaining = outstanding(invoice);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setPending(true); setError("");
    const form = new FormData(event.currentTarget);
    try {
      await clientAPI("/api/v1/payments", { method: "POST", body: JSON.stringify({ invoice_id: invoice.id, amount: Number(form.get("amount")), method: form.get("method") }) });
      onSaved();
    } catch (cause) { setError(cause instanceof Error ? cause.message : "Pembayaran belum dapat disimpan."); }
    finally { setPending(false); }
  }
  return <div className="modal-backdrop" role="dialog" aria-modal="true" aria-labelledby="payment-form-title"><form className="modal" onSubmit={submit}>
    <header className="modal-header"><h2 id="payment-form-title">Terima pembayaran</h2><button className="icon-button" type="button" onClick={onClose} aria-label="Tutup"><X size={20} /></button></header>
    <div className="modal-body">
      {error && <div className="error-box" role="alert">{error}</div>}
      <div className="grid gap-1 text-sm">
        <p><strong>{invoice.invoice_number}</strong> — {invoice.customer_name}</p>
        <p>Total {formatIDR(invoice.total)} · Sisa <strong>{formatIDR(remaining)}</strong></p>
      </div>
      <div className="grid gap-4 sm:grid-cols-2">
        <div className="field"><label htmlFor="payment-amount">Jumlah (Rp)</label><input id="payment-amount" name="amount" type="number" min={1} max={remaining} step="0.01" required defaultValue={remaining} autoFocus /></div>
        <div className="field"><label htmlFor="payment-method">Metode</label><select id="payment-method" name="method" required><option value="cash">Tunai</option><option value="manual_transfer">Transfer manual</option></select></div>
      </div>
      <p className="text-xs text-[#607067]">Kuitansi dibuat otomatis. Pembayaran dapat dibatalkan dari menu Pembayaran.</p>
    </div>
    <footer className="modal-footer"><button type="button" className="secondary-button" onClick={onClose}>Batal</button><button className="primary-button" disabled={pending}>{pending ? "Menyimpan..." : "Simpan pembayaran"}</button></footer>
  </form></div>;
}

function InvoiceDetailModal({ invoice, onClose }: { invoice: Invoice; onClose: () => void }) {
  const [detail, setDetail] = useState<InvoiceDetail | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    let cancelled = false;
    clientAPI<InvoiceDetail>(`/api/v1/invoices/${invoice.id}`)
      .then((payload) => { if (!cancelled) setDetail(payload); })
      .catch((cause) => { if (!cancelled) setError(cause instanceof Error ? cause.message : "Rincian belum dapat dimuat."); });
    return () => { cancelled = true; };
  }, [invoice.id]);
  return <div className="modal-backdrop" role="dialog" aria-modal="true" aria-labelledby="invoice-detail-title"><div className="modal">
    <header className="modal-header"><h2 id="invoice-detail-title">{invoice.invoice_number}</h2><button className="icon-button" type="button" onClick={onClose} aria-label="Tutup"><X size={20} /></button></header>
    <div className="modal-body">
      {error && <div className="error-box" role="alert">{error}</div>}
      {!detail && !error && <p className="text-sm text-[#607067]">Memuat rincian…</p>}
      {detail && <div className="grid gap-4 text-sm">
        <div className="grid gap-1">
          <p><strong>{detail.customer_name}</strong> ({detail.customer_number}) · {detail.service_number}</p>
          <p>Periode {formatDate(detail.period_start)} – {formatDate(detail.period_end)} · Jatuh tempo {formatDate(detail.due_at)}</p>
          <p>Status: {statusBadge(detail.status)}</p>
        </div>
        <div>
          <h3 className="mb-2 font-semibold">Rincian item</h3>
          {detail.lines.map((line) => <div key={line.id} className="flex items-center justify-between gap-3 border-b border-[#eef1ef] py-2">
            <span>{line.description}</span><span>{formatIDR(line.line_total)}</span>
          </div>)}
          <div className="flex items-center justify-between gap-3 py-2"><span>Subtotal</span><span>{formatIDR(detail.subtotal)}</span></div>
          <div className="flex items-center justify-between gap-3"><span>Pajak</span><span>{formatIDR(detail.tax_amount)}</span></div>
          <div className="flex items-center justify-between gap-3 py-2 font-bold"><span>Total</span><span>{formatIDR(detail.total)}</span></div>
          <div className="flex items-center justify-between gap-3 text-[#096b4c]"><span>Dibayar</span><span>{formatIDR(detail.paid_amount)}</span></div>
        </div>
        {detail.payments.length > 0 && <div>
          <h3 className="mb-2 font-semibold">Pembayaran</h3>
          {detail.payments.map((item) => <div key={item.payment_id} className="flex items-center justify-between gap-3 border-b border-[#eef1ef] py-2">
            <span>{item.payment_number} · {item.method === "cash" ? "Tunai" : "Transfer"} · {formatDate(item.received_at)}</span>
            <span className={item.status === "voided" ? "line-through text-[#8a8f8c]" : ""}>{formatIDR(item.amount)}</span>
          </div>)}
        </div>}
      </div>}
    </div>
    <footer className="modal-footer"><button type="button" className="primary-button" onClick={onClose}>Tutup</button></footer>
  </div></div>;
}
