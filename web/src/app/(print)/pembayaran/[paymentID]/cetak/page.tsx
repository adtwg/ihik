import { serverAPI } from "@/lib/api/server";
import type { Payment } from "@/lib/payments/types";
import { formatIDR, formatDateTime } from "@/lib/format";
import { PrintActions } from "@/components/print/print-actions";

export default async function ReceiptPrintPage({ params }: { params: Promise<{ paymentID: string }> }) {
  const { paymentID } = await params;
  const payment = await serverAPI<Payment>(`/api/v1/payments/${paymentID}`);
  return (
    <div className="print-sheet receipt">
      <PrintActions />
      <header className="print-header">
        <div>
          <div className="print-brand">ISP Billing</div>
          <div className="text-sm text-[#607067]">Bukti pembayaran</div>
        </div>
        <div className="text-right">
          <h1 className="print-title">KUITANSI</h1>
          <div className="font-semibold">{payment.receipt_number}</div>
          {payment.status === "voided" && <div className="print-status void">DIBATALKAN</div>}
        </div>
      </header>
      <section className="print-meta">
        <div>
          <h2>Diterima dari</h2>
          <p className="font-semibold">{payment.customer_name}</p>
          <p>{payment.customer_number}</p>
        </div>
        <div className="text-right">
          <p>Tanggal: {formatDateTime(payment.received_at)}</p>
          <p>Kasir: {payment.received_by_name}</p>
        </div>
      </section>
      <table className="print-table">
        <tbody>
          <tr><td>Nomor pembayaran</td><td className="text-right">{payment.payment_number}</td></tr>
          <tr><td>Untuk tagihan</td><td className="text-right">{payment.invoice_number}</td></tr>
          <tr><td>Metode</td><td className="text-right">{payment.method === "cash" ? "Tunai" : "Transfer manual"}</td></tr>
          <tr className="print-total"><td>Jumlah diterima</td><td className="text-right">{formatIDR(payment.amount)}</td></tr>
        </tbody>
      </table>
      {payment.status === "voided" && <p className="mt-4 text-sm text-[#b3261e]">Dibatalkan {payment.voided_at ? formatDateTime(payment.voided_at) : ""}: {payment.void_reason}</p>}
      <footer className="print-footer">Simpan kuitansi ini sebagai bukti pembayaran yang sah.</footer>
    </div>
  );
}
