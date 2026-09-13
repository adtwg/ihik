ALTER TABLE olt_onus
    DROP COLUMN IF EXISTS in_octets,
    DROP COLUMN IF EXISTS out_octets,
    DROP COLUMN IF EXISTS sampled_at,
    DROP COLUMN IF EXISTS distance_m,
    DROP COLUMN IF EXISTS ip_address,
    DROP COLUMN IF EXISTS in_bps,
    DROP COLUMN IF EXISTS out_bps;
