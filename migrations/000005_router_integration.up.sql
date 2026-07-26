-- 000005_router_integration.up.sql
-- Integrasi MikroTik: izinkan hostname (bukan hanya IP) untuk alamat router
-- dan satu router utama per tenant.

ALTER TABLE routers ALTER COLUMN host TYPE text USING host::text;

CREATE UNIQUE INDEX IF NOT EXISTS routers_one_default_per_tenant_idx
    ON routers (tenant_id)
    WHERE archived_at IS NULL;

CREATE INDEX IF NOT EXISTS pppoe_accounts_tenant_service_idx
    ON pppoe_accounts (tenant_id, service_id)
    WHERE archived_at IS NULL;
