package router

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"isp-billing/internal/mikrotik"
	"isp-billing/internal/secretbox"
)

// pricePattern menangkap harga dari nama profil PPPoE, contoh: "150K",
// "PAKET-150K", "Home 250k" → 150000 / 250000.
var pricePattern = regexp.MustCompile(`(?i)(\d+)\s*K\b`)

type Service struct {
	repository    Repository
	encryptionKey []byte
	dialTimeout   time.Duration
}

func NewService(repository Repository, encryptionKey []byte) *Service {
	return &Service{repository: repository, encryptionKey: encryptionKey, dialTimeout: 12 * time.Second}
}

func ParseProfilePrice(profileName string) (float64, bool) {
	match := pricePattern.FindStringSubmatch(profileName)
	if match == nil {
		return 0, false
	}
	value, err := strconv.ParseFloat(match[1], 64)
	if err != nil || value <= 0 || value > 100_000 {
		return 0, false
	}
	return value * 1000, true
}

func (service *Service) requireKey() error {
	if len(service.encryptionKey) != 32 {
		return ErrNoEncryption
	}
	return nil
}

func (service *Service) Get(ctx context.Context, tenantID string) (Config, error) {
	if tenantID == "" {
		return Config{}, ErrInvalidInput
	}
	stored, err := service.repository.Get(ctx, tenantID)
	if err != nil {
		if errors.Is(err, ErrNotConfigured) {
			return Config{Configured: false, APIPort: 8728}, nil
		}
		return Config{}, fmt.Errorf("get router config: %w", err)
	}
	return stored.Config, nil
}

func (service *Service) Save(ctx context.Context, tenantID string, input SaveInput) (Config, error) {
	if err := service.requireKey(); err != nil {
		return Config{}, err
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Host = strings.TrimSpace(input.Host)
	input.APIUsername = strings.TrimSpace(input.APIUsername)
	if tenantID == "" || input.Host == "" || len(input.Host) > 253 || input.APIUsername == "" || len(input.APIUsername) > 100 {
		return Config{}, ErrInvalidInput
	}
	if strings.ContainsAny(input.Host, " /@") {
		return Config{}, ErrInvalidInput
	}
	if input.Name == "" {
		input.Name = "Router utama"
	}
	if len(input.Name) > 100 {
		return Config{}, ErrInvalidInput
	}
	if input.APIPort == 0 {
		if input.UseTLS {
			input.APIPort = 8729
		} else {
			input.APIPort = 8728
		}
	}
	if input.APIPort < 1 || input.APIPort > 65535 {
		return Config{}, ErrInvalidInput
	}

	var ciphertext []byte
	keyVersion := 0
	if input.APIPassword != "" {
		if len(input.APIPassword) > 200 {
			return Config{}, ErrInvalidInput
		}
		encrypted, err := secretbox.Encrypt(service.encryptionKey, input.APIPassword)
		if err != nil {
			return Config{}, fmt.Errorf("encrypt router password: %w", err)
		}
		ciphertext = encrypted
		keyVersion = secretbox.KeyVersion
	}

	saved, err := service.repository.Save(ctx, tenantID, input, ciphertext, keyVersion)
	if err != nil {
		if errors.Is(err, ErrInvalidInput) || errors.Is(err, ErrNotConfigured) {
			return Config{}, err
		}
		return Config{}, fmt.Errorf("save router config: %w", err)
	}
	return saved, nil
}

func (service *Service) connect(ctx context.Context, tenantID string) (*mikrotik.Client, StoredCredentials, error) {
	if err := service.requireKey(); err != nil {
		return nil, StoredCredentials{}, err
	}
	stored, err := service.repository.Get(ctx, tenantID)
	if err != nil {
		if errors.Is(err, ErrNotConfigured) {
			return nil, StoredCredentials{}, ErrNotConfigured
		}
		return nil, StoredCredentials{}, fmt.Errorf("load router config: %w", err)
	}
	password, err := secretbox.Decrypt(service.encryptionKey, stored.PasswordCiphertext)
	if err != nil {
		return nil, StoredCredentials{}, fmt.Errorf("decrypt router password: %w", err)
	}
	client, err := mikrotik.Dial(ctx, mikrotik.DialOptions{
		Host:     stored.Config.Host,
		Port:     stored.Config.APIPort,
		UseTLS:   stored.Config.UseTLS,
		Username: stored.Config.APIUsername,
		Password: password,
		Timeout:  service.dialTimeout,
	})
	if err != nil {
		_ = service.repository.RecordStatus(ctx, tenantID, stored.Config.ID, "", "", truncateError(err))
		return nil, StoredCredentials{}, fmt.Errorf("%w: %s", ErrUnreachable, err.Error())
	}
	return client, stored, nil
}

func (service *Service) Test(ctx context.Context, tenantID string) (TestResult, error) {
	client, stored, err := service.connect(ctx, tenantID)
	if err != nil {
		return TestResult{}, err
	}
	defer client.Close()

	var result TestResult
	if rows, err := client.Run("/system/identity/print"); err == nil && len(rows) > 0 {
		result.Identity = rows[0]["name"]
	}
	if rows, err := client.Run("/system/resource/print"); err == nil && len(rows) > 0 {
		result.RouterOSVersion = rows[0]["version"]
	}
	secrets, err := client.Run("/ppp/secret/print")
	if err != nil {
		return TestResult{}, fmt.Errorf("baca ppp secret: %w", err)
	}
	profiles, err := client.Run("/ppp/profile/print")
	if err != nil {
		return TestResult{}, fmt.Errorf("baca ppp profile: %w", err)
	}
	result.SecretCount = len(secrets)
	result.ProfileCount = len(profiles)
	_ = service.repository.RecordStatus(ctx, tenantID, stored.Config.ID, result.Identity, result.RouterOSVersion, "")
	return result, nil
}

// SyncPPPoE membaca seluruh profile + secret PPPoE dari router lalu
// menyelaraskannya ke paket, pelanggan, layanan, dan akun PPPoE.
func (service *Service) SyncPPPoE(ctx context.Context, tenantID string) (SyncSummary, error) {
	client, stored, err := service.connect(ctx, tenantID)
	if err != nil {
		return SyncSummary{}, err
	}
	defer client.Close()

	profileRows, err := client.Run("/ppp/profile/print")
	if err != nil {
		return SyncSummary{}, fmt.Errorf("baca ppp profile: %w", err)
	}
	secretRows, err := client.Run("/ppp/secret/print")
	if err != nil {
		return SyncSummary{}, fmt.Errorf("baca ppp secret: %w", err)
	}

	profiles := make([]ProfileImport, 0, len(profileRows))
	for _, row := range profileRows {
		name := strings.TrimSpace(row["name"])
		if name == "" || name == "default" || name == "default-encryption" {
			continue
		}
		price, hasPrice := ParseProfilePrice(name)
		profiles = append(profiles, ProfileImport{
			ExternalID: row[".id"],
			Name:       name,
			RateLimit:  row["rate-limit"],
			Price:      price,
			HasPrice:   hasPrice,
		})
	}

	secrets := make([]SecretImport, 0, len(secretRows))
	for _, row := range secretRows {
		if !strings.EqualFold(row["service"], "pppoe") && !strings.EqualFold(row["service"], "any") {
			continue
		}
		username := strings.TrimSpace(row["name"])
		if username == "" {
			continue
		}
		secrets = append(secrets, SecretImport{
			ExternalID:  row[".id"],
			Username:    username,
			Password:    row["password"],
			ProfileName: strings.TrimSpace(row["profile"]),
			Comment:     strings.TrimSpace(row["comment"]),
			Disabled:    row["disabled"] == "true" || row["disabled"] == "yes",
		})
	}

	encrypt := func(plaintext string) ([]byte, error) {
		return secretbox.Encrypt(service.encryptionKey, plaintext)
	}
	summary, err := service.repository.ApplySync(ctx, tenantID, stored.Config.ID, profiles, secrets, encrypt)
	if err != nil {
		return SyncSummary{}, fmt.Errorf("terapkan hasil sync: %w", err)
	}
	summary.ProfilesSeen = int64(len(profiles))
	summary.SecretsSeen = int64(len(secrets))
	_ = service.repository.RecordStatus(ctx, tenantID, stored.Config.ID, stored.Config.Identity, stored.Config.RouterOSVersion, "")
	return summary, nil
}

// CheckConnection memeriksa apakah pelanggan sedang online di router.
func (service *Service) CheckConnection(ctx context.Context, tenantID, customerID string) (ConnectionStatus, error) {
	if tenantID == "" || strings.TrimSpace(customerID) == "" {
		return ConnectionStatus{}, ErrInvalidInput
	}
	account, err := service.repository.AccountByCustomer(ctx, tenantID, customerID)
	if err != nil {
		if errors.Is(err, ErrNoAccount) {
			return ConnectionStatus{}, ErrNoAccount
		}
		return ConnectionStatus{}, fmt.Errorf("cari akun pppoe: %w", err)
	}
	client, _, err := service.connect(ctx, tenantID)
	if err != nil {
		return ConnectionStatus{}, err
	}
	defer client.Close()

	status := ConnectionStatus{Username: account.Username}
	active, err := client.Run("/ppp/active/print", "?name="+account.Username)
	if err != nil {
		return ConnectionStatus{}, fmt.Errorf("baca sesi aktif: %w", err)
	}
	if len(active) > 0 {
		status.Online = true
		status.Address = active[0]["address"]
		status.Uptime = active[0]["uptime"]
		status.CallerID = active[0]["caller-id"]
	}
	secretRows, err := client.Run("/ppp/secret/print", "?name="+account.Username)
	if err == nil && len(secretRows) > 0 {
		status.SecretDisabled = secretRows[0]["disabled"] == "true" || secretRows[0]["disabled"] == "yes"
	}
	return status, nil
}

// SetSecretDisabled menonaktifkan/mengaktifkan secret PPPoE milik sebuah
// layanan di router dan memutus sesi aktifnya saat isolir.
func (service *Service) SetSecretDisabled(ctx context.Context, tenantID, serviceID string, disabled bool) error {
	account, err := service.repository.AccountByService(ctx, tenantID, serviceID)
	if err != nil {
		if errors.Is(err, ErrNoAccount) {
			return nil // layanan tanpa akun PPPoE: cukup ubah status di billing
		}
		return fmt.Errorf("cari akun pppoe: %w", err)
	}
	client, _, err := service.connect(ctx, tenantID)
	if err != nil {
		if errors.Is(err, ErrNotConfigured) || errors.Is(err, ErrNoEncryption) {
			return nil // router belum dikonfigurasi: jangan blokir aksi billing
		}
		return err
	}
	defer client.Close()

	value := "no"
	if disabled {
		value = "yes"
	}
	target := account.ExternalID
	if target == "" {
		rows, err := client.Run("/ppp/secret/print", "?name="+account.Username)
		if err != nil || len(rows) == 0 {
			return fmt.Errorf("secret %s tidak ditemukan di router", account.Username)
		}
		target = rows[0][".id"]
	}
	if _, err := client.Run("/ppp/secret/set", "=.id="+target, "=disabled="+value); err != nil {
		return fmt.Errorf("ubah secret di router: %w", err)
	}
	if disabled {
		active, err := client.Run("/ppp/active/print", "?name="+account.Username)
		if err == nil {
			for _, session := range active {
				_, _ = client.Run("/ppp/active/remove", "=.id="+session[".id"])
			}
		}
	}
	return nil
}

// ProvisionPPPoE membuat akun PPPoE di router untuk layanan pelanggan
// yang baru dibuat, memakai profile mapping dari paketnya. Jika router
// belum dikonfigurasi atau paket belum dipetakan ke profil, method ini
// mengembalikan ErrNotConfigured tanpa memblokir pembuatan layanan.
func (service *Service) ProvisionPPPoE(ctx context.Context, tenantID string, input ProvisionInput) (ProvisionResult, error) {
	if err := service.requireKey(); err != nil {
		return ProvisionResult{}, err
	}
	if tenantID == "" || input.ServiceID == "" || input.PackageID == "" {
		return ProvisionResult{}, ErrInvalidInput
	}
	mapping, err := service.repository.ProfileMappingByPackage(ctx, tenantID, input.PackageID)
	if err != nil {
		if errors.Is(err, ErrNotConfigured) {
			return ProvisionResult{}, ErrNotConfigured
		}
		return ProvisionResult{}, fmt.Errorf("cari mapping profil paket: %w", err)
	}

	username := strings.ToLower(input.CustomerNumber) + "-pppoe"
	password, err := newRandomPassword()
	if err != nil {
		return ProvisionResult{}, err
	}

	client, stored, err := service.connect(ctx, tenantID)
	if err != nil {
		return ProvisionResult{}, err
	}
	defer client.Close()

	args := []string{
		"/ppp/secret/add",
		"=service=pppoe",
		"=name=" + username,
		"=password=" + password,
		"=profile=" + mapping.ProfileName,
		"=comment=billing " + input.CustomerNumber,
	}
	rows, err := client.Run(args...)
	if err != nil {
		return ProvisionResult{}, fmt.Errorf("tambah secret pppoe: %w", err)
	}
	externalID := ""
	if len(rows) > 0 {
		externalID = rows[0]["ret"]
	}
	if externalID == "" {
		found, err := client.Run("/ppp/secret/print", "?name="+username)
		if err == nil && len(found) > 0 {
			externalID = found[0][".id"]
		}
	}

	encrypt := func(plaintext string) ([]byte, error) {
		return secretbox.Encrypt(service.encryptionKey, plaintext)
	}
	if err := service.repository.CreateAccount(ctx, tenantID, stored.Config.ID, externalID, username, password, encrypt); err != nil {
		return ProvisionResult{}, fmt.Errorf("simpan akun pppoe: %w", err)
	}
	return ProvisionResult{
		Username:   username,
		Password:   password,
		Profile:    mapping.ProfileName,
		ExternalID: externalID,
	}, nil
}

// newRandomPassword menghasilkan password acak 16 karakter yang aman.
func newRandomPassword() (string, error) {
	const alphabet = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKMNPQRSTUVWXYZ23456789"
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate password: %w", err)
	}
	out := make([]byte, len(raw))
	for i, b := range raw {
		out[i] = alphabet[int(b)%len(alphabet)]
	}
	return string(out), nil
}

func truncateError(err error) string {
	message := err.Error()
	if len(message) > 500 {
		return message[:500]
	}
	return message
}
