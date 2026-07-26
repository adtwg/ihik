package router

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidInput  = errors.New("invalid router input")
	ErrNotConfigured = errors.New("router is not configured")
	ErrNoEncryption  = errors.New("encryption key is not configured")
	ErrUnreachable   = errors.New("router is unreachable")
	ErrNoAccount     = errors.New("customer has no pppoe account")
)

type Config struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Host            string     `json:"host"`
	APIPort         int        `json:"api_port"`
	UseTLS          bool       `json:"use_tls"`
	APIUsername     string     `json:"api_username"`
	RouterOSVersion string     `json:"routeros_version,omitempty"`
	Identity        string     `json:"identity,omitempty"`
	LastConnectedAt *time.Time `json:"last_connected_at,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
	Configured      bool       `json:"configured"`
}

type SaveInput struct {
	Name        string `json:"name"`
	Host        string `json:"host"`
	APIPort     int    `json:"api_port"`
	UseTLS      bool   `json:"use_tls"`
	APIUsername string `json:"api_username"`
	APIPassword string `json:"api_password"`
}

type TestResult struct {
	Identity        string `json:"identity"`
	RouterOSVersion string `json:"routeros_version"`
	SecretCount     int    `json:"secret_count"`
	ProfileCount    int    `json:"profile_count"`
}

type ProfileImport struct {
	ExternalID string
	Name       string
	RateLimit  string
	Price      float64
	HasPrice   bool
}

type SecretImport struct {
	ExternalID  string
	Username    string
	Password    string
	ProfileName string
	Comment     string
	Disabled    bool
}

type SyncSummary struct {
	ProfilesSeen        int64 `json:"profiles_seen"`
	PackagesCreated     int64 `json:"packages_created"`
	PackagesUpdated     int64 `json:"packages_updated"`
	PackagesNoPrice     int64 `json:"packages_without_price"`
	SecretsSeen         int64 `json:"secrets_seen"`
	SecretsSkipped      int64 `json:"secrets_skipped"`
	CustomersCreated    int64 `json:"customers_created"`
	AccountsLinked      int64 `json:"accounts_linked"`
	AccountsUpdated     int64 `json:"accounts_updated"`
	ServicesIsolated    int64 `json:"services_isolated"`
	ServicesRestored    int64 `json:"services_restored"`
}

type ConnectionStatus struct {
	Online         bool   `json:"online"`
	Username       string `json:"username"`
	Address        string `json:"address,omitempty"`
	Uptime         string `json:"uptime,omitempty"`
	CallerID       string `json:"caller_id,omitempty"`
	SecretDisabled bool   `json:"secret_disabled"`
}

type StoredCredentials struct {
	Config             Config
	PasswordCiphertext []byte
	KeyVersion         int
}

type AccountRef struct {
	Username   string
	ExternalID string
	ServiceID  string
}

type Repository interface {
	Get(ctx context.Context, tenantID string) (StoredCredentials, error)
	Save(ctx context.Context, tenantID string, input SaveInput, ciphertext []byte, keyVersion int) (Config, error)
	RecordStatus(ctx context.Context, tenantID, routerID, identity, version, lastError string) error
	ApplySync(ctx context.Context, tenantID, routerID string, profiles []ProfileImport, secrets []SecretImport, encrypt func(string) ([]byte, error)) (SyncSummary, error)
	AccountByCustomer(ctx context.Context, tenantID, customerID string) (AccountRef, error)
	AccountByService(ctx context.Context, tenantID, serviceID string) (AccountRef, error)
}
