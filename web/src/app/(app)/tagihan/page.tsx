import { serverAPI } from "@/lib/api/server";
import type { InvoicePage } from "@/lib/invoices/types";
import { InvoiceTable } from "./invoice-table";

type SearchParams = Promise<Record<string, string | string[] | undefined>>;

export default async function InvoicesPage({ searchParams }: { searchParams: SearchParams }) {
  const raw = await searchParams;
  const query = new URLSearchParams();
  for (const key of ["page", "page_size", "search", "sort", "order", "status", "period"]) {
    const value = raw[key];
    if (typeof value === "string" && value) query.set(key, value);
  }
  const data = await serverAPI<InvoicePage>(`/api/v1/invoices?${query.toString()}`);
  return (
    <div className="page">
      <div className="page-heading"><div><h1>Tagihan</h1><p>Terbitkan tagihan bulanan dan pantau pembayarannya.</p></div></div>
      <InvoiceTable data={data} />
    </div>
  );
}
