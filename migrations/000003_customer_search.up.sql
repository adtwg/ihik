CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX customers_number_search_idx
    ON customers (tenant_id, customer_number text_pattern_ops);
CREATE INDEX customers_name_trgm_idx
    ON customers USING gin (name gin_trgm_ops);
CREATE INDEX customers_phone_trgm_idx
    ON customers USING gin (phone gin_trgm_ops);
CREATE INDEX customers_email_trgm_idx
    ON customers USING gin (email gin_trgm_ops);
CREATE INDEX customers_tenant_created_stable_idx
    ON customers (tenant_id, created_at DESC, id);