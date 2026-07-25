import { serverAPI } from "@/lib/api/server";
import type { CustomerPage } from "@/lib/customers/types";
import { CustomerTable } from "./customer-table";

type SearchParams = Promise<Record<string, string | string[] | undefined>>;

export default async function CustomersPage({ searchParams }: { searchParams: SearchParams }) {
  const raw = await searchParams;
  const query = new URLSearchParams();
  for (const key of ["page", "page_size", "search", "sort", "order", "archived"]) {
    const value = raw[key];
    if (typeof value === "string" && value) query.set(key, value);
  }
  const data = await serverAPI<CustomerPage>(`/api/v1/customers?${query.toString()}`);
  return (
    <div className="page">
      <div className="page-heading"><div><h1>Pelanggan</h1><p>Kelola identitas pelanggan dan arsip layanan.</p></div></div>
      <CustomerTable data={data} />
    </div>
  );
}