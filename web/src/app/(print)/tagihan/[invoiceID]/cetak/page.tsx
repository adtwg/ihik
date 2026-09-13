import { serverAPI } from "@/lib/api/server";
import type { InvoiceDetail } from "@/lib/invoices/types";
import { formatIDR, formatDate } from "@/lib/format";
import { PrintActions } from "@/components/print/print-actions";

const statusLabels: Record<string, string> = {
  unpaid: "BELUM DIBAYAR",
  partial: "DIBAYAR SEBAGIAN",
  paid: "LUNAS",
  overdue: "JATUH TEMPO",
  void: "DIBATALKAN",
};

export default async function InvoicePrintPage({ params }: { params: Promise<{ invoiceID: string }> }) {
  const { invoiceID } = await params;
  const invoice = await serverAPI<InvoiceDetail>(`/api/v1/invoices/${invoiceID}`);
  const outstanding = Number(invoice.total) - Number(invoice.paid_amount);
  return (
    <div className="print-sheet">
      <PrintActions />
      <header className="print-header">
        <div>
          <div className="print-brand">AWGRevBILL</div>
          <div className="text-sm text-[#607067]">Tagihan layanan internet</div>
        </div>
        <div className="text-right">
          <h1 className="print-title">TAGIHAN</h1>
          <div className="font-semibold">{invoice.invoice_number}</div>
          <div className={`print-status ${invoice.status === "paid" ? "paid" : invoice.status === "void" ? "void" : "open"}`}>{statusLabels[invoice.status] ?? invoice.status}</div>
        </div>
      </header>
      <section className="print-meta">
        <div>
          <h2>Ditagihkan kepada</h2>
          <p className="font-semibold">{invoice.customer_name}</p>
          <p>{invoice.customer_number} · {invoice.service_number}</p>
        </div>
        <div className="text-right">
          <p>Terbit: {formatDate(invoice.issued_at)}</p>
          <p>Jatuh tempo: <strong>{formatDate(invoice.due_at)}</strong></p>
          <p>Periode: {formatDate(invoice.period_start)} – {formatDate(invoice.period_end)}</p>
        </div>
      </section>
      <table className="print-table">
        <thead><tr><th>Deskripsi</th><th className="text-right">Harga</th><th className="text-right">Pajak</th><th className="text-right">Jumlah</th></tr></thead>
        <tbody>
          {invoice.lines.map((line) => <tr key={line.id}>
            <td>{line.description}</td>
            <td className="text-right">{formatIDR(line.unit_price)}</td>
            <td className="text-right">{Number(line.tax_percent).toLocaleString("id-ID")}%</td>
            <td className="text-right">{formatIDR(line.line_total)}</td>
          </tr>)}
        </tbody>
        <tfoot>
          <tr><td colSpan={3}>Subtotal</td><td className="text-right">{formatIDR(invoice.subtotal)}</td></tr>
          <tr><td colSpan={3}>Pajak</td><td className="text-right">{formatIDR(invoice.tax_amount)}</td></tr>
          <tr className="print-total"><td colSpan={3}>Total</td><td className="text-right">{formatIDR(invoice.total)}</td></tr>
          <tr><td colSpan={3}>Sudah dibayar</td><td className="text-right">{formatIDR(invoice.paid_amount)}</td></tr>
          <tr className="print-total"><td colSpan={3}>Sisa tagihan</td><td className="text-right">{formatIDR(outstanding)}</td></tr>
        </tfoot>
      </table>
      {invoice.payments.length > 0 && <section className="mt-6">
        <h2 className="mb-2 text-sm font-bold uppercase text-[#607067]">Riwayat pembayaran</h2>
        <table className="print-table">
          <thead><tr><th>Nomor</th><th>Metode</th><th>Tanggal</th><th className="text-right">Jumlah</th></tr></thead>
          <tbody>{invoice.payments.map((item) => <tr key={item.payment_id}>
            <td>{item.payment_number}{item.status === "voided" ? " (batal)" : ""}</td>
            <td>{item.method === "cash" ? "Tunai" : "Transfer"}</td>
            <td>{formatDate(item.received_at)}</td>
            <td className="text-right">{formatIDR(item.amount)}</td>
          </tr>)}</tbody>
        </table>
      </section>}
      <footer className="print-footer">Dokumen ini dibuat otomatis oleh sistem dan sah tanpa tanda tangan.</footer>
    </div>
  );
}
