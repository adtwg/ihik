export type Customer = {
  id: string;
  customer_number: string;
  name: string;
  phone?: string;
  email?: string;
  address?: string;
  created_at: string;
  archived_at?: string;
  service_id?: string;
  service_number?: string;
  service_status?: string;
  package_id?: string;
  package_name?: string;
  package_price?: string;
  pppoe_username?: string;
  open_invoices: number;
  outstanding: string;
  last_invoice_number?: string;
  last_invoice_status?: string;
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

export type SyncSummary = {
  profiles_seen: number;
  packages_created: number;
  packages_updated: number;
  packages_without_price: number;
  secrets_seen: number;
  secrets_skipped: number;
  customers_created: number;
  accounts_linked: number;
  accounts_updated: number;
  services_isolated: number;
  services_restored: number;
};

export type ConnectionStatus = {
  online: boolean;
  username: string;
  address?: string;
  uptime?: string;
  caller_id?: string;
  secret_disabled: boolean;
};