import { serverAPI } from "@/lib/api/server";
import type { TenantPage } from "@/lib/tenants/types";
import { TenantTable } from "./tenant-table";

type SearchParams = Promise<Record<string, string | string[] | undefined>>;

export default async function TenantsPage({ searchParams }: { searchParams: SearchParams }) {
  const raw = await searchParams;
  const query = new URLSearchParams();
  for (const key of ["page", "page_size", "search", "sort", "order"]) {
    const value = raw[key];
    if (typeof value === "string" && value) query.set(key, value);
  }
  const data = await serverAPI<TenantPage>(`/api/v1/platform/tenants?${query.toString()}`);
  return (
    <div className="page">
      <div className="page-heading"><div><h1>Mitra</h1><p>Kelola mitra ISP dan akun admin mereka.</p></div></div>
      <TenantTable data={data} />
    </div>
  );
}
