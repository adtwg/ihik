ALTER TABLE olt_onus
    ADD COLUMN IF NOT EXISTS in_octets bigint,
    ADD COLUMN IF NOT EXISTS out_octets bigint,
    ADD COLUMN IF NOT EXISTS sampled_at timestamptz,
    ADD COLUMN IF NOT EXISTS distance_m double precision,
    ADD COLUMN IF NOT EXISTS ip_address text,
    ADD COLUMN IF NOT EXISTS in_bps double precision,
    ADD COLUMN IF NOT EXISTS out_bps double precision;
