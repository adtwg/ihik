export type Tenant = {
  id: string;
  code: string;
  name: string;
  active: boolean;
  user_count: number;
  customer_count: number;
  created_at: string;
  archived_at?: string | null;
};

export type TenantPage = {
  items: Tenant[];
  page: number;
  page_size: number;
  total: number;
  filtered_total: number;
  sort: string;
  order: string;
};
