package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"isp-billing/internal/router"
	"isp-billing/internal/secretbox"
)

type RouterRepository struct {
	pool *pgxpool.Pool
}

func NewRouterRepository(pool *pgxpool.Pool) *RouterRepository {
	return &RouterRepository{pool: pool}
}

func (repository *RouterRepository) Get(ctx context.Context, tenantID string) (router.StoredCredentials, error) {
	var stored router.StoredCredentials
	var lastError *string
	var identity, version *string
	err := repository.pool.QueryRow(ctx, `
		SELECT id, name, host, api_port, use_tls, api_username,
		       api_password_ciphertext, encryption_key_version,
		       routeros_version, router_identity, last_connected_at, last_error
		FROM routers
		WHERE tenant_id = $1 AND archived_at IS NULL
	`, tenantID).Scan(
		&stored.Config.ID,
		&stored.Config.Name,
		&stored.Config.Host,
		&stored.Config.APIPort,
		&stored.Config.UseTLS,
		&stored.Config.APIUsername,
		&stored.PasswordCiphertext,
		&stored.KeyVersion,
		&version,
		&identity,
		&stored.Config.LastConnectedAt,
		&lastError,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return router.StoredCredentials{}, router.ErrNotConfigured
		}
		return router.StoredCredentials{}, err
	}
	if version != nil {
		stored.Config.RouterOSVersion = *version
	}
	if identity != nil {
		stored.Config.Identity = *identity
	}
	if lastError != nil {
		stored.Config.LastError = *lastError
	}
	stored.Config.Configured = true
	return stored, nil
}

func (repository *RouterRepository) Save(ctx context.Context, tenantID string, input router.SaveInput, ciphertext []byte, keyVersion int) (router.Config, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return router.Config{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var existingID string
	err = tx.QueryRow(ctx, `
		SELECT id FROM routers WHERE tenant_id = $1 AND archived_at IS NULL FOR UPDATE
	`, tenantID).Scan(&existingID)
	newRouter := errors.Is(err, pgx.ErrNoRows)
	if err != nil && !newRouter {
		return router.Config{}, err
	}
	if newRouter && len(ciphertext) == 0 {
		return router.Config{}, router.ErrInvalidInput
	}

	if newRouter {
		err = tx.QueryRow(ctx, `
			INSERT INTO routers (tenant_id, name, host, api_port, use_tls, api_username, api_password_ciphertext, encryption_key_version, active)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, true)
			RETURNING id
		`, tenantID, input.Name, input.Host, input.APIPort, input.UseTLS, input.APIUsername, ciphertext, keyVersion).Scan(&existingID)
	} else if len(ciphertext) > 0 {
		_, err = tx.Exec(ctx, `
			UPDATE routers
			SET name = $3, host = $4, api_port = $5, use_tls = $6, api_username = $7,
			    api_password_ciphertext = $8, encryption_key_version = $9, updated_at = now()
			WHERE tenant_id = $1 AND id = $2
		`, tenantID, existingID, input.Name, input.Host, input.APIPort, input.UseTLS, input.APIUsername, ciphertext, keyVersion)
	} else {
		_, err = tx.Exec(ctx, `
			UPDATE routers
			SET name = $3, host = $4, api_port = $5, use_tls = $6, api_username = $7, updated_at = now()
			WHERE tenant_id = $1 AND id = $2
		`, tenantID, existingID, input.Name, input.Host, input.APIPort, input.UseTLS, input.APIUsername)
	}
	if err != nil {
		return router.Config{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return router.Config{}, err
	}
	stored, err := repository.Get(ctx, tenantID)
	if err != nil {
		return router.Config{}, err
	}
	return stored.Config, nil
}

func (repository *RouterRepository) RecordStatus(ctx context.Context, tenantID, routerID, identity, version, lastError string) error {
	if lastError == "" {
		_, err := repository.pool.Exec(ctx, `
			UPDATE routers
			SET router_identity = NULLIF($3, ''), routeros_version = NULLIF($4, ''),
			    last_connected_at = now(), last_error = NULL, updated_at = now()
			WHERE tenant_id = $1 AND id = $2
		`, tenantID, routerID, identity, version)
		return err
	}
	_, err := repository.pool.Exec(ctx, `
		UPDATE routers SET last_error = $3, updated_at = now()
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, routerID, lastError)
	return err
}

// ApplySync menyelaraskan profile & secret dari router ke billing dalam satu
// transaksi: profile → paket (+harga dari nama profil), secret → pelanggan +
// layanan + akun PPPoE. Idempoten: entri yang sudah tertaut hanya diperbarui.
func (repository *RouterRepository) ApplySync(ctx context.Context, tenantID, routerID string, profiles []router.ProfileImport, secrets []router.SecretImport, encrypt func(string) ([]byte, error)) (router.SyncSummary, error) {
	var summary router.SyncSummary
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return summary, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	packageByProfile := make(map[string]string, len(profiles))
	for _, profile := range profiles {
		code := normalizePackageCode(profile.Name)
		var packageID string
		var created bool
		err := tx.QueryRow(ctx, `
			INSERT INTO packages (tenant_id, code, name)
			VALUES ($1, $2, $3)
			ON CONFLICT (tenant_id, code) DO UPDATE SET updated_at = now()
			RETURNING id, (xmax = 0)
		`, tenantID, code, profile.Name).Scan(&packageID, &created)
		if err != nil {
			return summary, fmt.Errorf("upsert paket %s: %w", profile.Name, err)
		}
		packageByProfile[strings.ToLower(profile.Name)] = packageID
		if created {
			summary.PackagesCreated++
		}

		if !profile.HasPrice {
			summary.PackagesNoPrice++
			continue
		}
		var priceChanged bool
		err = tx.QueryRow(ctx, `
			SELECT NOT EXISTS (
				SELECT 1 FROM package_prices
				WHERE tenant_id = $1 AND package_id = $2
				  AND valid_from <= CURRENT_DATE
				  AND (valid_until IS NULL OR valid_until >= CURRENT_DATE)
				  AND amount = round($3::numeric, 2)
			)
		`, tenantID, packageID, profile.Price).Scan(&priceChanged)
		if err != nil {
			return summary, err
		}
		if priceChanged {
			if _, err := tx.Exec(ctx, `
				UPDATE package_prices SET valid_until = CURRENT_DATE - 1
				WHERE tenant_id = $1 AND package_id = $2
				  AND valid_from < CURRENT_DATE
				  AND (valid_until IS NULL OR valid_until >= CURRENT_DATE)
			`, tenantID, packageID); err != nil {
				return summary, err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO package_prices (tenant_id, package_id, amount, tax_percent, valid_from)
				VALUES ($1, $2, round($3::numeric, 2), 0, CURRENT_DATE)
				ON CONFLICT (tenant_id, package_id, valid_from)
				DO UPDATE SET amount = excluded.amount
			`, tenantID, packageID, profile.Price); err != nil {
				return summary, err
			}
			if !created {
				summary.PackagesUpdated++
			}
		}
	}

	for _, secret := range secrets {
		packageID, hasPackage := packageByProfile[strings.ToLower(secret.ProfileName)]
		if !hasPackage {
			summary.SecretsSkipped++
			continue
		}

		ciphertext, err := encrypt(secret.Password)
		if err != nil {
			return summary, fmt.Errorf("enkripsi password %s: %w", secret.Username, err)
		}

		var accountID, serviceID string
		err = tx.QueryRow(ctx, `
			SELECT id, service_id FROM pppoe_accounts
			WHERE tenant_id = $1 AND router_id = $2 AND username = $3
		`, tenantID, routerID, secret.Username).Scan(&accountID, &serviceID)
		exists := !errors.Is(err, pgx.ErrNoRows)
		if err != nil && exists {
			return summary, err
		}

		if exists {
			if _, err := tx.Exec(ctx, `
				UPDATE pppoe_accounts
				SET external_id = $4, password_ciphertext = $5, encryption_key_version = $6,
				    sync_status = 'linked', last_synced_at = now(), version = version + 1, updated_at = now()
				WHERE tenant_id = $1 AND router_id = $2 AND username = $3
			`, tenantID, routerID, secret.Username, secret.ExternalID, ciphertext, 1); err != nil {
				return summary, err
			}
			// Selaraskan paket layanan bila profil di router berubah.
			if _, err := tx.Exec(ctx, `
				UPDATE services SET package_id = $3, updated_at = now()
				WHERE tenant_id = $1 AND id = $2 AND package_id <> $3 AND archived_at IS NULL
			`, tenantID, serviceID, packageID); err != nil {
				return summary, err
			}
			summary.AccountsUpdated++
			if err := repository.alignServiceStatus(ctx, tx, tenantID, serviceID, secret.Disabled, &summary); err != nil {
				return summary, err
			}
			continue
		}

		customerName := secret.Comment
		if customerName == "" {
			customerName = secret.Username
		}
		if len(customerName) > 200 {
			customerName = customerName[:200]
		}

		var customerID string
		err = tx.QueryRow(ctx, `
			SELECT id FROM customers
			WHERE tenant_id = $1 AND lower(name) = lower($2) AND archived_at IS NULL
			ORDER BY created_at LIMIT 1
		`, tenantID, customerName).Scan(&customerID)
		if errors.Is(err, pgx.ErrNoRows) {
			var sequence int64
			if err := tx.QueryRow(ctx, `
				INSERT INTO tenant_counters (tenant_id, counter_key, counter_value)
				VALUES ($1, 'customer_number', 1)
				ON CONFLICT (tenant_id, counter_key)
				DO UPDATE SET counter_value = tenant_counters.counter_value + 1
				RETURNING counter_value
			`, tenantID).Scan(&sequence); err != nil {
				return summary, err
			}
			if err := tx.QueryRow(ctx, `
				INSERT INTO customers (tenant_id, customer_number, name)
				VALUES ($1, $2, $3)
				RETURNING id
			`, tenantID, fmt.Sprintf("CUST-%06d", sequence), customerName).Scan(&customerID); err != nil {
				return summary, err
			}
			summary.CustomersCreated++
		} else if err != nil {
			return summary, err
		}

		var serviceSequence int64
		if err := tx.QueryRow(ctx, `
			INSERT INTO tenant_counters (tenant_id, counter_key, counter_value)
			VALUES ($1, 'service_number', 1)
			ON CONFLICT (tenant_id, counter_key)
			DO UPDATE SET counter_value = tenant_counters.counter_value + 1
			RETURNING counter_value
		`, tenantID).Scan(&serviceSequence); err != nil {
			return summary, err
		}
		status := "active"
		if secret.Disabled {
			status = "isolated"
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO services (tenant_id, customer_id, router_id, package_id, service_number, status, activated_at, isolated_at)
			VALUES ($1, $2, $3, $4, $5, $6::service_status, now(), CASE WHEN $6 = 'isolated' THEN now() END)
			RETURNING id
		`, tenantID, customerID, routerID, packageID, fmt.Sprintf("SVC-%06d", serviceSequence), status).Scan(&serviceID); err != nil {
			return summary, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO pppoe_accounts (tenant_id, service_id, router_id, external_id, username, password_ciphertext, encryption_key_version, sync_status, last_synced_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 'linked', now())
		`, tenantID, serviceID, routerID, secret.ExternalID, secret.Username, ciphertext, 1); err != nil {
			return summary, err
		}
		summary.AccountsLinked++
	}

	if err := tx.Commit(ctx); err != nil {
		return summary, err
	}
	return summary, nil
}

func (repository *RouterRepository) alignServiceStatus(ctx context.Context, tx pgx.Tx, tenantID, serviceID string, disabled bool, summary *router.SyncSummary) error {
	if disabled {
		result, err := tx.Exec(ctx, `
			UPDATE services SET status = 'isolated', isolated_at = now(), updated_at = now()
			WHERE tenant_id = $1 AND id = $2 AND archived_at IS NULL AND status = 'active'
		`, tenantID, serviceID)
		if err != nil {
			return err
		}
		summary.ServicesIsolated += result.RowsAffected()
		return nil
	}
	result, err := tx.Exec(ctx, `
		UPDATE services SET status = 'active', isolated_at = NULL, updated_at = now()
		WHERE tenant_id = $1 AND id = $2 AND archived_at IS NULL AND status = 'isolated'
	`, tenantID, serviceID)
	if err != nil {
		return err
	}
	summary.ServicesRestored += result.RowsAffected()
	return nil
}

func (repository *RouterRepository) AccountByCustomer(ctx context.Context, tenantID, customerID string) (router.AccountRef, error) {
	var account router.AccountRef
	var externalID *string
	err := repository.pool.QueryRow(ctx, `
		SELECT pa.username, pa.external_id, pa.service_id
		FROM pppoe_accounts pa
		JOIN services s ON s.tenant_id = pa.tenant_id AND s.id = pa.service_id
		WHERE pa.tenant_id = $1 AND s.customer_id = $2 AND pa.archived_at IS NULL AND s.archived_at IS NULL
		ORDER BY pa.created_at DESC
		LIMIT 1
	`, tenantID, customerID).Scan(&account.Username, &externalID, &account.ServiceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || isInvalidUUID(err) {
			return router.AccountRef{}, router.ErrNoAccount
		}
		return router.AccountRef{}, err
	}
	if externalID != nil {
		account.ExternalID = *externalID
	}
	return account, nil
}

func (repository *RouterRepository) AccountByService(ctx context.Context, tenantID, serviceID string) (router.AccountRef, error) {
	var account router.AccountRef
	var externalID *string
	err := repository.pool.QueryRow(ctx, `
		SELECT username, external_id, service_id
		FROM pppoe_accounts
		WHERE tenant_id = $1 AND service_id = $2 AND archived_at IS NULL
	`, tenantID, serviceID).Scan(&account.Username, &externalID, &account.ServiceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || isInvalidUUID(err) {
			return router.AccountRef{}, router.ErrNoAccount
		}
		return router.AccountRef{}, err
	}
	if externalID != nil {
		account.ExternalID = *externalID
	}
	return account, nil
}

func normalizePackageCode(name string) string {
	upper := strings.ToUpper(strings.TrimSpace(name))
	var builder strings.Builder
	for _, character := range upper {
		switch {
		case character >= 'A' && character <= 'Z', character >= '0' && character <= '9':
			builder.WriteRune(character)
		case character == '-' || character == '_':
			builder.WriteRune(character)
		default:
			builder.WriteRune('-')
		}
	}
	code := strings.Trim(builder.String(), "-_")
	if code == "" {
		code = "PROFIL"
	}
	if len(code) > 50 {
		code = code[:50]
	}
	return code
}

var _ router.Repository = (*RouterRepository)(nil)

func (repository *RouterRepository) ProfileMappingByPackage(ctx context.Context, tenantID, packageID string) (router.ProfileMapping, error) {
	var mapping router.ProfileMapping
	err := repository.pool.QueryRow(ctx, `
		SELECT m.router_id::text, p.external_id, p.name
		FROM package_router_profiles m
		JOIN ppp_profiles p ON p.tenant_id = m.tenant_id AND p.router_id = m.router_id AND p.id = m.normal_profile_id
		WHERE m.tenant_id = $1 AND m.package_id = $2
		LIMIT 1
	`, tenantID, packageID).Scan(&mapping.RouterID, &mapping.ProfileExternalID, &mapping.ProfileName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return router.ProfileMapping{}, router.ErrNotConfigured
		}
		return router.ProfileMapping{}, err
	}
	return mapping, nil
}

func (repository *RouterRepository) CreateAccount(ctx context.Context, tenantID, routerID, externalID, username, password string, encrypt func(string) ([]byte, error)) error {
	ciphertext, err := encrypt(password)
	if err != nil {
		return fmt.Errorf("enkripsi password pppoe: %w", err)
	}
	_, err = repository.pool.Exec(ctx, `
		INSERT INTO pppoe_accounts (tenant_id, router_id, external_id, username, password_ciphertext, encryption_key_version, sync_status, last_synced_at)
		VALUES ($1, $2, NULLIF($3, ''), $4, $5, $6, 'linked', now())
	`, tenantID, routerID, externalID, username, ciphertext, secretbox.KeyVersion)
	return err
}
