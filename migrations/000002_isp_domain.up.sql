CREATE TYPE service_status AS ENUM (
    'pending_provisioning',
    'active',
    'isolated',
    'provisioning_failed',
    'archived'
);

CREATE TYPE sync_status AS ENUM (
    'linked',
    'pending_create',
    'conflict',
    'missing_on_router',
    'archived',
    'error'
);

ALTER TABLE users
    ADD CONSTRAINT users_tenant_id_id_unique UNIQUE (tenant_id, id);

CREATE TABLE tenant_counters (
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    counter_key text NOT NULL,
    counter_value bigint NOT NULL DEFAULT 0 CHECK (counter_value >= 0),
    PRIMARY KEY (tenant_id, counter_key)
);

CREATE TABLE customers (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    customer_number text NOT NULL,
    name text NOT NULL,
    phone text,
    email text,
    address text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz,
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, customer_number)
);

CREATE INDEX customers_tenant_name_idx
    ON customers (tenant_id, lower(name))
    WHERE archived_at IS NULL;

CREATE TABLE routers (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    name text NOT NULL,
    site text,
    host inet NOT NULL,
    api_port integer NOT NULL DEFAULT 8729 CHECK (api_port BETWEEN 1 AND 65535),
    use_tls boolean NOT NULL DEFAULT true,
    api_username text NOT NULL,
    api_password_ciphertext bytea NOT NULL,
    encryption_key_version smallint NOT NULL,
    routeros_version text,
    router_identity text,
    active boolean NOT NULL DEFAULT true,
    last_connected_at timestamptz,
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz,
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, name)
);

CREATE TABLE ppp_profiles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    router_id uuid NOT NULL,
    external_id text,
    name text NOT NULL,
    rate_limit text,
    raw_fingerprint text NOT NULL,
    last_synced_at timestamptz NOT NULL,
    archived_at timestamptz,
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, router_id, id),
    UNIQUE (tenant_id, router_id, name),
    FOREIGN KEY (tenant_id, router_id) REFERENCES routers (tenant_id, id)
);

CREATE TABLE packages (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    code text NOT NULL,
    name text NOT NULL,
    billing_period_months smallint NOT NULL DEFAULT 1 CHECK (billing_period_months > 0),
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz,
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, code)
);

CREATE TABLE package_prices (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    package_id uuid NOT NULL,
    amount numeric(19, 2) NOT NULL CHECK (amount >= 0),
    tax_percent numeric(7, 4) NOT NULL DEFAULT 0 CHECK (tax_percent BETWEEN 0 AND 100),
    valid_from date NOT NULL,
    valid_until date,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, package_id, valid_from),
    CHECK (valid_until IS NULL OR valid_until >= valid_from),
    FOREIGN KEY (tenant_id, package_id) REFERENCES packages (tenant_id, id)
);

CREATE TABLE package_router_profiles (
    tenant_id uuid NOT NULL,
    package_id uuid NOT NULL,
    router_id uuid NOT NULL,
    normal_profile_id uuid NOT NULL,
    isolated_profile_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, package_id, router_id),
    FOREIGN KEY (tenant_id, package_id) REFERENCES packages (tenant_id, id),
    FOREIGN KEY (tenant_id, router_id) REFERENCES routers (tenant_id, id),
    FOREIGN KEY (tenant_id, router_id, normal_profile_id)
        REFERENCES ppp_profiles (tenant_id, router_id, id),
    FOREIGN KEY (tenant_id, router_id, isolated_profile_id)
        REFERENCES ppp_profiles (tenant_id, router_id, id),
    CHECK (normal_profile_id <> isolated_profile_id)
);

CREATE TABLE services (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    customer_id uuid NOT NULL,
    router_id uuid NOT NULL,
    package_id uuid NOT NULL,
    service_number text NOT NULL,
    status service_status NOT NULL DEFAULT 'pending_provisioning',
    activated_at timestamptz,
    isolated_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz,
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, service_number),
    FOREIGN KEY (tenant_id, customer_id) REFERENCES customers (tenant_id, id),
    FOREIGN KEY (tenant_id, router_id) REFERENCES routers (tenant_id, id),
    FOREIGN KEY (tenant_id, package_id) REFERENCES packages (tenant_id, id)
);

CREATE TABLE pppoe_accounts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    service_id uuid NOT NULL,
    router_id uuid NOT NULL,
    external_id text,
    username text NOT NULL,
    password_ciphertext bytea NOT NULL,
    encryption_key_version smallint NOT NULL,
    sync_status sync_status NOT NULL DEFAULT 'pending_create',
    raw_fingerprint text,
    last_synced_at timestamptz,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz,
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, service_id),
    UNIQUE (tenant_id, router_id, username),
    FOREIGN KEY (tenant_id, service_id) REFERENCES services (tenant_id, id),
    FOREIGN KEY (tenant_id, router_id) REFERENCES routers (tenant_id, id)
);

CREATE TABLE sync_jobs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    router_id uuid NOT NULL,
    status text NOT NULL CHECK (status IN ('queued', 'running', 'review', 'completed', 'failed')),
    started_at timestamptz,
    completed_at timestamptz,
    summary jsonb NOT NULL DEFAULT '{}'::jsonb,
    error_message text,
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, router_id) REFERENCES routers (tenant_id, id),
    FOREIGN KEY (tenant_id, created_by) REFERENCES users (tenant_id, id)
);

CREATE TABLE sync_conflicts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    sync_job_id uuid NOT NULL,
    pppoe_account_id uuid,
    router_external_id text,
    conflict_type text NOT NULL,
    billing_snapshot jsonb,
    router_snapshot jsonb,
    resolution text CHECK (resolution IN ('use_billing', 'use_router', 'link', 'ignore', 'archive')),
    resolved_by uuid,
    resolved_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (tenant_id, sync_job_id) REFERENCES sync_jobs (tenant_id, id),
    FOREIGN KEY (tenant_id, pppoe_account_id) REFERENCES pppoe_accounts (tenant_id, id),
    FOREIGN KEY (tenant_id, resolved_by) REFERENCES users (tenant_id, id)
);

CREATE TABLE provisioning_commands (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    router_id uuid NOT NULL,
    pppoe_account_id uuid NOT NULL,
    command_type text NOT NULL CHECK (command_type IN ('create', 'update', 'disable', 'isolate', 'restore', 'reset_password')),
    idempotency_key text NOT NULL,
    payload_ciphertext bytea NOT NULL,
    encryption_key_version smallint NOT NULL,
    status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'processing', 'succeeded', 'failed', 'cancelled')),
    attempt_count integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    UNIQUE (tenant_id, idempotency_key),
    FOREIGN KEY (tenant_id, router_id) REFERENCES routers (tenant_id, id),
    FOREIGN KEY (tenant_id, pppoe_account_id) REFERENCES pppoe_accounts (tenant_id, id)
);

CREATE INDEX provisioning_commands_worker_idx
    ON provisioning_commands (status, next_attempt_at)
    WHERE status IN ('queued', 'failed');

CREATE TABLE invoices (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    customer_id uuid NOT NULL,
    service_id uuid NOT NULL,
    invoice_number text NOT NULL,
    period_start date NOT NULL,
    period_end date NOT NULL,
    issued_at timestamptz NOT NULL,
    due_at timestamptz NOT NULL,
    status text NOT NULL CHECK (status IN ('unpaid', 'partial', 'paid', 'overdue', 'void')),
    subtotal numeric(19, 2) NOT NULL CHECK (subtotal >= 0),
    tax_amount numeric(19, 2) NOT NULL CHECK (tax_amount >= 0),
    total numeric(19, 2) NOT NULL CHECK (total >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, invoice_number),
    UNIQUE (tenant_id, service_id, period_start, period_end),
    CHECK (period_end >= period_start),
    CHECK (total = subtotal + tax_amount),
    FOREIGN KEY (tenant_id, customer_id) REFERENCES customers (tenant_id, id),
    FOREIGN KEY (tenant_id, service_id) REFERENCES services (tenant_id, id)
);

CREATE TABLE invoice_lines (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    invoice_id uuid NOT NULL,
    item_code text NOT NULL,
    description text NOT NULL,
    quantity numeric(12, 4) NOT NULL CHECK (quantity > 0),
    unit_price numeric(19, 2) NOT NULL CHECK (unit_price >= 0),
    tax_percent numeric(7, 4) NOT NULL CHECK (tax_percent BETWEEN 0 AND 100),
    line_subtotal numeric(19, 2) NOT NULL CHECK (line_subtotal >= 0),
    line_tax numeric(19, 2) NOT NULL CHECK (line_tax >= 0),
    line_total numeric(19, 2) NOT NULL CHECK (line_total >= 0),
    FOREIGN KEY (tenant_id, invoice_id) REFERENCES invoices (tenant_id, id),
    CHECK (line_total = line_subtotal + line_tax)
);

CREATE TABLE cash_shifts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    cashier_user_id uuid NOT NULL,
    opened_at timestamptz NOT NULL DEFAULT now(),
    closed_at timestamptz,
    opening_amount numeric(19, 2) NOT NULL DEFAULT 0 CHECK (opening_amount >= 0),
    closing_amount numeric(19, 2) CHECK (closing_amount >= 0),
    status text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'closed')),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, cashier_user_id) REFERENCES users (tenant_id, id)
);

CREATE UNIQUE INDEX cash_shifts_one_open_per_cashier_idx
    ON cash_shifts (tenant_id, cashier_user_id)
    WHERE status = 'open';

CREATE TABLE payments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    cash_shift_id uuid,
    payment_number text NOT NULL,
    amount numeric(19, 2) NOT NULL CHECK (amount > 0),
    method text NOT NULL CHECK (method IN ('cash', 'manual_transfer')),
    status text NOT NULL DEFAULT 'posted' CHECK (status IN ('posted', 'voided', 'refunded')),
    received_by uuid NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now(),
    void_reason text,
    voided_by uuid,
    voided_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, payment_number),
    FOREIGN KEY (tenant_id, cash_shift_id) REFERENCES cash_shifts (tenant_id, id),
    FOREIGN KEY (tenant_id, received_by) REFERENCES users (tenant_id, id),
    FOREIGN KEY (tenant_id, voided_by) REFERENCES users (tenant_id, id)
);

CREATE TABLE payment_allocations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    payment_id uuid NOT NULL,
    invoice_id uuid NOT NULL,
    amount numeric(19, 2) NOT NULL CHECK (amount > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, payment_id, invoice_id),
    FOREIGN KEY (tenant_id, payment_id) REFERENCES payments (tenant_id, id),
    FOREIGN KEY (tenant_id, invoice_id) REFERENCES invoices (tenant_id, id)
);

CREATE TABLE receipts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    payment_id uuid NOT NULL,
    receipt_number text NOT NULL,
    issued_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, payment_id),
    UNIQUE (tenant_id, receipt_number),
    FOREIGN KEY (tenant_id, payment_id) REFERENCES payments (tenant_id, id)
);

CREATE OR REPLACE FUNCTION prevent_financial_delete() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'financial records cannot be deleted; use void or refund';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER invoices_no_delete BEFORE DELETE ON invoices
    FOR EACH ROW EXECUTE FUNCTION prevent_financial_delete();
CREATE TRIGGER invoice_lines_no_delete BEFORE DELETE ON invoice_lines
    FOR EACH ROW EXECUTE FUNCTION prevent_financial_delete();
CREATE TRIGGER payments_no_delete BEFORE DELETE ON payments
    FOR EACH ROW EXECUTE FUNCTION prevent_financial_delete();
CREATE TRIGGER payment_allocations_no_delete BEFORE DELETE ON payment_allocations
    FOR EACH ROW EXECUTE FUNCTION prevent_financial_delete();
CREATE TRIGGER receipts_no_delete BEFORE DELETE ON receipts
    FOR EACH ROW EXECUTE FUNCTION prevent_financial_delete();