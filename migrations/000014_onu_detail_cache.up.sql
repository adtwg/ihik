ALTER TABLE olt_onus
    ADD COLUMN IF NOT EXISTS detail_config_cache jsonb,
    ADD COLUMN IF NOT EXISTS detail_config_cached_at timestamptz;

COMMENT ON COLUMN olt_onus.detail_config_cache IS 'Cache detail konfigurasi ONU dari CLI (VLAN, T-CONT, GEM-Port, Service-Port, WAN IP).';
