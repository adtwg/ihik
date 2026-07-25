import Link from "next/link";
import { AlertTriangle, CircleDollarSign, RefreshCw, Router, Users, type LucideIcon } from "lucide-react";
import { serverAPI } from "@/lib/api/server";
import type { DashboardSnapshot } from "@/lib/dashboard/types";

const rupiah = new Intl.NumberFormat("id-ID", { style: "currency", currency: "IDR", maximumFractionDigits: 0 });

function money(value: string) {
  return rupiah.format(Number(value));
}

export default async function DashboardPage() {
  const data = await serverAPI<DashboardSnapshot>("/api/v1/dashboard");
  const maxTrend = Math.max(1, ...data.trend.flatMap((point) => [Number(point.invoiced), Number(point.received)]));
  const kpis = [
    ["Layanan aktif", data.services.active.toLocaleString("id-ID")],
    ["Terisolir", data.services.isolated.toLocaleString("id-ID")],
    ["Tagihan berjalan", data.billing.invoices_this_month.toLocaleString("id-ID")],
    ["Outstanding", money(data.billing.outstanding)],
    ["Penerimaan bulan ini", money(data.billing.received_this_month)],
    ["Router online", `${data.network.routers_online}/${data.network.routers_online + data.network.routers_offline}`],
  ];
  const actions: Array<{ label: string; count: number; icon: LucideIcon; href?: string }> = [
    { label: "Invoice perlu ditagih", count: data.billing.overdue, icon: CircleDollarSign },
    { label: "Konflik sinkronisasi", count: data.network.unresolved_conflicts, icon: AlertTriangle },
    { label: "Provisioning gagal", count: data.network.failed_provisioning, icon: Router },
    { label: "Layanan menunggu", count: data.services.pending, icon: Users },
  ];

  return (
    <div className="page">
      <div className="page-heading">
        <div><h1>Dashboard</h1><p>Diperbarui {new Date(data.generated_at).toLocaleString("id-ID", { dateStyle: "medium", timeStyle: "short" })}</p></div>
        <Link href="/dashboard" className="secondary-button"><RefreshCw size={17} /> Segarkan</Link>
      </div>
      <section className="kpi-grid" aria-label="Ringkasan operasional">
        {kpis.map(([label, value]) => <div className="kpi-cell" key={label}><span className="kpi-label">{label}</span><strong className="kpi-value">{value}</strong></div>)}
      </section>
      <div className="dashboard-grid">
        <section className="panel">
          <div className="panel-header"><h2>Tagihan dan penerimaan</h2><div className="flex gap-4 text-xs text-[#607067]"><span><i className="mr-1 inline-block h-2 w-2 bg-[#096b4c]" />Tagihan</span><span><i className="mr-1 inline-block h-2 w-2 bg-[#d9a514]" />Pembayaran</span></div></div>
          <div className="panel-body">
            <div className="trend-chart" role="img" aria-label="Grafik tagihan dan penerimaan enam bulan">
              {data.trend.map((point) => <div className="trend-group" key={point.month}><div className="trend-bars"><div className="trend-bar bg-[#096b4c]" style={{ height: `${Math.max(2, Number(point.invoiced) / maxTrend * 100)}%` }} title={`Tagihan ${money(point.invoiced)}`} /><div className="trend-bar bg-[#d9a514]" style={{ height: `${Math.max(2, Number(point.received) / maxTrend * 100)}%` }} title={`Pembayaran ${money(point.received)}`} /></div><span className="trend-month">{new Date(`${point.month}-01`).toLocaleDateString("id-ID", { month: "short" })}</span></div>)}
            </div>
          </div>
        </section>
        <section className="panel">
          <div className="panel-header"><h2>Perlu tindakan</h2></div>
          <div className="action-list">
            {actions.map(({ label, count, href, icon: Icon }) => href ? <Link className="action-row" href={href} key={label}><span className="flex items-center gap-3"><Icon size={18} className="text-[#607067]" />{label}</span><b className="action-count">{count}</b></Link> : <div className="action-row" key={label}><span className="flex items-center gap-3"><Icon size={18} className="text-[#607067]" />{label}</span><b className="action-count">{count}</b></div>)}
          </div>
        </section>
      </div>
    </div>
  );
}