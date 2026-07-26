export type Plan = {
  id: string;
  code: string;
  name: string;
  billing_period_months: number;
  price: string;
  tax_percent: string;
  active: boolean;
  service_count: number;
  created_at: string;
  archived_at?: string | null;
};

export type PlanPage = {
  items: Plan[];
  page: number;
  page_size: number;
  total: number;
  filtered_total: number;
  sort: string;
  order: string;
};
