export type ReportTotals = {
  invoiced_amount: string;
  invoiced_count: number;
  collected_amount: string;
  collected_count: number;
  outstanding_amount: string;
  overdue_amount: string;
  overdue_count: number;
};

export type StatusMetric = { status: string; count: number; amount: string };
export type MethodMetric = { method: string; count: number; amount: string };
export type MonthMetric = { month: string; invoiced: string; collected: string };

export type ReportSummary = {
  from: string;
  to: string;
  generated_at: string;
  totals: ReportTotals;
  status_breakdown: StatusMetric[];
  method_breakdown: MethodMetric[];
  monthly: MonthMetric[];
};
