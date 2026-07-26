export type Subscription = {
  id: string;
  service_number: string;
  customer_id: string;
  customer_name: string;
  customer_number: string;
  package_id: string;
  package_name: string;
  package_code: string;
  price: string;
  status: string;
  activated_at?: string | null;
  isolated_at?: string | null;
  created_at: string;
  archived_at?: string | null;
};

export type SubscriptionPage = {
  items: Subscription[];
  page: number;
  page_size: number;
  total: number;
  filtered_total: number;
  sort: string;
  order: string;
};
