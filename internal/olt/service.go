package olt

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gosnmp/gosnmp"

	"isp-billing/internal/secretbox"
	"isp-billing/internal/zte"
	"isp-billing/internal/ztecli"
)

var (
	ErrInvalidInput  = errors.New("invalid olt input")
	ErrNotFound      = errors.New("olt not found")
	ErrNoEncryption  = errors.New("encryption key not configured")
	ErrUnreachable   = errors.New("olt unreachable")
	ErrDuplicateName = errors.New("duplicate olt name")

	// Error CLI hybrid (didelegasikan dari paket ztecli agar konsisten).
	ErrLocked       = ztecli.ErrLocked
	ErrForbiddenCmd = ztecli.ErrForbiddenCmd
)

// SaveInput adalah data pembuatan/pembaruan OLT.
type SaveInput struct {
	Name              string `json:"name"`
	Host              string `json:"host"`
	Port              int    `json:"port"`
	Model             string `json:"model"`
	SNMPMode          string `json:"snmp_mode"` // v2c | v3
	Community         string `json:"community,omitempty"`
	V3Username        string `json:"v3_username,omitempty"`
	V3AuthProtocol    string `json:"v3_auth_protocol,omitempty"`
	V3AuthPassphrase  string `json:"v3_auth_passphrase,omitempty"`
	V3PrivProtocol    string `json:"v3_priv_protocol,omitempty"`
	V3PrivPassphrase  string `json:"v3_priv_passphrase,omitempty"`
	CLIProtocol       string `json:"cli_protocol,omitempty"` // ssh | telnet
	CLIPort           int    `json:"cli_port,omitempty"`
	CLIUsername       string `json:"cli_username,omitempty"`
	CLIPassword       string `json:"cli_password,omitempty"`
	CLIEnablePassword string `json:"cli_enable_password,omitempty"`
}

// OLT adalah data OLT tanpa kredensial sensitif.
type OLT struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Host        string  `json:"host"`
	Port        int     `json:"port"`
	Model       string  `json:"model"`
	SNMPMode    string  `json:"snmp_mode"`
	V3Username  string  `json:"v3_username,omitempty"`
	V3AuthProto string  `json:"v3_auth_protocol,omitempty"`
	V3PrivProto string  `json:"v3_priv_protocol,omitempty"`
	CLIProtocol string  `json:"cli_protocol,omitempty"`
	CLIPort     int     `json:"cli_port,omitempty"`
	CLIUsername string  `json:"cli_username,omitempty"`
	HasSecret   bool    `json:"has_secret"`
	LastContact *string `json:"last_connected_at,omitempty"`
	LastError   string  `json:"last_error,omitempty"`
}

// CLITestInfo berisi hasil uji koneksi CLI (SSH/Telnet) OLT.
type CLITestInfo struct {
	Protocol     string `json:"protocol"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	Username     string `json:"username"`
	Command      string `json:"command"`
	SampleOutput string `json:"sample_output,omitempty"`
}

// ONUActionResult melaporkan channel aksi yang dipakai (snmp/cli).
type ONUActionResult struct {
	Action   string `json:"action"`
	Method   string `json:"method"`
	Fallback bool   `json:"fallback,omitempty"`
	Message  string `json:"message,omitempty"`
}

type Repository interface {
	Create(ctx context.Context, tenantID string, input SaveInput, secrets map[string][]byte) (OLT, error)
	Update(ctx context.Context, tenantID, id string, input SaveInput, secrets map[string][]byte) (OLT, error)
	Get(ctx context.Context, tenantID, id string) (OLT, error)
	List(ctx context.Context, tenantID string) ([]OLT, error)
	Delete(ctx context.Context, tenantID, id string) error
	Credentials(ctx context.Context, tenantID, id string) (map[string]any, error)
	UpsertONUs(ctx context.Context, tenantID, oltID string, onus []zte.ONU) error
	ListONUs(ctx context.Context, tenantID, oltID string) ([]zte.ONU, error)
	ListONUSPaged(ctx context.Context, tenantID, oltID string, page, pageSize int, search, sort, order, ponFilter string) ([]zte.ONU, int, error)
	LoadTrafficCounters(ctx context.Context, tenantID, oltID string) (map[string]OnuCounterRow, error)
	UpdateTraffic(ctx context.Context, tenantID, oltID string, samples map[string]zte.TrafficSample) (int, error)
	UpdateONUOptical(ctx context.Context, tenantID, oltID, index string, rx, tx, distance float64) error
	UpdateONUIdentityByRef(ctx context.Context, tenantID, oltID, pon string, onuID int, name, description *string) error
	UpdateONUSerialByRef(ctx context.Context, tenantID, oltID, pon string, onuID int, serial *string) error
	UpdateONULiveByRef(ctx context.Context, tenantID, oltID, pon string, onuID int, status string, rx, tx, distance *float64, name, description *string) error
	FindONUIndexByRef(ctx context.Context, tenantID, oltID, pon string, onuID int) (string, error)
	SaveONUDetailCache(ctx context.Context, tenantID, oltID, index string, detail *ONUConfigDetail) error
	LoadONUDetailCache(ctx context.Context, tenantID, oltID, index string) (*ONUConfigDetail, time.Time, error)
	InvalidateONUDetailCache(ctx context.Context, tenantID, oltID, index string) error
	GetDaily(ctx context.Context, tenantID, oltID, index string, days int) ([]OnuDailyRow, error)
	GetStats(ctx context.Context, tenantID, oltID string) (OnuStats, error)
	GetLatestBps(ctx context.Context, tenantID, oltID, index string) (BpsRow, error)
	UpsertDaily(ctx context.Context, tenantID, oltID, index string, inBps, outBps float64) error
	SaveBpsSamples(ctx context.Context, tenantID, oltID string, samples map[string]BpsRow) error
	GetIntraday(ctx context.Context, tenantID, oltID, index string, minutes int) ([]BpsSampleRow, error)
	RecordStatus(ctx context.Context, tenantID, id, lastError string) error
}

// BpsRow: nilai bps terkini satu ONU.
type BpsRow struct {
	InBps  float64
	OutBps float64
}

// BpsSampleRow: satu titik sampel bps untuk grafik intraday.
type BpsSampleRow struct {
	T      int64   `json:"t"` // unix detik (per menit)
	InBps  float64 `json:"in_bps"`
	OutBps float64 `json:"out_bps"`
}

// onuCounterRow adalah counter octets terakhir per ONU dari DB.
type OnuCounterRow struct {
	InOctets  uint64
	OutOctets uint64
	SampledAt *time.Time
}

// TrafficPage adalah hasil query server-side untuk tabel ONU.
type TrafficPage struct {
	Items []zte.ONU `json:"items"`
	Total int       `json:"total"`
	Page  int       `json:"page"`
}

type Service struct {
	repository    Repository
	encryptionKey []byte
	cliManager    *ztecli.Manager
	healthMu      sync.Mutex
	healthCache   map[string]healthCacheEntry
}

type healthCacheEntry struct {
	data *zte.OltHealth
	at   time.Time
}

func NewService(repository Repository, encryptionKey []byte) *Service {
	return &Service{repository: repository, encryptionKey: encryptionKey,
		cliManager: ztecli.NewManager(), healthCache: map[string]healthCacheEntry{}}
}

// decryptSecret mendekripsi bytea secretbox menjadi plaintext.
func decryptSecret(key []byte, ciphertext []byte) (string, error) {
	return secretbox.Decrypt(key, ciphertext)
}

func (service *Service) requireKey() error {
	if len(service.encryptionKey) != 32 {
		return ErrNoEncryption
	}
	return nil
}

func validate(input SaveInput) error {
	input.Name = strings.TrimSpace(input.Name)
	input.Host = strings.TrimSpace(input.Host)
	if input.Name == "" || len(input.Name) > 100 || input.Host == "" || len(input.Host) > 253 {
		return ErrInvalidInput
	}
	if strings.ContainsAny(input.Host, " /@") {
		return ErrInvalidInput
	}
	if input.Port == 0 {
		input.Port = 161
	}
	if input.Port < 1 || input.Port > 65535 {
		return ErrInvalidInput
	}
	mode := strings.ToLower(input.SNMPMode)
	if mode != "v2c" && mode != "v3" {
		return ErrInvalidInput
	}
	if mode == "v2c" && strings.TrimSpace(input.Community) == "" && input.Community == "" {
		// community boleh kosong saat update (artinya tidak diubah); divalidasi di handler create.
	}
	if mode == "v3" && strings.TrimSpace(input.V3Username) == "" {
		return ErrInvalidInput
	}
	return nil
}

func (service *Service) encryptSecrets(input SaveInput) (map[string][]byte, error) {
	secrets := map[string][]byte{}
	encrypt := func(label, plaintext string) error {
		if plaintext == "" {
			return nil
		}
		ciphertext, err := secretbox.Encrypt(service.encryptionKey, plaintext)
		if err != nil {
			return fmt.Errorf("enkripsi %s: %w", label, err)
		}
		secrets[label] = ciphertext
		return nil
	}
	if err := encrypt("community", input.Community); err != nil {
		return nil, err
	}
	if err := encrypt("auth", input.V3AuthPassphrase); err != nil {
		return nil, err
	}
	if err := encrypt("priv", input.V3PrivPassphrase); err != nil {
		return nil, err
	}
	if err := encrypt("cli_password", input.CLIPassword); err != nil {
		return nil, err
	}
	if err := encrypt("cli_enable_password", input.CLIEnablePassword); err != nil {
		return nil, err
	}
	return secrets, nil
}

// CredentialsFor membangun kredensial SNMP terdekripsi untuk sebuah OLT.
func (service *Service) CredentialsFor(ctx context.Context, tenantID, id string) (zte.Credentials, error) {
	if err := service.requireKey(); err != nil {
		return zte.Credentials{}, err
	}
	raw, err := service.repository.Credentials(ctx, tenantID, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return zte.Credentials{}, ErrNotFound
		}
		return zte.Credentials{}, fmt.Errorf("muat kredensial olt: %w", err)
	}
	getStr := func(key string) string {
		value, _ := raw[key].(string)
		return value
	}
	creds := zte.Credentials{
		Host:        getStr("host"),
		Port:        uint16(getInt(raw["port"])),
		Mode:        getStr("mode"),
		Community:   getStr("community"),
		V3User:      getStr("v3_user"),
		V3AuthProto: getStr("v3_auth_protocol"),
		V3PrivProto: getStr("v3_priv_protocol"),
	}
	for label, target := range map[string]*string{
		"community_ciphertext": &creds.Community,
		"v3_auth_ciphertext":   &creds.V3AuthPass,
		"v3_priv_ciphertext":   &creds.V3PrivPass,
	} {
		ciphertext, ok := raw[label].([]byte)
		if !ok || len(ciphertext) == 0 {
			continue
		}
		plaintext, err := secretbox.Decrypt(service.encryptionKey, ciphertext)
		if err != nil {
			return zte.Credentials{}, fmt.Errorf("dekripsi %s: %w", label, err)
		}
		*target = plaintext
	}
	return creds, nil
}

func getInt(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	default:
		return 0
	}
}

// Connect membuka sesi SNMP ke OLT dan menandai status sukses/gagal.
func (service *Service) connect(ctx context.Context, tenantID, id string) (*gosnmp.GoSNMP, func(), error) {
	creds, err := service.CredentialsFor(ctx, tenantID, id)
	if err != nil {
		return nil, nil, err
	}
	session, err := zte.Connect(creds)
	if err != nil {
		_ = service.repository.RecordStatus(ctx, tenantID, id, truncate(err.Error()))
		return nil, nil, fmt.Errorf("%w: %s", ErrUnreachable, err.Error())
	}
	cleanup := func() {
		session.Conn.Close()
		_ = service.repository.RecordStatus(ctx, tenantID, id, "")
	}
	return session, cleanup, nil
}

func truncate(message string) string {
	if len(message) > 500 {
		return message[:500]
	}
	return message
}

// Test memverifikasi koneksi SNMP dan mengembalikan identitas OLT.
func (service *Service) Test(ctx context.Context, tenantID, id string) (zte.SystemInfo, error) {
	session, cleanup, err := service.connect(ctx, tenantID, id)
	if err != nil {
		return zte.SystemInfo{}, err
	}
	defer cleanup()
	info, err := zte.GetSystem(session)
	if err != nil {
		_ = service.repository.RecordStatus(ctx, tenantID, id, truncate(err.Error()))
		return zte.SystemInfo{}, fmt.Errorf("%w: %s", ErrUnreachable, err.Error())
	}
	return info, nil
}

// TestCLI memverifikasi koneksi CLI (SSH/Telnet) dan mencoba satu perintah
// read-only agar operator tahu akses CLI benar-benar hidup.
func (service *Service) TestCLI(ctx context.Context, tenantID, id string) (CLITestInfo, error) {
	creds, err := service.CLICredentialsFor(ctx, tenantID, id)
	if err != nil {
		return CLITestInfo{}, err
	}
	if strings.TrimSpace(creds.Username) == "" || strings.TrimSpace(creds.Password) == "" {
		return CLITestInfo{}, fmt.Errorf("%w: username/password CLI wajib diisi", ErrInvalidInput)
	}

	commands := []string{"show card", "show version", "show gpon onu state"}
	used := commands[0]
	out, err := service.withCLISession(ctx, tenantID, id, func(session *ztecli.Session) (string, error) {
		var firstErr error
		for _, cmd := range commands {
			used = cmd
			text, execErr := session.Execute(ctx, cmd)
			if execErr == nil && strings.TrimSpace(text) != "" {
				return text, nil
			}
			if firstErr == nil && execErr != nil {
				firstErr = execErr
			}
		}
		if firstErr != nil {
			return "", firstErr
		}
		return "", fmt.Errorf("CLI tidak mengembalikan output")
	})
	if err != nil {
		return CLITestInfo{}, err
	}

	sample := strings.TrimSpace(out)
	if len(sample) > 220 {
		sample = sample[:220]
	}

	return CLITestInfo{
		Protocol:     strings.ToLower(strings.TrimSpace(creds.Protocol)),
		Host:         creds.Host,
		Port:         int(creds.Port),
		Username:     creds.Username,
		Command:      used,
		SampleOutput: sample,
	}, nil
}

// SyncONUs membaca seluruh ONU dari OLT lalu menyimpan cache ke database.
func (service *Service) SyncONUs(ctx context.Context, tenantID, id string) (int, error) {
	count, err := service.syncONUsRun(ctx, tenantID, id)
	// Simpan hasil ke last_error agar UI menampilkan alasan gagal/sukses.
	statusCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err != nil {
		_ = service.repository.RecordStatus(statusCtx, tenantID, id, "sync gagal: "+err.Error())
	} else {
		_ = service.repository.RecordStatus(statusCtx, tenantID, id, "")
	}
	return count, err
}

func (service *Service) syncONUsRun(ctx context.Context, tenantID, id string) (int, error) {
	session, cleanup, err := service.connect(ctx, tenantID, id)
	if err != nil {
		return 0, err
	}
	defer cleanup()

	profile, onus, err := service.detectBestProfile(ctx, session)
	if err != nil {
		return 0, err
	}

	// Optional env override: kalau mayoritas online kosong redaman, coba fallback profile lain.
	if os.Getenv("ZTE_FORCE_PROFILE") == "v2.1" {
		profile, onus, _ = service.tryProfile(ctx, session, zte.FirmwareProfiles["v2.1"])
	} else if os.Getenv("ZTE_FORCE_PROFILE") == "v2.2" {
		profile, onus, _ = service.tryProfile(ctx, session, zte.FirmwareProfiles["v2.2"])
	}

	if len(onus) == 0 {
		return 0, fmt.Errorf("%w: tidak ada ONU ditemukan dari SNMP", ErrUnreachable)
	}

	log.Printf("[SyncONUs] olt=%s profile=%s onus=%d online=%d with_optical=%d",
		id, profile.Name, len(onus), countOnline(onus), countWithOptical(onus))
	sampleRawOptical(ctx, session, profile, onus)

	// Context terpisah untuk tulis DB agar tidak kehabisan budget gara-gara
	// walk SNMP yang lama (dulu gagal: "begin tx: context deadline exceeded").
	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancelWrite()
	if err := service.repository.UpsertONUs(writeCtx, tenantID, id, onus); err != nil {
		return 0, fmt.Errorf("simpan cache onu: %w", err)
	}
	_, _ = service.RefreshTraffic(ctx, tenantID, id)
	return len(onus), nil
}

func countOnline(onus []zte.ONU) int {
	c := 0
	for _, o := range onus {
		if statusAllowsOptical(o.Status) {
			c++
		}
	}
	return c
}

func countWithOptical(onus []zte.ONU) int {
	c := 0
	for _, o := range onus {
		if o.RxPowerDBM != 0 || o.TxPowerDBM != 0 {
			c++
		}
	}
	return c
}

func (service *Service) tryProfile(ctx context.Context, session *gosnmp.GoSNMP, profile *zte.FirmwareProfile) (*zte.FirmwareProfile, []zte.ONU, error) {
	onus, err := zte.WalkONUs(ctx, session, profile)
	if err != nil {
		return profile, nil, err
	}
	return profile, onus, nil
}

// profileLooksGood true bila mayoritas ONU punya serial (profil valid).
func profileLooksGood(onus []zte.ONU) bool {
	if len(onus) == 0 {
		return false
	}
	withSerial := 0
	for _, o := range onus {
		if strings.TrimSpace(o.SerialNumber) != "" {
			withSerial++
		}
	}
	return withSerial*2 >= len(onus)
}

func (service *Service) detectBestProfile(ctx context.Context, session *gosnmp.GoSNMP) (*zte.FirmwareProfile, []zte.ONU, error) {
	// Coba v2.2 (tree .1082) dulu. Bila sudah menghasilkan ONU ber-serial,
	// pakai langsung tanpa walk v2.1 (tree .1012) yang lambat/timeout — walk
	// ganda inilah yang dulu menghabiskan budget context sampai tulis DB gagal.
	p22, onus22, err22 := service.tryProfile(ctx, session, zte.FirmwareProfiles["v2.2"])
	if err22 == nil && profileLooksGood(onus22) {
		return p22, onus22, nil
	}
	p21, onus21, err21 := service.tryProfile(ctx, session, zte.FirmwareProfiles["v2.1"])

	score := func(profile *zte.FirmwareProfile, onus []zte.ONU, err error) int {
		if err != nil || len(onus) == 0 {
			return -1
		}
		s := len(onus) * 2
		for _, o := range onus {
			if strings.TrimSpace(o.SerialNumber) != "" {
				s += 2
			}
			if statusAllowsOptical(o.Status) {
				s += 3
			}
			if o.RxPowerDBM != 0 || o.TxPowerDBM != 0 {
				s += 8
			}
		}
		return s
	}

	s22 := score(p22, onus22, err22)
	s21 := score(p21, onus21, err21)

	if s22 < 0 && s21 < 0 {
		if err22 != nil {
			return nil, nil, fmt.Errorf("%w: v2.2=%v, v2.1=%v", ErrUnreachable, err22, err21)
		}
		return nil, nil, fmt.Errorf("%w: v2.2 & v2.1 keduanya kosong", ErrUnreachable)
	}
	if s22 >= s21 {
		return p22, onus22, nil
	}
	return p21, onus21, nil
}

func sampleRawOptical(ctx context.Context, session *gosnmp.GoSNMP, profile *zte.FirmwareProfile, onus []zte.ONU) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[sampleRawOptical] panic: %v", r)
		}
	}()
	var candidates []zte.ONU
	for _, o := range onus {
		if statusAllowsOptical(o.Status) && (o.RxPowerDBM != 0 || o.TxPowerDBM != 0) {
			candidates = append(candidates, o)
		}
		if len(candidates) >= 5 {
			break
		}
	}
	if len(candidates) == 0 {
		return
	}
	base := profile.BaseOID
	rxOID := base + profile.ONURxPower
	txOID := base + profile.ONUTxPower
	for _, o := range candidates {
		idx := o.Index
		if idx == "" {
			continue
		}
		fullRx := rxOID + "." + idx
		fullTx := txOID + "." + idx
		rxRaw, _ := snmpGetString(session, fullRx)
		txRaw, _ := snmpGetString(session, fullTx)
		log.Printf("[SyncONUs] sample serial=%s idx=%s rx_raw=%s tx_raw=%s rx_dbm=%.2f tx_dbm=%.2f",
			o.SerialNumber, idx, rxRaw, txRaw, o.RxPowerDBM, o.TxPowerDBM)
	}
}

func snmpGetString(session *gosnmp.GoSNMP, oid string) (string, error) {
	result, err := session.Get([]string{oid})
	if err != nil {
		return "", err
	}
	for _, v := range result.Variables {
		return fmt.Sprintf("%v", v.Value), nil
	}
	return "", fmt.Errorf("empty")
}

// ListONUs mengembalikan cache ONU dari database (cepat, tanpa SNMP).
func (service *Service) ListONUs(ctx context.Context, tenantID, id string) ([]zte.ONU, error) {
	return service.repository.ListONUs(ctx, tenantID, id)
}

// RebootONU melakukan reboot satu ONU langsung di OLT.
func (service *Service) RebootONU(ctx context.Context, tenantID, id, index string) error {
	session, cleanup, err := service.connect(ctx, tenantID, id)
	if err != nil {
		return err
	}
	defer cleanup()
	if err := zte.ResetONU(session, index); err != nil {
		return fmt.Errorf("%w: %s", ErrUnreachable, err.Error())
	}
	return nil
}

// SetONUEnabled meng-enable/disable satu ONU langsung di OLT.
func (service *Service) SetONUEnabled(ctx context.Context, tenantID, id, index string, enable bool) error {
	session, cleanup, err := service.connect(ctx, tenantID, id)
	if err != nil {
		return err
	}
	defer cleanup()
	if err := zte.SetONUAdmin(session, index, enable); err != nil {
		return fmt.Errorf("%w: %s", ErrUnreachable, err.Error())
	}
	return nil
}

func resolveONURef(index, pon string, onuID int) (string, int, bool) {
	if p := sanitizePON(pon); p != "" && onuID > 0 {
		return p, onuID, true
	}
	if index == "" {
		return "", 0, false
	}
	p, oid, ok := splitIndex(index)
	if !ok {
		return "", 0, false
	}
	return p, oid, true
}

// RebootONUHybrid prioritas SNMP; jika gagal fallback CLI onu reset.
func (service *Service) RebootONUHybrid(ctx context.Context, tenantID, id, index, pon string, onuID int) (ONUActionResult, error) {
	result := ONUActionResult{Action: "reboot"}
	var snmpErr error
	if index != "" {
		if err := service.RebootONU(ctx, tenantID, id, index); err == nil {
			result.Method = "snmp"
			result.Message = "reboot via SNMP berhasil"
			return result, nil
		} else {
			snmpErr = err
		}
	}
	p, oid, ok := resolveONURef(index, pon, onuID)
	if !ok {
		if snmpErr != nil {
			return result, snmpErr
		}
		return result, ErrInvalidInput
	}
	if err := service.RebootONUCli(ctx, tenantID, id, p, oid); err != nil {
		if snmpErr != nil {
			return result, fmt.Errorf("SNMP gagal (%v); fallback CLI gagal (%v)", snmpErr, err)
		}
		return result, err
	}
	result.Method = "cli"
	result.Fallback = snmpErr != nil
	result.Message = "reboot via CLI berhasil"
	return result, nil
}

// ResetONUHybrid melakukan reset ringan (disable-enable) SNMP, fallback CLI.
func (service *Service) ResetONUHybrid(ctx context.Context, tenantID, id, index, pon string, onuID int) (ONUActionResult, error) {
	result := ONUActionResult{Action: "reset"}
	var snmpErr error
	if index != "" {
		if err := service.SetONUEnabled(ctx, tenantID, id, index, false); err == nil {
			time.Sleep(1200 * time.Millisecond)
			if err2 := service.SetONUEnabled(ctx, tenantID, id, index, true); err2 == nil {
				result.Method = "snmp"
				result.Message = "reset (disable-enable) via SNMP berhasil"
				return result, nil
			} else {
				snmpErr = err2
			}
		} else {
			snmpErr = err
		}
	}
	p, oid, ok := resolveONURef(index, pon, onuID)
	if !ok {
		if snmpErr != nil {
			return result, snmpErr
		}
		return result, ErrInvalidInput
	}
	if err := service.BounceONUCli(ctx, tenantID, id, p, oid); err != nil {
		if snmpErr != nil {
			return result, fmt.Errorf("SNMP reset gagal (%v); fallback CLI gagal (%v)", snmpErr, err)
		}
		return result, err
	}
	result.Method = "cli"
	result.Fallback = snmpErr != nil
	result.Message = "reset (off-on) via CLI berhasil"
	return result, nil
}

// DeleteONUHybrid hapus konfigurasi ONU. SNMP tidak mendukung remove config,
// sehingga jalur utama langsung CLI (`interface gpon-olt` + `no onu`).
func (service *Service) DeleteONUHybrid(ctx context.Context, tenantID, id, index, pon string, onuID int) (ONUActionResult, error) {
	result := ONUActionResult{Action: "delete"}
	p, oid, ok := resolveONURef(index, pon, onuID)
	if !ok {
		return result, ErrInvalidInput
	}
	if err := service.DeleteONUCli(ctx, tenantID, id, p, oid); err != nil {
		return result, err
	}
	result.Method = "cli"
	result.Message = "hapus konfigurasi ONU via CLI berhasil"
	return result, nil
}

// GetONUConfigDetail mengambil detail ONU via SNMP terlebih dahulu.
// Data deep (tcont/gemport/service-port) hanya dari CLI dan tidak diisi di sini
// karena CLI serial/berat. Fallback ke CLI hanya jika SNMP tidak mengembalikan data.
func (service *Service) GetONUConfigDetail(ctx context.Context, tenantID, id, index, pon string, onuID int, forceCLI bool) (*ONUConfigDetail, map[string]string, error) {
	// Index asli dari FE = otoritatif: decode pon/onuID langsung darinya agar
	// tidak bergantung label onu_number DB yang bisa basi/duplikat.
	p, oid := "", 0
	if isRealSNMPIndex(index) {
		if pp, id2, ok := splitIndex(index); ok {
			p, oid = pp, id2
		}
	}
	if p == "" {
		var ok bool
		p, oid, ok = resolveONURef("", pon, onuID)
		if !ok {
			return nil, nil, ErrInvalidInput
		}
	}

	onuNumber := fmt.Sprintf("%s:%d", p, oid)

	// Kunci cache = index SNMP asli (kolom olt_onus.index); onuNumber hanya fallback.
	realIndex := strings.TrimSpace(index)
	if !isRealSNMPIndex(realIndex) {
		realIndex, _ = service.repository.FindONUIndexByRef(ctx, tenantID, id, p, oid)
		realIndex = strings.TrimSpace(realIndex)
	}
	cacheKey := realIndex
	if cacheKey == "" {
		cacheKey = onuNumber
	}

	// Coba cache CLI detail jika tersedia dan belum basi (<5 menit).
	cached, cachedAt, cacheErr := service.repository.LoadONUDetailCache(ctx, tenantID, id, cacheKey)
	if cacheErr != nil {
		log.Printf("LoadONUDetailCache %s error: %v", cacheKey, cacheErr)
	}
	cacheFresh := cached != nil && time.Since(cachedAt) < 5*time.Minute

	// SNMP fast path untuk data dasar + optical.
	snmp, snmpErr := service.fetchONUDetailSNMP(ctx, tenantID, id, p, oid, realIndex)
	if snmpErr == nil && snmp != nil {
		detail := &ONUConfigDetail{
			PONPort:      p,
			ONUID:        oid,
			Interface:    fmt.Sprintf("gpon-onu_%s:%d", p, oid),
			Status:       snmp.Status,
			Name:         snmp.Name,
			Description:  snmp.Description,
			SerialNumber: snmp.SerialNumber,
			ONUType:      snmp.ONUType,
			DistanceM:    snmp.DistanceM,
			RxONUSideDBM: snmp.RxDBM,
			TxONUSideDBM: snmp.TxDBM,
		}
		if statusAllowsOptical(snmp.Status) {
			// Backfill dari cache full-sync: tabel optical ZTE memakai format
			// index berbeda sehingga GET per-ONU bisa kosong padahal ONU online.
			if row := service.lookupCachedONU(ctx, tenantID, id, realIndex, onuNumber); row != nil {
				if detail.RxONUSideDBM == 0 && row.RxPowerDBM != 0 {
					detail.RxONUSideDBM = row.RxPowerDBM
				}
				if detail.TxONUSideDBM == 0 && row.TxPowerDBM != 0 {
					detail.TxONUSideDBM = row.TxPowerDBM
				}
				if detail.DistanceM == 0 && row.DistanceM != 0 {
					detail.DistanceM = row.DistanceM
				}
				if detail.SerialNumber == "" {
					detail.SerialNumber = row.SerialNumber
				}
				if detail.Name == "" {
					detail.Name = row.Name
				}
			}
		} else if !statusAllowsOptical(snmp.Status) && snmp.Status != "" && snmp.Status != "unknown" {
			detail.RxONUSideDBM = 0
			detail.TxONUSideDBM = 0
		}
		meta := map[string]string{"method": "snmp_snapshot"}

		// VLAN/service-port via BP MIB (best-effort, tidak memblokir bila kosong).
		if vlans, ports, spErr := service.fetchONUServicePortsSNMP(ctx, tenantID, id, p, oid); spErr == nil {
			if len(ports) > 0 {
				detail.ServicePorts = ports
			}
			if len(vlans) > 0 {
				detail.VLANs = vlans
			}
		} else {
			log.Printf("fetchONUServicePortsSNMP %s:%d: %v", p, oid, spErr)
		}

		// Merge cache lokal jika ada.
		if cacheFresh {
			mergeDetailConfig(detail, cached)
			meta["method"] = "snmp+cache"
			meta["cached_at"] = cachedAt.Format(time.RFC3339)
			return detail, meta, nil
		}

		// CLI detail hanya dieksekusi saat user eksplisit forceCLI, karena CLI per-ONU lambat
		// dan tidak boleh otomatis background agar tidak memblokir/membebani OLT.
		if forceCLI && !cacheFresh {
			return service.ProbeONUConfigDetailAndCache(ctx, tenantID, id, p, oid, cacheKey)
		}
		// Cache basi: tampilkan config lama dulu (lebih baik daripada kosong),
		// lalu refresh deep-config via CLI di background (non-blocking, anti-duplikat).
		if cached != nil {
			mergeDetailConfig(detail, cached)
			meta["cached_at"] = cachedAt.Format(time.RFC3339)
			meta["method"] = "snmp+stale_cache"
		}
		service.scheduleDetailConfigFetch(tenantID, id, p, oid, cacheKey)
		meta["config_loading"] = "1"
		return detail, meta, nil
	}

	log.Printf("GetONUConfigDetail SNMP failed for %s:%d, falling back to CLI: %v", p, oid, snmpErr)

	// Cache fresh cukup; return tanpa CLI lagi.
	if cacheFresh {
		cached.PONPort = p
		cached.ONUID = oid
		cached.Interface = fmt.Sprintf("gpon-onu_%s:%d", p, oid)
		// Identitas dipaksa dari DB (sumber SNMP exact-index) — cache CLI
		// pernah terkontaminasi output ONU lain (telnet bleed).
		if row := service.lookupCachedONU(ctx, tenantID, id, realIndex, onuNumber); row != nil {
			if strings.TrimSpace(row.Name) != "" {
				cached.Name = row.Name
			}
			if strings.TrimSpace(row.SerialNumber) != "" {
				cached.SerialNumber = row.SerialNumber
			}
		}
		return cached, map[string]string{"method": "cache", "cached_at": cachedAt.Format(time.RFC3339)}, nil
	}

	// SNMP gagal. Cache tidak fresh. Hanya forceCLI yang trigger CLI lengkap.
	if forceCLI {
		return service.ProbeONUConfigDetailAndCache(ctx, tenantID, id, p, oid, cacheKey)
	}
	// Snapshot dari cache full-sync agar panel tidak kosong; deep-config di background.
	fallback := &ONUConfigDetail{
		PONPort:   p,
		ONUID:     oid,
		Interface: fmt.Sprintf("gpon-onu_%s:%d", p, oid),
	}
	if row := service.lookupCachedONU(ctx, tenantID, id, realIndex, onuNumber); row != nil {
		fallback.Status = row.Status
		fallback.Name = row.Name
		fallback.SerialNumber = row.SerialNumber
		fallback.Description = row.Description
		fallback.RxONUSideDBM = row.RxPowerDBM
		fallback.TxONUSideDBM = row.TxPowerDBM
		fallback.DistanceM = row.DistanceM
	}
	if cached != nil {
		mergeDetailConfig(fallback, cached)
	}
	service.scheduleDetailConfigFetch(tenantID, id, p, oid, cacheKey)
	return fallback, map[string]string{"method": "db_snapshot", "config_loading": "1"}, nil
}

// backgroundFetchONUConfigDetail menarik detail config via CLI dan menyimpannya ke cache DB.
func (service *Service) backgroundFetchONUConfigDetail(tenantID, oltID, pon string, onuID int, index string) {
	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
	defer cancel()
	detail, _, err := service.ProbeONUConfigCLI(ctx, tenantID, oltID, pon, onuID)
	if err != nil {
		log.Printf("backgroundFetchONUConfigDetail %s:%d error: %v", pon, onuID, err)
		return
	}
	if detail == nil {
		return
	}
	if saveErr := service.repository.SaveONUDetailCache(ctx, tenantID, oltID, index, detail); saveErr != nil {
		log.Printf("SaveONUDetailCache %s error: %v", index, saveErr)
	}
}

// detailFetchInFlight mencegah probe CLI ganda untuk ONU yang sama.
var detailFetchInFlight sync.Map

// scheduleDetailConfigFetch menarik deep-config CLI di background sekali per ONU
// agar panel detail tidak blocking dan OLT tidak kebanjiran sesi CLI.
func (service *Service) scheduleDetailConfigFetch(tenantID, oltID, pon string, onuID int, index string) {
	key := tenantID + "/" + oltID + "/" + index
	if _, running := detailFetchInFlight.LoadOrStore(key, true); running {
		return
	}
	go func() {
		defer detailFetchInFlight.Delete(key)
		service.backgroundFetchONUConfigDetail(tenantID, oltID, pon, onuID, index)
	}()
}

// ProbeONUConfigDetailAndCache menjalankan probe CLI penuh lalu menyimpan hasilnya
// ke cache agar pembukaan detail berikutnya instan.
func (service *Service) ProbeONUConfigDetailAndCache(ctx context.Context, tenantID, oltID, pon string, onuID int, cacheKey string) (*ONUConfigDetail, map[string]string, error) {
	detail, raws, err := service.ProbeONUConfigCLI(ctx, tenantID, oltID, pon, onuID)
	if err == nil && detail != nil && strings.TrimSpace(cacheKey) != "" {
		if saveErr := service.repository.SaveONUDetailCache(ctx, tenantID, oltID, cacheKey, detail); saveErr != nil {
			log.Printf("SaveONUDetailCache %s error: %v", cacheKey, saveErr)
		}
	}
	return detail, raws, err
}

// lookupCachedONU mengembalikan baris cache full-sync satu ONU (match index atau onuNumber).
func (service *Service) lookupCachedONU(ctx context.Context, tenantID, oltID, index, onuNumber string) *zte.ONU {
	onus, err := service.repository.ListONUs(ctx, tenantID, oltID)
	if err != nil {
		return nil
	}
	for i := range onus {
		if (index != "" && onus[i].Index == index) || (onuNumber != "" && onus[i].ONUNumber == onuNumber) {
			return &onus[i]
		}
	}
	return nil
}

func mergeDetailConfig(base, extra *ONUConfigDetail) {
	if extra == nil {
		return
	}
	if base.RxOLTSideDBM == 0 && extra.RxOLTSideDBM != 0 {
		base.RxOLTSideDBM = extra.RxOLTSideDBM
	}
	if base.UpstreamBps == 0 && extra.UpstreamBps != 0 {
		base.UpstreamBps = extra.UpstreamBps
	}
	if base.DownstreamBps == 0 && extra.DownstreamBps != 0 {
		base.DownstreamBps = extra.DownstreamBps
	}
	if len(extra.VLANs) > 0 {
		base.VLANs = extra.VLANs
	}
	if len(extra.DBAProfiles) > 0 {
		base.DBAProfiles = extra.DBAProfiles
	}
	if len(extra.UpstreamProfiles) > 0 {
		base.UpstreamProfiles = extra.UpstreamProfiles
	}
	if len(extra.DownstreamProfiles) > 0 {
		base.DownstreamProfiles = extra.DownstreamProfiles
	}
	if len(extra.Tconts) > 0 {
		base.Tconts = extra.Tconts
	}
	if len(extra.Gemports) > 0 {
		base.Gemports = extra.Gemports
	}
	if len(extra.ServicePorts) > 0 {
		base.ServicePorts = extra.ServicePorts
	}
	if len(extra.WANIPs) > 0 {
		base.WANIPs = extra.WANIPs
	}
	if extra.ConfigState.Status != "" {
		base.ConfigState = extra.ConfigState
	}
	if len(extra.Warnings) > 0 {
		base.Warnings = extra.Warnings
	}
}

type ONULiveSyncResult struct {
	PON      string  `json:"pon"`
	ONUID    int     `json:"onu_id"`
	Status   string  `json:"status"`
	RxPower  float64 `json:"rx_power_dbm"`
	TxPower  float64 `json:"tx_power_dbm"`
	Distance float64 `json:"distance_m"`
	Name     string  `json:"name,omitempty"`
	Method   string  `json:"method"`
}

func normalizeONUStatus(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case strings.Contains(v, "work"):
		return "working"
	case strings.Contains(v, "dying"):
		return "dying_gasp"
	case strings.Contains(v, "los"):
		return "los"
	case strings.Contains(v, "offline"):
		return "offlined"
	case strings.Contains(v, "sync"):
		return "sync_mib"
	case strings.Contains(v, "auth"):
		return "auth_failed"
	case v == "":
		return "unknown"
	default:
		return v
	}
}

func statusAllowsOptical(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "working", "logging", "sync_mib", "online", "ready":
		return true
	default:
		return false
	}
}

// normalizeStatusKey tersedia untuk FE tidak perlu reimplementasi.
func normalizeStatusKeyFE(raw string) string {
	return normalizeONUStatus(raw)
}

// SyncONULiveByRef sinkronisasi satu ONU secara on-demand via SNMP.
// SNMP jauh lebih cepat dari CLI serial; CLI hanya fallback jika SNMP gagal.
func (service *Service) SyncONULiveByRef(ctx context.Context, tenantID, id, index, pon string, onuID int) (ONULiveSyncResult, error) {
	p, oid, ok := resolveONURef(index, pon, onuID)
	if !ok {
		return ONULiveSyncResult{}, ErrInvalidInput
	}

	// SNMP fast path.
	snmp, err := service.fetchONUDetailSNMP(ctx, tenantID, id, p, oid, index)
	if err == nil && snmp != nil {
		status := normalizeONUStatus(snmp.Status)
		name := strings.TrimSpace(snmp.Name)
		desc := strings.TrimSpace(snmp.Description)
		rx := snmp.RxDBM
		tx := snmp.TxDBM
		distance := snmp.DistanceM

		// Tulis non-destruktif: status unknown => jangan ubah status/optik lama;
		// offline terkonfirmasi => nolkan optik; online => tulis optik yang terbaca
		// (nil = pertahankan nilai lama saat pembacaan drop/kosong).
		writeStatus := status
		var rxPtr, txPtr, distPtr *float64
		switch {
		case status == "unknown":
			writeStatus = ""
		case statusAllowsOptical(status):
			if rx != 0 {
				rxPtr = &rx
			}
			if tx != 0 {
				txPtr = &tx
			}
			if distance != 0 {
				distPtr = &distance
			}
		default:
			zero := 0.0
			rxPtr, txPtr = &zero, &zero
			rx, tx = 0, 0
		}
		var namePtr *string
		if name != "" {
			namePtr = &name
		}
		var descPtr *string
		if desc != "" {
			descPtr = &desc
		}
		var serialPtr *string
		serial := strings.ToUpper(strings.TrimSpace(snmp.SerialNumber))
		if serial != "" {
			serialPtr = &serial
		}
		if dbErr := service.repository.UpdateONULiveByRef(ctx, tenantID, id, p, oid, writeStatus, rxPtr, txPtr, distPtr, namePtr, descPtr); dbErr != nil {
			return ONULiveSyncResult{}, dbErr
		}
		// Backfill respons dari cache DB: pembacaan optical per-ONU bisa kosong
		// (format index tabel optical beda) — FE mem-patch baris, jangan kirim 0.
		if statusAllowsOptical(status) && (rx == 0 || tx == 0 || distance == 0 || name == "") {
			if row := service.lookupCachedONU(ctx, tenantID, id, index, fmt.Sprintf("%s:%d", p, oid)); row != nil {
				if rx == 0 {
					rx = row.RxPowerDBM
				}
				if tx == 0 {
					tx = row.TxPowerDBM
				}
				if distance == 0 {
					distance = row.DistanceM
				}
				if name == "" {
					name = row.Name
				}
			}
		}
		if serialPtr != nil {
			if dbErr := service.repository.UpdateONUSerialByRef(ctx, tenantID, id, p, oid, serialPtr); dbErr != nil {
				log.Printf("SyncONULiveByRef UpdateONUSerialByRef %s:%d error: %v", p, oid, dbErr)
			}
		}
		return ONULiveSyncResult{
			PON:      p,
			ONUID:    oid,
			Status:   status,
			RxPower:  rx,
			TxPower:  tx,
			Distance: distance,
			Name:     name,
			Method:   "snmp",
		}, nil
	}

	log.Printf("SyncONULiveByRef SNMP failed for %s:%d: %v", p, oid, err)

	// SNMP gagal. JANGAN blok request dengan sesi CLI serial (bisa 30-60s dan
	// memicu timeout FE). Kembalikan snapshot cepat dari cache DB lalu jadwalkan
	// refresh CLI di background (non-blocking) untuk mengisi field yang hilang.
	service.scheduleCLIRefresh(tenantID, id, p, oid, index)

	fallbackStatus := "unknown"
	if onus, listErr := service.repository.ListONUs(ctx, tenantID, id); listErr == nil {
		want := fmt.Sprintf("%s:%d", p, oid)
		for _, o := range onus {
			if o.ONUNumber == want || o.Index == index {
				fallbackStatus = normalizeONUStatus(o.Status)
				return ONULiveSyncResult{
					PON:      p,
					ONUID:    oid,
					Status:   fallbackStatus,
					RxPower:  o.RxPowerDBM,
					TxPower:  o.TxPowerDBM,
					Distance: o.DistanceM,
					Name:     o.Name,
					Method:   "cache",
				}, nil
			}
		}
	}

	return ONULiveSyncResult{
		PON:    p,
		ONUID:  oid,
		Status: fallbackStatus,
		Method: "cache",
	}, nil
}

// scheduleCLIRefresh menjalankan probe CLI di goroutine terpisah dengan timeout
// sendiri, lalu menyimpan hasil ke cache DB. Tidak memblokir request pemanggil.
func (service *Service) scheduleCLIRefresh(tenantID, id, pon string, onuID int, index string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		detail, _, detailErr := service.ProbeONUConfigCLI(ctx, tenantID, id, pon, onuID)
		optical, _, opticalErr := service.ProbeONUOpticalCLI(ctx, tenantID, id, pon, onuID)
		if detailErr != nil && opticalErr != nil {
			log.Printf("scheduleCLIRefresh %s:%d gagal: detail=%v optical=%v", pon, onuID, detailErr, opticalErr)
			return
		}
		status := "unknown"
		if detail != nil {
			status = normalizeONUStatus(detail.Status)
		}
		rx, tx, distance := 0.0, 0.0, 0.0
		if optical != nil {
			rx, tx, distance = optical.Rx, optical.Tx, optical.DistanceM
		}
		if detail != nil {
			if rx == 0 && detail.RxONUSideDBM != 0 {
				rx = detail.RxONUSideDBM
			}
			if tx == 0 && detail.TxONUSideDBM != 0 {
				tx = detail.TxONUSideDBM
			}
			if distance == 0 && detail.DistanceM > 0 {
				distance = detail.DistanceM
			}
		}
		writeStatus := status
		var rxPtr, txPtr, distPtr *float64
		switch {
		case status == "unknown":
			writeStatus = ""
		case statusAllowsOptical(status):
			if rx != 0 {
				rxPtr = &rx
			}
			if tx != 0 {
				txPtr = &tx
			}
			if distance != 0 {
				distPtr = &distance
			}
		default:
			zero := 0.0
			rxPtr, txPtr = &zero, &zero
		}
		// Name/description SENGAJA tidak ditulis dari CLI: output telnet pooled
		// pernah bocor antar-ONU sehingga nama ONU lain menimpa DB. Identitas
		// hanya ditulis dari SNMP (GET per-index, pasti ONU yang benar).
		if dbErr := service.repository.UpdateONULiveByRef(ctx, tenantID, id, pon, onuID, writeStatus, rxPtr, txPtr, distPtr, nil, nil); dbErr != nil {
			log.Printf("scheduleCLIRefresh UpdateONULiveByRef %s:%d error: %v", pon, onuID, dbErr)
		}
		if detail != nil && strings.TrimSpace(index) != "" {
			if saveErr := service.repository.SaveONUDetailCache(ctx, tenantID, id, index, detail); saveErr != nil {
				log.Printf("scheduleCLIRefresh SaveONUDetailCache %s error: %v", index, saveErr)
			}
		}
	}()
}


// Create menambahkan OLT baru.
func (service *Service) Create(ctx context.Context, tenantID string, input SaveInput) (OLT, error) {
	if err := service.requireKey(); err != nil {
		return OLT{}, err
	}
	if err := validate(input); err != nil {
		return OLT{}, err
	}
	if strings.ToLower(input.SNMPMode) == "v2c" && strings.TrimSpace(input.Community) == "" {
		return OLT{}, ErrInvalidInput
	}
	secrets, err := service.encryptSecrets(input)
	if err != nil {
		return OLT{}, err
	}
	return service.repository.Create(ctx, tenantID, input, secrets)
}

// Update memperbarui data OLT; field rahasia kosong artinya tidak berubah.
func (service *Service) Update(ctx context.Context, tenantID, id string, input SaveInput) (OLT, error) {
	if err := service.requireKey(); err != nil {
		return OLT{}, err
	}
	if err := validate(input); err != nil {
		return OLT{}, err
	}
	secrets, err := service.encryptSecrets(input)
	if err != nil {
		return OLT{}, err
	}
	return service.repository.Update(ctx, tenantID, id, input, secrets)
}

func (service *Service) Get(ctx context.Context, tenantID, id string) (OLT, error) {
	return service.repository.Get(ctx, tenantID, id)
}

func (service *Service) List(ctx context.Context, tenantID string) ([]OLT, error) {
	return service.repository.List(ctx, tenantID)
}

func (service *Service) Delete(ctx context.Context, tenantID, id string) error {
	return service.repository.Delete(ctx, tenantID, id)
}

// SyncONUsFast melakukan sinkronisasi berkecepatan tinggi untuk puluhan
// ribu ONU: identitas via walk paralel, lalu simpan dengan bulk upsert.
func (service *Service) SyncONUsFast(ctx context.Context, tenantID, id string) (int, error) {
	session, cleanup, err := service.connect(ctx, tenantID, id)
	if err != nil {
		return 0, err
	}
	defer cleanup()
	// Prioritaskan profil v2.2 (tree .1082) — default modern & tervalidasi
	// di OLT produksi. sysDescr "V2.1" sering menyesatkan (OLT tetap expose
	// tree .1082), jadi kita probe: v2.2 dulu, fallback v2.1 bila kosong.
	profile := zte.FirmwareProfiles["v2.2"]
	onus, err := zte.WalkONUs(ctx, session, profile)
	if err != nil {
		// v2.2 gagal (timeout/OID tidak ada) — coba v2.1
		onus2, err2 := zte.WalkONUs(ctx, session, zte.FirmwareProfiles["v2.1"])
		if err2 != nil {
			return 0, fmt.Errorf("%w: baca onu: %s", ErrUnreachable, err2.Error())
		}
		onus = onus2
	} else if len(onus) == 0 {
		if onus2, err2 := zte.WalkONUs(ctx, session, zte.FirmwareProfiles["v2.1"]); err2 == nil && len(onus2) > 0 {
			onus = onus2
		}
	}
	if err := service.repository.UpsertONUs(ctx, tenantID, id, onus); err != nil {
		return 0, fmt.Errorf("simpan cache onu: %w", err)
	}
	// Setelah sync, langsung catat sampel counter pertama (bps masih 0).
	_, _ = service.RefreshTraffic(ctx, tenantID, id)
	return len(onus), nil
}

// RefreshTraffic menghitung bps realtime untuk ONU: baca counter octets
// sekarang lalu bandingkan dengan sampel sebelumnya yang tersimpan di DB.
// Untuk firmware C320 V2.1.0 hybrid tree .1082: ifTable hanya punya
// interface per-PON-port, tidak per-ONU. Karena itu kita pakai tabel
// privat ZTE .500.10.2.3.2.2.1.<index>.<col> untuk mengambil oktet
// downstream (col 1) dan upstream (col 2) per-ONU.
func (service *Service) RefreshTraffic(ctx context.Context, tenantID, id string) (int, error) {
	session, cleanup, err := service.connect(ctx, tenantID, id)
	if err != nil {
		return 0, err
	}
	defer cleanup()

	current, err := service.repository.LoadTrafficCounters(ctx, tenantID, id)
	if err != nil {
		return 0, err
	}
	if len(current) == 0 {
		return 0, nil
	}

	// Firmware C320 V2.1.0 hybrid tree .1082: ifTable hanya punya
	// interface per-PON-port, tidak per-ONU. Karena itu kita pakai tabel
	// privat ZTE .500.10.2.3.2.2.1.<index>.<col> untuk mengambil oktet
	// downstream (col 1) dan upstream (col 2) per-ONU.
	samples, privateErr := zte.SampleAllPrivateONUCounters(session, zte.PrivateInCol(), zte.PrivateOutCol())
	if privateErr == nil && len(samples) > 0 {
		// Hanya update ONU yang memang punya counter privat.
		filtered := make(map[string]zte.TrafficSample, len(current))
		for index := range current {
			if s, ok := samples[index]; ok && (s.InOctets > 0 || s.OutOctets > 0) {
				filtered[index] = s
			}
		}
		if len(filtered) > 0 && !looksLikePerPONAggregate(filtered) {
			updated, err := service.repository.UpdateTraffic(ctx, tenantID, id, filtered)
			if err != nil {
				return 0, err
			}
			service.saveTrafficHistory(ctx, tenantID, id)
			return updated, nil
		}
	}

	// Fallback: ifTable standar (jika firmware memang expose interface per-ONU).
	labels := zte.WalkIfNameLabels(session)
	fallback := make(map[string]zte.TrafficSample, len(current))
	for index := range current {
		ifIndex := index
		if i := strings.IndexByte(index, '.'); i > 0 {
			ifIndex = index[:i]
		}
		if _, err := strconv.Atoi(ifIndex); err != nil {
			if v, hit := labels[zte.ONULabel(index)]; hit {
				ifIndex = v
			} else {
				continue
			}
		}
		sample, sampleErr := zte.SampleTrafficONU(session, ifIndex)
		if sampleErr == nil {
			fallback[index] = sample
		}
	}
	if len(fallback) > 0 && !looksLikePerPONAggregate(fallback) {
		updated, err := service.repository.UpdateTraffic(ctx, tenantID, id, fallback)
		if err != nil {
			return 0, err
		}
		service.saveTrafficHistory(ctx, tenantID, id)
		return updated, nil
	}

	// Fallback terakhir: CLI per-ONU (lebih lambat, default OFF agar tidak
	// membanjiri OLT saat polling background). Aktifkan jika dibutuhkan:
	//   ZTE_TRAFFIC_CLI_BULK=1
	if strings.TrimSpace(os.Getenv("ZTE_TRAFFIC_CLI_BULK")) != "1" {
		return 0, nil
	}
	cliSamples := service.refreshTrafficViaCLI(ctx, tenantID, id, current)
	if len(cliSamples) == 0 {
		return 0, nil
	}
	updated, err := service.repository.UpdateTraffic(ctx, tenantID, id, cliSamples)
	if err != nil {
		return 0, err
	}
	service.saveTrafficHistory(ctx, tenantID, id)
	return updated, nil
}

func (service *Service) refreshTrafficViaCLI(ctx context.Context, tenantID, id string, current map[string]OnuCounterRow) map[string]zte.TrafficSample {
	onus, err := service.repository.ListONUs(ctx, tenantID, id)
	if err != nil {
		log.Printf("RefreshTraffic CLI fallback ListONUs error: %v", err)
		return nil
	}
	if len(onus) == 0 {
		return nil
	}
	result := make(map[string]zte.TrafficSample, len(onus))
	for _, onu := range onus {
		if _, ok := current[onu.Index]; !ok {
			continue
		}
		pon, onuID, ok := splitIndex(onu.ONUNumber)
		if !ok {
			pon, onuID, ok = splitIndex(onu.Index)
		}
		if !ok {
			continue
		}
		sample, _, probeErr := service.ProbeONUTrafficCLI(ctx, tenantID, id, pon, onuID)
		if probeErr != nil || sample == nil {
			continue
		}
		if sample.InOctets == 0 && sample.OutOctets == 0 {
			continue
		}
		result[onu.Index] = zte.TrafficSample{InOctets: sample.InOctets, OutOctets: sample.OutOctets}
	}
	if len(result) > 0 {
		log.Printf("RefreshTraffic CLI fallback samples: %d/%d", len(result), len(onus))
	}
	return result
}

func (service *Service) saveTrafficHistory(ctx context.Context, tenantID, id string) {
	rows, _, rowErr := service.repository.ListONUSPaged(ctx, tenantID, id, 1, 100000, "", "index", "asc", "")
	if rowErr != nil {
		return
	}
	samples := make(map[string]BpsRow, len(rows))
	for _, row := range rows {
		if row.InBps > 0 || row.OutBps > 0 {
			samples[row.Index] = BpsRow{InBps: row.InBps, OutBps: row.OutBps}
		}
	}
	if len(samples) > 0 {
		_ = service.repository.SaveBpsSamples(ctx, tenantID, id, samples)
		for index, bps := range samples {
			_ = service.repository.UpsertDaily(ctx, tenantID, id, index, bps.InBps, bps.OutBps)
		}
	}
}

// looksLikePerPONAggregate mendeteksi pola nilai yang identik untuk banyak ONU
// pada PON yang sama (signature counter agregat per-port, bukan per-ONU).
func looksLikePerPONAggregate(samples map[string]zte.TrafficSample) bool {
	byPON := map[string][]zte.TrafficSample{}
	for index, sample := range samples {
		pon, _, ok := splitIndex(zte.ONULabel(index))
		if !ok {
			continue
		}
		byPON[pon] = append(byPON[pon], sample)
	}
	if len(byPON) == 0 {
		return false
	}
	aggGroups := 0
	for _, group := range byPON {
		if len(group) < 2 {
			continue
		}
		first := group[0]
		same := true
		for i := 1; i < len(group); i++ {
			if group[i].InOctets != first.InOctets || group[i].OutOctets != first.OutOctets {
				same = false
				break
			}
		}
		if same {
			aggGroups++
		}
	}
	return aggGroups > 0
}

// GetIntraday: sampel bps per menit (default 180 menit) untuk grafik naik-turun.
func (service *Service) GetIntraday(ctx context.Context, tenantID, id, index string, minutes int) ([]BpsSampleRow, error) {
	return service.repository.GetIntraday(ctx, tenantID, id, index, minutes)
}

// ListONUSPaged mengembalikan halaman ONU dari cache DB (server-side).
func (service *Service) ListONUSPaged(ctx context.Context, tenantID, id string, page, pageSize int, search, sort, order, ponFilter string) (TrafficPage, error) {
	items, total, err := service.repository.ListONUSPaged(ctx, tenantID, id, page, pageSize, search, sort, order, ponFilter)
	if err != nil {
		return TrafficPage{}, err
	}
	return TrafficPage{Items: items, Total: total, Page: page}, nil
}

// GetHealth mengembalikan snapshot kesehatan fisik OLT.
// Untuk firmware lama (V2.1.0), OID card/temp/fan/PSU dari .1082.10 sering
// tidak tersedia dan walk timeout; kita ambil SFP-only sebagai fallback.
func (service *Service) GetHealth(ctx context.Context, tenantID, id string) (*zte.OltHealth, error) {
	service.healthMu.Lock()
	cached, ok := service.healthCache[id]
	service.healthMu.Unlock()
	if ok && time.Since(cached.at) < 15*time.Second {
		return cached.data, nil
	}

	session, cleanup, err := service.connect(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	walk := func(rootOID string) (map[string]string, error) {
		values := map[string]string{}
		err := session.Walk(rootOID, func(pdu gosnmp.SnmpPDU) error {
			switch v := pdu.Value.(type) {
			case []byte:
				values[strings.TrimPrefix(pdu.Name, rootOID+".")] = strings.TrimSpace(string(v))
			default:
				big := gosnmp.ToBigInt(pdu.Value)
				values[strings.TrimPrefix(pdu.Name, rootOID+".")] = big.String()
			}
			return nil
		})
		return values, err
	}
	walker := &zte.GosnmpHealthWalker{Session: &zte.GoSNMPWrapper{WalkFunc: walk}}

	health, err := zte.CollectHealth(walker)
	if err != nil {
		// Fallback SFP-only agar detail OLT tidak kosong/error di V2.1.
		health, sfpErr := zte.CollectHealthSFPOnly(walker)
		if sfpErr != nil {
			return nil, fmt.Errorf("%w: %s", ErrUnreachable, err.Error())
		}
		service.healthMu.Lock()
		service.healthCache[id] = healthCacheEntry{data: health, at: time.Now()}
		service.healthMu.Unlock()
		return health, nil
	}

	service.healthMu.Lock()
	if prev, okHit := service.healthCache[id]; okHit && prev.data != nil {
		elapsed := time.Since(prev.at).Seconds()
		if elapsed >= 2 {
			prevByLabel := map[string]zte.SfpInfo{}
			for _, s := range prev.data.Sfps {
				prevByLabel[s.Label] = s
			}
			for i := range health.Sfps {
				ps, hit := prevByLabel[health.Sfps[i].Label]
				if !hit {
					continue
				}
				if health.Sfps[i].InOctets >= ps.InOctets {
					health.Sfps[i].InBps = float64(health.Sfps[i].InOctets-ps.InOctets) * 8 / elapsed
				}
				if health.Sfps[i].OutOctets >= ps.OutOctets {
					health.Sfps[i].OutBps = float64(health.Sfps[i].OutOctets-ps.OutOctets) * 8 / elapsed
				}
			}
		}
	}
	service.healthCache[id] = healthCacheEntry{data: health, at: time.Now()}
	service.healthMu.Unlock()
	return health, nil
}

// OpenSession membuka sesi SNMP untuk OLT (untuk endpoint diagnosa).
func (service *Service) OpenSession(ctx context.Context, tenantID, id string) (*gosnmp.GoSNMP, func(), error) {
	return service.connect(ctx, tenantID, id)
}
