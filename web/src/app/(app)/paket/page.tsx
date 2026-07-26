import { serverAPI } from "@/lib/api/server";
import type { PlanPage } from "@/lib/plans/types";
import { PlanTable } from "./plan-table";

type SearchParams = Promise<Record<string, string | string[] | undefined>>;

export default async function PlansPage({ searchParams }: { searchParams: SearchParams }) {
  const raw = await searchParams;
  const query = new URLSearchParams();
  for (const key of ["page", "page_size", "search", "sort", "order", "archived"]) {
    const value = raw[key];
    if (typeof value === "string" && value) query.set(key, value);
  }
  const data = await serverAPI<PlanPage>(`/api/v1/plans?${query.toString()}`);
  return (
    <div className="page">
      <div className="page-heading"><div><h1>Paket layanan</h1><p>Kelola paket internet beserta harga dan pajaknya.</p></div></div>
      <PlanTable data={data} />
    </div>
  );
}
