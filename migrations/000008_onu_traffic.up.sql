ALTER TABLE olt_onus
    ADD COLUMN IF NOT EXISTS model text,
    ADD COLUMN IF NOT EXISTS ip_address text,
    ADD COLUMN IF NOT EXISTS distance_m double precision,
    ADD COLUMN IF NOT EXISTS in_octets bigint,
    ADD COLUMN IF NOT EXISTS out_octets bigint,
    ADD COLUMN IF NOT EXISTS in_bps double precision NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS out_bps double precision NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS sampled_at timestamptz;

CREATE INDEX IF NOT EXISTS idx_olt_onus_lookup ON olt_onus (tenant_id, olt_id, index);
