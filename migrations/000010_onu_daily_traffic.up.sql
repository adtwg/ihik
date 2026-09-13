-- Riwayat trafik harian per ONU (untuk grafik & laporan bandwidth)
CREATE TABLE olt_onu_daily (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    olt_id uuid NOT NULL,
    onu_index text NOT NULL,
    day date NOT NULL,
    in_octets_start bigint NOT NULL DEFAULT 0,
    in_octets_end bigint NOT NULL DEFAULT 0,
    out_octets_start bigint NOT NULL DEFAULT 0,
    out_octets_end bigint NOT NULL DEFAULT 0,
    in_bps_peak double precision NOT NULL DEFAULT 0,
    out_bps_peak double precision NOT NULL DEFAULT 0,
    in_bytes_total bigint NOT NULL DEFAULT 0,
    out_bytes_total bigint NOT NULL DEFAULT 0,
    samples integer NOT NULL DEFAULT 1,
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, olt_id, onu_index, day),
    FOREIGN KEY (tenant_id, olt_id) REFERENCES olts (tenant_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_onu_daily_chart ON olt_onu_daily (tenant_id, olt_id, onu_index, day DESC);
CREATE INDEX idx_onu_daily_day ON olt_onu_daily (day);
