import { serverAPI } from "@/lib/api/server";
import type { PaymentPage } from "@/lib/payments/types";
import { PaymentTable } from "./payment-table";

type SearchParams = Promise<Record<string, string | string[] | undefined>>;

export default async function PaymentsPage({ searchParams }: { searchParams: SearchParams }) {
  const raw = await searchParams;
  const query = new URLSearchParams();
  for (const key of ["page", "page_size", "search", "sort", "order", "method", "status"]) {
    const value = raw[key];
    if (typeof value === "string" && value) query.set(key, value);
  }
  const data = await serverAPI<PaymentPage>(`/api/v1/payments?${query.toString()}`);
  return (
    <div className="page">
      <div className="page-heading"><div><h1>Pembayaran</h1><p>Riwayat penerimaan pembayaran dan kuitansi. Pembayaran baru dicatat dari menu Tagihan.</p></div></div>
      <PaymentTable data={data} />
    </div>
  );
}
