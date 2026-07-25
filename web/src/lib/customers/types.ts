export type Customer = {
  id: string;
  customer_number: string;
  name: string;
  phone?: string;
  email?: string;
  address?: string;
  created_at: string;
  archived_at?: string;
};

export type CustomerPage = {
  items: Customer[];
  page: number;
  page_size: number;
  total: number;
  filtered_total: number;
  sort: string;
  order: "asc" | "desc";
};