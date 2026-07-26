-- 000004_billing_core.up.sql
-- Billing inti tanpa integrasi router: layanan boleh berdiri tanpa router
-- (integrasi MikroTik/PPPoE menyusul pada fase berikutnya).

ALTER TABLE services ALTER COLUMN router_id DROP NOT NULL;

CREATE INDEX IF NOT EXISTS packages_tenant_created_idx
    ON packages (tenant_id, created_at DESC, id);
CREATE INDEX IF NOT EXISTS services_tenant_created_idx
    ON services (tenant_id, created_at DESC, id);
CREATE INDEX IF NOT EXISTS services_tenant_status_idx
    ON services (tenant_id, status)
    WHERE archived_at IS NULL;
CREATE INDEX IF NOT EXISTS invoices_tenant_created_idx
    ON invoices (tenant_id, created_at DESC, id);
CREATE INDEX IF NOT EXISTS invoices_tenant_status_idx
    ON invoices (tenant_id, status);
CREATE INDEX IF NOT EXISTS invoices_tenant_service_period_idx
    ON invoices (tenant_id, service_id, period_start);
CREATE INDEX IF NOT EXISTS invoice_lines_tenant_invoice_idx
    ON invoice_lines (tenant_id, invoice_id);
CREATE INDEX IF NOT EXISTS payments_tenant_received_idx
    ON payments (tenant_id, received_at DESC, id);
CREATE INDEX IF NOT EXISTS payment_allocations_tenant_invoice_idx
    ON payment_allocations (tenant_id, invoice_id);
CREATE INDEX IF NOT EXISTS payment_allocations_tenant_payment_idx
    ON payment_allocations (tenant_id, payment_id);
