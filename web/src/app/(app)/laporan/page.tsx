import { serverAPI } from "@/lib/api/server";
import type { ReportSummary } from "@/lib/reports/types";
import { formatIDR } from "@/lib/format";
import { ReportFilter } from "./report-filter";

type SearchParams = Promise<Record<string, string | string[] | undefined>>;

const statusLabels: Record<string, string> = {
  unpaid: "Belum dibayar",
  partial: "Sebagian",
  paid: "Lunas",
  overdue: "Jatuh tempo",
  void: "Batal",
};

function monthLabel(month: string): string {
  return new Date(`${month}-01`).toLocaleDateString("id-ID", { month: "short", year: "2-digit" });
}

export default async function ReportsPage({ searchParams }: { searchParams: SearchParams }) {
  const raw = await searchParams;
  const query = new URLSearchParams();
  for (const key of ["from", "to"]) {
    const value = raw[key];
    if (typeof value === "string" && value) query.set(key, value);
  }
  const data = await serverAPI<ReportSummary>(`/api/v1/reports/financial?${query.toString()}`);
  const maxMonthly = Math.max(1, ...data.monthly.flatMap((point) => [Number(point.invoiced), Number(point.collected)]));
  const collectRate = Number(data.totals.invoiced_amount) > 0
    ? Math.round((Number(data.totals.collected_amount) / Number(data.totals.invoiced_amount)) * 100)
    : 0;

  const kpis: Array<[string, string, string?]> = [
    ["Total tagihan", formatIDR(data.totals.invoiced_amount), `${data.totals.invoiced_count.toLocaleString("id-ID")} faktur`],
    ["Total penerimaan", formatIDR(data.totals.collected_amount), `${data.totals.collected_count.toLocaleString("id-ID")} pembayaran`],
    ["Tingkat penagihan", `${collectRate}%`, "penerimaan / tagihan"],
    ["Outstanding", formatIDR(data.totals.outstanding_amount), "seluruh periode"],
    ["Jatuh tempo", formatIDR(data.totals.overdue_amount), `${data.totals.overdue_count.toLocaleString("id-ID")} faktur`],
  ];

  return (
    <div className="page">
      <div className="page-heading">
        <div><h1>Laporan keuangan</h1><p>Periode {data.from} s.d. {data.to} · dibuat {new Date(data.generated_at).toLocaleString("id-ID", { dateStyle: "medium", timeStyle: "short" })}</p></div>
        <ReportFilter from={data.from} to={data.to} />
      </div>
      <section className="kpi-grid" aria-label="Ringkasan keuangan">
        {kpis.map(([label, value, hint]) => <div className="kpi-cell" key={label}><span className="kpi-label">{label}</span><strong className="kpi-value">{value}</strong>{hint && <span className="text-xs text-[#607067]">{hint}</span>}</div>)}
      </section>
      <div className="dashboard-grid">
        <section className="panel">
          <div className="panel-header"><h2>Tagihan vs penerimaan per bulan</h2><div className="flex gap-4 text-xs text-[#607067]"><span><i className="mr-1 inline-block h-2 w-2 bg-[#096b4c]" />Tagihan</span><span><i className="mr-1 inline-block h-2 w-2 bg-[#d9a514]" />Penerimaan</span></div></div>
          <div className="panel-body">
            <div className="trend-chart" role="img" aria-label="Grafik tagihan dan penerimaan per bulan">
              {data.monthly.map((point) => <div className="trend-group" key={point.month}><div className="trend-bars"><div className="trend-bar bg-[#096b4c]" style={{ height: `${Math.max(2, Number(point.invoiced) / maxMonthly * 100)}%` }} title={`Tagihan ${formatIDR(point.invoiced)}`} /><div className="trend-bar bg-[#d9a514]" style={{ height: `${Math.max(2, Number(point.collected) / maxMonthly * 100)}%` }} title={`Penerimaan ${formatIDR(point.collected)}`} /></div><span className="trend-month">{monthLabel(point.month)}</span></div>)}
            </div>
            <div className="data-table-wrap mt-4">
              <table className="data-table">
                <thead><tr><th>Bulan</th><th>Tagihan</th><th>Penerimaan</th><th>Selisih</th></tr></thead>
                <tbody>{data.monthly.map((point) => <tr key={point.month}>
                  <td>{new Date(`${point.month}-01`).toLocaleDateString("id-ID", { month: "long", year: "numeric" })}</td>
                  <td>{formatIDR(point.invoiced)}</td>
                  <td>{formatIDR(point.collected)}</td>
                  <td className={Number(point.collected) - Number(point.invoiced) < 0 ? "text-[#b3261e]" : "text-[#096b4c]"}>{formatIDR(Number(point.collected) - Number(point.invoiced))}</td>
                </tr>)}</tbody>
              </table>
            </div>
          </div>
        </section>
        <div className="grid gap-6">
          <section className="panel">
            <div className="panel-header"><h2>Status tagihan (periode terpilih)</h2></div>
            <div className="action-list">
              {data.status_breakdown.length === 0 && <div className="p-4 text-sm text-[#607067]">Belum ada tagihan pada rentang ini.</div>}
              {data.status_breakdown.map((metric) => <div className="action-row" key={metric.status}><span>{statusLabels[metric.status] ?? metric.status} ({metric.count.toLocaleString("id-ID")})</span><b className="action-count">{formatIDR(metric.amount)}</b></div>)}
            </div>
          </section>
          <section className="panel">
            <div className="panel-header"><h2>Metode pembayaran</h2></div>
            <div className="action-list">
              {data.method_breakdown.length === 0 && <div className="p-4 text-sm text-[#607067]">Belum ada pembayaran pada rentang ini.</div>}
              {data.method_breakdown.map((metric) => <div className="action-row" key={metric.method}><span>{metric.method === "cash" ? "Tunai" : "Transfer manual"} ({metric.count.toLocaleString("id-ID")})</span><b className="action-count">{formatIDR(metric.amount)}</b></div>)}
            </div>
          </section>
        </div>
      </div>
    </div>
  );
}
