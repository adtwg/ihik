export type Invoice = {
  id: string;
  invoice_number: string;
  customer_id: string;
  customer_name: string;
  customer_number: string;
  service_id: string;
  service_number: string;
  period_start: string;
  period_end: string;
  issued_at: string;
  due_at: string;
  status: string;
  subtotal: string;
  tax_amount: string;
  total: string;
  paid_amount: string;
  created_at: string;
};

export type InvoiceLine = {
  id: string;
  item_code: string;
  description: string;
  quantity: string;
  unit_price: string;
  tax_percent: string;
  line_total: string;
};

export type InvoicePayment = {
  payment_id: string;
  payment_number: string;
  amount: string;
  method: string;
  received_at: string;
  status: string;
};

export type InvoiceDetail = Invoice & {
  lines: InvoiceLine[];
  payments: InvoicePayment[];
};

export type InvoicePage = {
  items: Invoice[];
  page: number;
  page_size: number;
  total: number;
  filtered_total: number;
  sort: string;
  order: string;
};

export type GenerateResult = {
  period: string;
  created: number;
  skipped: number;
  without_price: number;
};
