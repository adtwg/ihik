package postgres

import (
	"context"

	"isp-billing/internal/olt"
)

// GetDaily mengembalikan riwayat harian trafik satu ONU.
func (repository *OLTRepository) GetDaily(ctx context.Context, tenantID, oltID, index string, days int) ([]olt.OnuDailyRow, error) {
	rows, err := repository.pool.Query(ctx, `
		SELECT day::text, in_bps_peak, out_bps_peak, in_bytes_total, out_bytes_total, samples
		FROM olt_onu_daily
		WHERE tenant_id=$1 AND olt_id=$2 AND onu_index=$3
		  AND day >= CURRENT_DATE - $4::int
		ORDER BY day ASC
	`, tenantID, oltID, index, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]olt.OnuDailyRow, 0, days)
	for rows.Next() {
		var row olt.OnuDailyRow
		if err := rows.Scan(&row.Day, &row.InBpsPeak, &row.OutBpsPeak,
			&row.InBytesTot, &row.OutBytesTot, &row.Samples); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// UpsertDaily mencatat peak bps hari ini untuk satu ONU.
// Dipanggil setiap sampling realtime; peak diambil max(existing, new).
func (repository *OLTRepository) UpsertDaily(ctx context.Context, tenantID, oltID, index string, inBps, outBps float64) error {
	_, err := repository.pool.Exec(ctx, `
		INSERT INTO olt_onu_daily (tenant_id, olt_id, onu_index, day,
			in_bps_peak, out_bps_peak, samples, updated_at)
		VALUES ($1,$2,$3,CURRENT_DATE,$4,$5,1, now())
		ON CONFLICT (tenant_id, olt_id, onu_index, day) DO UPDATE SET
			in_bps_peak  = GREATEST(olt_onu_daily.in_bps_peak, EXCLUDED.in_bps_peak),
			out_bps_peak = GREATEST(olt_onu_daily.out_bps_peak, EXCLUDED.out_bps_peak),
			samples      = olt_onu_daily.samples + 1,
			updated_at   = now()
	`, tenantID, oltID, index, inBps, outBps)
	return err
}

// GetLatestBps membaca bps terakhir dari cache olt_onus.
func (repository *OLTRepository) GetLatestBps(ctx context.Context, tenantID, oltID, index string) (olt.BpsRow, error) {
	var row olt.BpsRow
	err := repository.pool.QueryRow(ctx, `
		SELECT COALESCE(in_bps,0), COALESCE(out_bps,0)
		FROM olt_onus WHERE tenant_id=$1 AND olt_id=$2 AND index=$3
	`, tenantID, oltID, index).Scan(&row.InBps, &row.OutBps)
	return row, err
}

// SaveBpsSamples menyimpan satu baris sampel bps per ONU untuk grafik
// intraday. Dipanggil setiap tick Trafik Live.
func (repository *OLTRepository) SaveBpsSamples(ctx context.Context, tenantID, oltID string, samples map[string]olt.BpsRow) error {
	for index, row := range samples {
		if _, err := repository.pool.Exec(ctx, `
			INSERT INTO olt_onu_bps_samples (tenant_id, olt_id, onu_index, t, in_bps, out_bps)
			VALUES ($1,$2,$3, now(), $4, $5)
			ON CONFLICT (tenant_id, olt_id, onu_index, t) DO NOTHING
		`, tenantID, oltID, index, row.InBps, row.OutBps); err != nil {
			return err
		}
	}
	// Retensi 3 hari (dianggap murah: satu DELETE per tick).
	_, _ = repository.pool.Exec(ctx, `DELETE FROM olt_onu_bps_samples WHERE t < now() - interval '3 days'`)
	return nil
}

// GetIntraday mengembalikan sampel bps satu ONU (default 180 menit terakhir)
// untuk grafik naik-turun.
func (repository *OLTRepository) GetIntraday(ctx context.Context, tenantID, oltID, index string, minutes int) ([]olt.BpsSampleRow, error) {
	if minutes < 5 {
		minutes = 180
	}
	rows, err := repository.pool.Query(ctx, `
		SELECT floor(extract(epoch from t)/60)::bigint*60, in_bps, out_bps
		FROM olt_onu_bps_samples
		WHERE tenant_id=$1 AND olt_id=$2 AND onu_index=$3
		  AND t >= now() - ($4::int * interval '1 minute')
		ORDER BY t ASC
	`, tenantID, oltID, index, minutes)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]olt.BpsSampleRow, 0, 64)
	for rows.Next() {
		var row olt.BpsSampleRow
		if err := rows.Scan(&row.T, &row.InBps, &row.OutBps); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// GetStats: ringkasan jumlah ONU per status + daftar PON port.
func (repository *OLTRepository) GetStats(ctx context.Context, tenantID, oltID string) (olt.OnuStats, error) {
	var stats olt.OnuStats
	err := repository.pool.QueryRow(ctx, `
		SELECT count(*),
		       count(*) FILTER (WHERE status='working'),
		       count(*) FILTER (WHERE status='los'),
		       count(*) FILTER (WHERE status IN ('offlined','dying_gasp')),
		       count(*) FILTER (WHERE status IN ('logging','sync_mib','auth_failed','unknown'))
		FROM olt_onus WHERE tenant_id=$1 AND olt_id=$2
	`, tenantID, oltID).Scan(&stats.Total, &stats.Working, &stats.Los, &stats.Offline, &stats.Other)
	if err != nil {
		return stats, err
	}

	// PON port diambil dari prefix onu_number "pon:onuid".
	rows, err := repository.pool.Query(ctx, `
		SELECT split_part(COALESCE(NULLIF(onu_number,''), index), ':', 1) AS pon,
		       count(*)
		FROM olt_onus WHERE tenant_id=$1 AND olt_id=$2
		GROUP BY 1 ORDER BY 1
	`, tenantID, oltID)
	if err != nil {
		return stats, err
	}
	defer rows.Close()
	stats.PonPorts = make([]olt.PonPortStat, 0)
	for rows.Next() {
		var port olt.PonPortStat
		if err := rows.Scan(&port.Port, &port.Count); err != nil {
			return stats, err
		}
		stats.PonPorts = append(stats.PonPorts, port)
	}
	return stats, rows.Err()
}
