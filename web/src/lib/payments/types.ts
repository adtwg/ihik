export type Payment = {
  id: string;
  payment_number: string;
  receipt_number: string;
  invoice_id: string;
  invoice_number: string;
  customer_name: string;
  customer_number: string;
  amount: string;
  method: string;
  status: string;
  received_by_name: string;
  received_at: string;
  void_reason?: string;
  voided_at?: string | null;
};

export type PaymentPage = {
  items: Payment[];
  page: number;
  page_size: number;
  total: number;
  filtered_total: number;
  sort: string;
  order: string;
};
