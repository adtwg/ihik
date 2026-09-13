-- Sampel bps ONU (diisi setiap tick ~5 dtk saat Trafik Live aktif) untuk
-- grafik intraday naik-turun. Retensi 3 hari, dibersihkan berkala oleh api.
CREATE TABLE IF NOT EXISTS olt_onu_bps_samples (
    tenant_id uuid NOT NULL,
    olt_id uuid NOT NULL,
    onu_index text NOT NULL,
    t timestamptz NOT NULL,
    in_bps double precision NOT NULL DEFAULT 0,
    out_bps double precision NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, olt_id, onu_index, t),
    FOREIGN KEY (tenant_id, olt_id) REFERENCES olts (tenant_id, id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_onu_bps_samples_chart
    ON olt_onu_bps_samples (tenant_id, olt_id, onu_index, t DESC);

CREATE INDEX IF NOT EXISTS idx_onu_bps_samples_age
    ON olt_onu_bps_samples (t);
