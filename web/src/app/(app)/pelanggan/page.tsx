import { serverAPI } from "@/lib/api/server";
import type { CustomerPage } from "@/lib/customers/types";
import type { PlanPage } from "@/lib/plans/types";
import { CustomerTable } from "./customer-table";

type SearchParams = Promise<Record<string, string | string[] | undefined>>;

export default async function CustomersPage({ searchParams }: { searchParams: SearchParams }) {
  const raw = await searchParams;
  const query = new URLSearchParams();
  for (const key of ["page", "page_size", "search", "sort", "order", "status", "archived"]) {
    const value = raw[key];
    if (typeof value === "string" && value) query.set(key, value);
  }
  const [data, plans] = await Promise.all([
    serverAPI<CustomerPage>(`/api/v1/customers?${query.toString()}`),
    serverAPI<PlanPage>("/api/v1/plans?page_size=100&sort=name&order=asc"),
  ]);
  return (
    <div className="page">
      <div className="page-heading"><div><h1>Pelanggan</h1><p>Kelola pelanggan, paket, tagihan, dan koneksi PPPoE dalam satu tempat.</p></div></div>
      <CustomerTable data={data} plans={plans.items.filter((plan) => plan.active)} />
    </div>
  );
}