import { serverAPI } from "@/lib/api/server";
import type { SubscriptionPage } from "@/lib/subscriptions/types";
import type { CustomerPage } from "@/lib/customers/types";
import type { PlanPage } from "@/lib/plans/types";
import { SubscriptionTable } from "./subscription-table";

type SearchParams = Promise<Record<string, string | string[] | undefined>>;

export default async function SubscriptionsPage({ searchParams }: { searchParams: SearchParams }) {
  const raw = await searchParams;
  const query = new URLSearchParams();
  for (const key of ["page", "page_size", "search", "sort", "order", "status", "archived"]) {
    const value = raw[key];
    if (typeof value === "string" && value) query.set(key, value);
  }
  const [data, customers, plans] = await Promise.all([
    serverAPI<SubscriptionPage>(`/api/v1/services?${query.toString()}`),
    serverAPI<CustomerPage>("/api/v1/customers?page_size=100&sort=name&order=asc"),
    serverAPI<PlanPage>("/api/v1/plans?page_size=100&sort=name&order=asc"),
  ]);
  return (
    <div className="page">
      <div className="page-heading"><div><h1>Layanan</h1><p>Kelola langganan pelanggan: aktivasi, isolir, dan arsip.</p></div></div>
      <SubscriptionTable data={data} customers={customers.items} plans={plans.items.filter((plan) => plan.active)} />
    </div>
  );
}
