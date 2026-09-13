ALTER TABLE olt_onus
    DROP COLUMN IF EXISTS detail_config_cache,
    DROP COLUMN IF EXISTS detail_config_cached_at;
