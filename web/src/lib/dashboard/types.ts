export type DashboardSnapshot = {
  generated_at: string;
  services: { active: number; isolated: number; pending: number; archived: number };
  billing: {
    invoices_this_month: number;
    paid_this_month: number;
    overdue: number;
    outstanding: string;
    received_today: string;
    received_this_month: string;
  };
  network: {
    routers_online: number;
    routers_offline: number;
    unresolved_conflicts: number;
    failed_provisioning: number;
  };
  trend: Array<{ month: string; invoiced: string; received: string }>;
};