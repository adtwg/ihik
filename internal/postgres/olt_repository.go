package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"isp-billing/internal/olt"
	"isp-billing/internal/zte"
)

type OLTRepository struct {
	pool *pgxpool.Pool
}

func NewOLTRepository(pool *pgxpool.Pool) *OLTRepository {
	return &OLTRepository{pool: pool}
}

const oltSelectColumns = `
	id::text, name, host, port, model, snmp_mode, v3_username,
	v3_auth_protocol, v3_priv_protocol,
	COALESCE(cli_protocol,'ssh'), cli_port, COALESCE(cli_username,''),
	(community_ciphertext IS NOT NULL OR v3_auth_passphrase_ciphertext IS NOT NULL),
	last_connected_at::text, COALESCE(last_error, '')
`

func scanOLT(row pgx.Row) (olt.OLT, error) {
	var item olt.OLT
	err := row.Scan(
		&item.ID, &item.Name, &item.Host, &item.Port, &item.Model, &item.SNMPMode,
		&item.V3Username, &item.V3AuthProto, &item.V3PrivProto,
		&item.CLIProtocol, &item.CLIPort, &item.CLIUsername,
		&item.HasSecret, &item.LastContact, &item.LastError,
	)
	return item, err
}

func (repository *OLTRepository) Create(ctx context.Context, tenantID string, input olt.SaveInput, secrets map[string][]byte) (olt.OLT, error) {
	if input.Port == 0 {
		input.Port = 161
	}
	if input.Model == "" {
		input.Model = "ZTE-C320"
	}
	if input.SNMPMode == "" {
		input.SNMPMode = "v3"
	}
	if input.V3AuthProtocol == "" {
		input.V3AuthProtocol = "sha"
	}
	if input.V3PrivProtocol == "" {
		input.V3PrivProtocol = "aes"
	}
	var id string
	cliProtocol := input.CLIProtocol
	if cliProtocol == "" {
		cliProtocol = "ssh"
	}
	cliPort := input.CLIPort
	if cliPort == 0 {
		if cliProtocol == "telnet" {
			cliPort = 23
		} else {
			cliPort = 22
		}
	}
	err := repository.pool.QueryRow(ctx, `
		INSERT INTO olts (tenant_id, name, host, port, model, snmp_mode,
			community_ciphertext, v3_username, v3_auth_protocol, v3_auth_passphrase_ciphertext,
			v3_priv_protocol, v3_priv_passphrase_ciphertext,
			cli_protocol, cli_port, cli_username, cli_password_ciphertext, cli_enable_password_ciphertext)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
		RETURNING id::text
	`, tenantID, input.Name, input.Host, input.Port, input.Model, input.SNMPMode,
		secrets["community"], input.V3Username, input.V3AuthProtocol, secrets["auth"],
		input.V3PrivProtocol, secrets["priv"],
		cliProtocol, cliPort, input.CLIUsername, secrets["cli_password"], secrets["cli_enable_password"]).Scan(&id)
	if err != nil {
		return olt.OLT{}, fmt.Errorf("create olt: %w", err)
	}
	return repository.Get(ctx, tenantID, id)
}

func (repository *OLTRepository) Update(ctx context.Context, tenantID, id string, input olt.SaveInput, secrets map[string][]byte) (olt.OLT, error) {
	commandTag, err := repository.pool.Exec(ctx, `
		UPDATE olts SET
			name=$3, host=$4, port=$5, model=$6, snmp_mode=$7,
			v3_username=COALESCE(NULLIF($8,''), v3_username),
			v3_auth_protocol=COALESCE(NULLIF($9,''), v3_auth_protocol, 'sha'),
			v3_priv_protocol=COALESCE(NULLIF($10,''), v3_priv_protocol, 'aes'),
			community_ciphertext=COALESCE(NULLIF($11, '\x'::bytea), community_ciphertext),
			v3_auth_passphrase_ciphertext=COALESCE(NULLIF($12, '\x'::bytea), v3_auth_passphrase_ciphertext),
			v3_priv_passphrase_ciphertext=COALESCE(NULLIF($13, '\x'::bytea), v3_priv_passphrase_ciphertext),
			cli_protocol=COALESCE(NULLIF($14,''), cli_protocol, 'ssh'),
			cli_port=CASE WHEN $15=0 THEN cli_port ELSE $15 END,
			cli_username=COALESCE(NULLIF($16,''), cli_username),
			cli_password_ciphertext=COALESCE(NULLIF($17, '\x'::bytea), cli_password_ciphertext),
			cli_enable_password_ciphertext=COALESCE(NULLIF($18, '\x'::bytea), cli_enable_password_ciphertext),
			updated_at=now()
		WHERE tenant_id=$1 AND id=$2 AND archived_at IS NULL
	`, tenantID, id, input.Name, input.Host, orDefault(input.Port, 161), orDefaultStr(input.Model, "ZTE-C320"),
		orDefaultStr(input.SNMPMode, "v3"), input.V3Username, input.V3AuthProtocol, input.V3PrivProtocol,
		secrets["community"], secrets["auth"], secrets["priv"],
		input.CLIProtocol, input.CLIPort, input.CLIUsername, secrets["cli_password"], secrets["cli_enable_password"])
	if err != nil {
		return olt.OLT{}, fmt.Errorf("update olt: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return olt.OLT{}, olt.ErrNotFound
	}
	return repository.Get(ctx, tenantID, id)
}

func (repository *OLTRepository) Get(ctx context.Context, tenantID, id string) (olt.OLT, error) {
	item, err := scanOLT(repository.pool.QueryRow(ctx, `
		SELECT `+oltSelectColumns+` FROM olts WHERE tenant_id=$1 AND id=$2 AND archived_at IS NULL
	`, tenantID, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return olt.OLT{}, olt.ErrNotFound
		}
		return olt.OLT{}, err
	}
	return item, nil
}

func (repository *OLTRepository) List(ctx context.Context, tenantID string) ([]olt.OLT, error) {
	rows, err := repository.pool.Query(ctx, `
		SELECT `+oltSelectColumns+` FROM olts
		WHERE tenant_id=$1 AND archived_at IS NULL ORDER BY created_at DESC
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]olt.OLT, 0)
	for rows.Next() {
		item, scanErr := scanOLT(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (repository *OLTRepository) Delete(ctx context.Context, tenantID, id string) error {
	commandTag, err := repository.pool.Exec(ctx, `
		UPDATE olts SET archived_at = now(), updated_at = now()
		WHERE tenant_id=$1 AND id=$2 AND archived_at IS NULL
	`, tenantID, id)
	if err != nil {
		return fmt.Errorf("delete olt: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return olt.ErrNotFound
	}
	return nil
}

// Credentials mengembalikan seluruh parameter koneksi termasuk ciphertext terenkripsi.
func (repository *OLTRepository) Credentials(ctx context.Context, tenantID, id string) (map[string]any, error) {
	var (
		communityCT, authCT, privCT              []byte
		cliPasswordCT, cliEnableCT               []byte
		host, mode, v3User, authProto, privProto string
		cliProtocol, cliUsername                 string
		port, cliPort                            int
	)
	err := repository.pool.QueryRow(ctx, `
		SELECT host, port, snmp_mode, community_ciphertext,
		       v3_username, v3_auth_protocol, v3_auth_passphrase_ciphertext,
		       v3_priv_protocol, v3_priv_passphrase_ciphertext,
		       COALESCE(cli_protocol,'ssh'), cli_port, COALESCE(cli_username,''),
		       cli_password_ciphertext, cli_enable_password_ciphertext
		FROM olts WHERE tenant_id=$1 AND id=$2 AND archived_at IS NULL
	`, tenantID, id).Scan(
		&host, &port, &mode, &communityCT,
		&v3User, &authProto, &authCT,
		&privProto, &privCT,
		&cliProtocol, &cliPort, &cliUsername,
		&cliPasswordCT, &cliEnableCT,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, olt.ErrNotFound
		}
		return nil, err
	}
	raw := map[string]any{
		"host": host, "port": port, "mode": mode, "v3_user": v3User,
		"v3_auth_protocol": authProto, "v3_priv_protocol": privProto,
		"community_ciphertext":           communityCT,
		"v3_auth_ciphertext":             authCT,
		"v3_priv_ciphertext":             privCT,
		"cli_protocol":                   cliProtocol,
		"cli_port":                       cliPort,
		"cli_username":                   cliUsername,
		"cli_password_ciphertext":        cliPasswordCT,
		"cli_enable_password_ciphertext": cliEnableCT,
	}
	return raw, nil
}

func (repository *OLTRepository) RecordStatus(ctx context.Context, tenantID, id, lastError string) error {
	_, err := repository.pool.Exec(ctx, `
		UPDATE olts SET last_connected_at = now(), last_error = $3
		WHERE tenant_id=$1 AND id=$2 AND archived_at IS NULL
	`, tenantID, id, lastError)
	return err
}

func orDefault(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

func orDefaultStr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func (repository *OLTRepository) UpsertONUs(ctx context.Context, tenantID, oltID string, onus []zte.ONU) error {
	if len(onus) == 0 {
		return fmt.Errorf("sync onu dibatalkan: OLT mengembalikan 0 ONU")
	}

	type oldOptical struct {
		rx       float64
		tx       float64
		distance float64
	}
	oldByIndex := map[string]oldOptical{}
	oldRows, err := repository.pool.Query(ctx, `
		SELECT index, rx_power_dbm, tx_power_dbm, COALESCE(distance_m,0)
		FROM olt_onus WHERE tenant_id=$1 AND olt_id=$2
	`, tenantID, oltID)
	if err == nil {
		for oldRows.Next() {
			var idx string
			var rec oldOptical
			if scanErr := oldRows.Scan(&idx, &rec.rx, &rec.tx, &rec.distance); scanErr == nil {
				oldByIndex[idx] = rec
			}
		}
		oldRows.Close()
	}

	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		DELETE FROM olt_onus WHERE tenant_id=$1 AND olt_id=$2
	`, tenantID, oltID); err != nil {
		return fmt.Errorf("delete onu: %w", err)
	}
	for _, onu := range onus {
		var onlineAt *time.Time
		if onu.Status == "working" {
			n := time.Now()
			onlineAt = &n
		}

		rx := onu.RxPowerDBM
		txPower := onu.TxPowerDBM
		distance := onu.DistanceM
		if old, ok := oldByIndex[onu.Index]; ok {
			if rx == 0 && old.rx != 0 {
				rx = old.rx
			}
			if txPower == 0 && old.tx != 0 {
				txPower = old.tx
			}
			if distance == 0 && old.distance != 0 {
				distance = old.distance
			}
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO olt_onus (tenant_id, olt_id, index, onu_number, name, serial_number, description, status, rx_power_dbm, tx_power_dbm, distance_m, online_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
			ON CONFLICT (tenant_id, olt_id, index) DO UPDATE SET
				onu_number=EXCLUDED.onu_number,
				name=EXCLUDED.name,
				serial_number=EXCLUDED.serial_number,
				description=EXCLUDED.description,
				status=EXCLUDED.status,
				rx_power_dbm=EXCLUDED.rx_power_dbm,
				tx_power_dbm=EXCLUDED.tx_power_dbm,
				distance_m=EXCLUDED.distance_m,
				online_at=EXCLUDED.online_at,
				synced_at=now()
		`, tenantID, oltID, onu.Index, onu.ONUNumber, onu.Name, onu.SerialNumber, onu.Description, onu.Status, rx, txPower, distance, onlineAt); err != nil {
			return fmt.Errorf("insert onu %s: %w", onu.Index, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func (repository *OLTRepository) ListONUs(ctx context.Context, tenantID, oltID string) ([]zte.ONU, error) {
	rows, err := repository.pool.Query(ctx, `
		SELECT index, onu_number, name, serial_number, description, status,
			COALESCE(rx_power_dbm,0), COALESCE(tx_power_dbm,0),
			COALESCE(distance_m,0), COALESCE(ip_address,'')
		FROM olt_onus WHERE tenant_id=$1 AND olt_id=$2 ORDER BY index
	`, tenantID, oltID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []zte.ONU
	for rows.Next() {
		var onu zte.ONU
		if err := rows.Scan(&onu.Index, &onu.ONUNumber, &onu.Name, &onu.SerialNumber, &onu.Description, &onu.Status, &onu.RxPowerDBM, &onu.TxPowerDBM, &onu.DistanceM, &onu.IPAddress); err != nil {
			return nil, err
		}
		out = append(out, onu)
	}
	return out, rows.Err()
}

func (repository *OLTRepository) ListONUSPaged(ctx context.Context, tenantID, oltID string, page, pageSize int, search, sort, order, ponFilter string) ([]zte.ONU, int, error) {
	where := "tenant_id=$1 AND olt_id=$2"
	args := []any{tenantID, oltID}
	if search != "" {
		where += " AND (name ILIKE $3 OR serial_number ILIKE $3 OR index ILIKE $3)"
		args = append(args, "%"+search+"%")
	}
	if ponFilter != "" {
		where += fmt.Sprintf(" AND COALESCE(onu_number, index) LIKE $%d", len(args)+1)
		args = append(args, ponFilter+":%")
	}
	countQ := "SELECT count(*) FROM olt_onus WHERE " + where
	var total int
	if err := repository.pool.QueryRow(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	col := "index"
	switch sort {
	case "name", "status", "rx_power_dbm", "tx_power_dbm":
		col = sort
	}
	dir := "ASC"
	if strings.ToLower(order) == "desc" {
		dir = "DESC"
	}
	offset := (page - 1) * pageSize
	q := fmt.Sprintf(`
		SELECT index, onu_number, name, serial_number, description, status,
			COALESCE(rx_power_dbm,0), COALESCE(tx_power_dbm,0),
			COALESCE(distance_m,0), COALESCE(ip_address,''), COALESCE(in_bps,0), COALESCE(out_bps,0)
		FROM olt_onus WHERE %s ORDER BY %s %s LIMIT $%d OFFSET $%d
	`, where, col, dir, len(args)+1, len(args)+2)
	rows, err := repository.pool.Query(ctx, q, append(args, pageSize, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []zte.ONU
	for rows.Next() {
		var onu zte.ONU
		if err := rows.Scan(&onu.Index, &onu.ONUNumber, &onu.Name, &onu.SerialNumber, &onu.Description, &onu.Status, &onu.RxPowerDBM, &onu.TxPowerDBM, &onu.DistanceM, &onu.IPAddress, &onu.InBps, &onu.OutBps); err != nil {
			return nil, 0, err
		}
		out = append(out, onu)
	}
	return out, total, rows.Err()
}

func (repository *OLTRepository) UpdateONUOptical(ctx context.Context, tenantID, oltID, index string, rx, tx, distance float64) error {
	// ONU yang tidak online tidak boleh menyimpan redaman.
	var status string
	_ = repository.pool.QueryRow(ctx, `
		SELECT COALESCE(lower(trim(status)), 'unknown') FROM olt_onus
		WHERE tenant_id=$1 AND olt_id=$2 AND index=$3
	`, tenantID, oltID, index).Scan(&status)
	switch status {
	case "working", "logging", "sync_mib", "online", "ready":
		// keep optical
	default:
		rx = 0
		tx = 0
	}
	_, err := repository.pool.Exec(ctx, `
		UPDATE olt_onus SET
			rx_power_dbm=$4,
			tx_power_dbm=$5,
			distance_m=$6,
			synced_at=now()
		WHERE tenant_id=$1 AND olt_id=$2 AND index=$3
	`, tenantID, oltID, index, rx, tx, distance)
	return err
}

func (repository *OLTRepository) UpdateONUIdentityByRef(ctx context.Context, tenantID, oltID, pon string, onuID int, name, description *string) error {
	if onuID < 1 {
		return nil
	}
	if name == nil && description == nil {
		return nil
	}
	onuNumber := fmt.Sprintf("%s:%d", strings.TrimSpace(pon), onuID)
	var nameVal any
	if name != nil {
		trimmed := strings.TrimSpace(*name)
		if trimmed != "" {
			nameVal = trimmed
		}
	}
	var descVal any
	if description != nil {
		trimmed := strings.TrimSpace(*description)
		if trimmed != "" {
			descVal = trimmed
		}
	}
	_, err := repository.pool.Exec(ctx, `
		UPDATE olt_onus SET
			name = COALESCE($4, name),
			description = COALESCE($5, description),
			synced_at=now()
		WHERE tenant_id=$1 AND olt_id=$2 AND onu_number=$3
	`, tenantID, oltID, onuNumber, nameVal, descVal)
	return err
}

func (repository *OLTRepository) UpdateONUSerialByRef(ctx context.Context, tenantID, oltID, pon string, onuID int, serial *string) error {
	if onuID < 1 || serial == nil {
		return nil
	}
	serialVal := strings.ToUpper(strings.TrimSpace(*serial))
	if serialVal == "" {
		return nil
	}
	onuNumber := fmt.Sprintf("%s:%d", strings.TrimSpace(pon), onuID)
	_, err := repository.pool.Exec(ctx, `
		UPDATE olt_onus SET
			serial_number = $4,
			synced_at=now()
		WHERE tenant_id=$1 AND olt_id=$2 AND onu_number=$3
	`, tenantID, oltID, onuNumber, serialVal)
	return err
}

func (repository *OLTRepository) UpdateONULiveByRef(ctx context.Context, tenantID, oltID, pon string, onuID int, status string, rx, tx, distance float64, name, description *string) error {
	if onuID < 1 {
		return olt.ErrInvalidInput
	}
	onuNumber := fmt.Sprintf("%s:%d", strings.TrimSpace(pon), onuID)
	state := strings.ToLower(strings.TrimSpace(status))
	if state == "" {
		state = "unknown"
	}
	// ONU yang tidak online tidak boleh menyimpan redaman.
	switch state {
	case "working", "logging", "sync_mib", "online", "ready":
		// keep optical
	default:
		rx = 0
		tx = 0
	}
	var nameVal any
	if name != nil {
		trimmed := strings.TrimSpace(*name)
		if trimmed != "" {
			nameVal = trimmed
		}
	}
	var descVal any
	if description != nil {
		trimmed := strings.TrimSpace(*description)
		if trimmed != "" {
			descVal = trimmed
		}
	}
	tag, err := repository.pool.Exec(ctx, `
		UPDATE olt_onus SET
			status=$4,
			rx_power_dbm=$5,
			tx_power_dbm=$6,
			distance_m=$7,
			name = COALESCE($8, name),
			description = COALESCE($9, description),
			synced_at=now()
		WHERE tenant_id=$1 AND olt_id=$2 AND onu_number=$3
	`, tenantID, oltID, onuNumber, state, rx, tx, distance, nameVal, descVal)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return olt.ErrNotFound
	}
	return nil
}

// FindONUIndexByRef mengembalikan index SNMP asli (mis. "285278465.1")
// untuk satu ONU berdasarkan pon:onuID. Kosong bila belum tersinkron.
func (repository *OLTRepository) FindONUIndexByRef(ctx context.Context, tenantID, oltID, pon string, onuID int) (string, error) {
	if onuID < 1 {
		return "", olt.ErrInvalidInput
	}
	onuNumber := fmt.Sprintf("%s:%d", strings.TrimSpace(pon), onuID)
	var index string
	err := repository.pool.QueryRow(ctx, `
		SELECT index FROM olt_onus
		WHERE tenant_id=$1 AND olt_id=$2 AND onu_number=$3
	`, tenantID, oltID, onuNumber).Scan(&index)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(index), nil
}

func (repository *OLTRepository) SaveONUDetailCache(ctx context.Context, tenantID, oltID, index string, detail *olt.ONUConfigDetail) error {
	if index == "" {
		return nil
	}
	buf, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("marshal detail cache: %w", err)
	}
	_, err = repository.pool.Exec(ctx, `
		UPDATE olt_onus SET
			detail_config_cache=$4,
			detail_config_cached_at=now()
		WHERE tenant_id=$1 AND olt_id=$2 AND index=$3
	`, tenantID, oltID, index, buf)
	return err
}

func (repository *OLTRepository) LoadONUDetailCache(ctx context.Context, tenantID, oltID, index string) (*olt.ONUConfigDetail, time.Time, error) {
	var raw []byte
	var at time.Time
	err := repository.pool.QueryRow(ctx, `
		SELECT detail_config_cache, COALESCE(detail_config_cached_at, '1970-01-01'::timestamptz)
		FROM olt_onus
		WHERE tenant_id=$1 AND olt_id=$2 AND index=$3
	`, tenantID, oltID, index).Scan(&raw, &at)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, time.Time{}, nil
		}
		return nil, time.Time{}, err
	}
	if len(raw) == 0 {
		return nil, at, nil
	}
	var detail olt.ONUConfigDetail
	if err := json.Unmarshal(raw, &detail); err != nil {
		return nil, at, fmt.Errorf("unmarshal detail cache: %w", err)
	}
	return &detail, at, nil
}

func (repository *OLTRepository) LoadTrafficCounters(ctx context.Context, tenantID, oltID string) (map[string]olt.OnuCounterRow, error) {
	rows, err := repository.pool.Query(ctx, `
		SELECT index, COALESCE(in_octets,0), COALESCE(out_octets,0), sampled_at
		FROM olt_onus WHERE tenant_id=$1 AND olt_id=$2
	`, tenantID, oltID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]olt.OnuCounterRow{}
	for rows.Next() {
		var idx string
		var row olt.OnuCounterRow
		if err := rows.Scan(&idx, &row.InOctets, &row.OutOctets, &row.SampledAt); err != nil {
			return nil, err
		}
		out[idx] = row
	}
	return out, rows.Err()
}

func (repository *OLTRepository) UpdateTraffic(ctx context.Context, tenantID, oltID string, samples map[string]zte.TrafficSample) (int, error) {
	now := time.Now()
	updated := 0
	for idx, sample := range samples {
		var prev InOltCounter
		_ = repository.pool.QueryRow(ctx, `
			SELECT in_octets, out_octets, sampled_at FROM olt_onus
			WHERE tenant_id=$1 AND olt_id=$2 AND index=$3
		`, tenantID, oltID, idx).Scan(&prev.InOctets, &prev.OutOctets, &prev.SampledAt)
		var inBps, outBps float64
		if !prev.SampledAt.IsZero() {
			secs := now.Sub(prev.SampledAt).Seconds()
			if secs > 0 {
				inBps = deltaBps(prev.InOctets, sample.InOctets, secs)
				outBps = deltaBps(prev.OutOctets, sample.OutOctets, secs)
			}
		}
		tag, err := repository.pool.Exec(ctx, `
			UPDATE olt_onus SET
				in_octets=$4, out_octets=$5, sampled_at=$6,
				in_bps=$7, out_bps=$8,
				synced_at=now()
			WHERE tenant_id=$1 AND olt_id=$2 AND index=$3
		`, tenantID, oltID, idx, sample.InOctets, sample.OutOctets, now, inBps, outBps)
		if err != nil {
			return updated, err
		}
		updated += int(tag.RowsAffected())
	}
	return updated, nil
}

type InOltCounter struct {
	InOctets  uint64
	OutOctets uint64
	SampledAt time.Time
}

func deltaBps(prev, current uint64, secs float64) float64 {
	if current >= prev {
		return float64(current-prev) * 8 / secs
	}
	return float64(^uint64(0)-prev+current+1) * 8 / secs
}
