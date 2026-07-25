package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}
	migrationsDirectory := os.Getenv("MIGRATIONS_DIR")
	if migrationsDirectory == "" {
		migrationsDirectory = "migrations"
	}
	runtimeUser := os.Getenv("DATABASE_RUNTIME_USER")
	if runtimeUser == "" {
		log.Fatal("DATABASE_RUNTIME_USER is required")
	}
	runtimePassword := os.Getenv("DATABASE_RUNTIME_PASSWORD")
	if runtimePassword == "" {
		log.Fatal("DATABASE_RUNTIME_PASSWORD is required")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer pool.Close()

	connection, err := pool.Acquire(ctx)
	if err != nil {
		log.Fatalf("acquire migration connection: %v", err)
	}
	defer connection.Release()
	lockContext, cancelLock := context.WithTimeout(ctx, 2*time.Minute)
	if _, err := connection.Exec(lockContext, "SELECT pg_advisory_lock(hashtext('isp_billing_migrations'))"); err != nil {
		cancelLock()
		log.Fatalf("lock migrations: %v", err)
	}
	cancelLock()
	defer func() {
		_, _ = connection.Exec(ctx, "SELECT pg_advisory_unlock(hashtext('isp_billing_migrations'))")
	}()

	if _, err := connection.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version text PRIMARY KEY,
			checksum text,
			applied_at timestamptz NOT NULL DEFAULT now()
		);
		ALTER TABLE schema_migrations ADD COLUMN IF NOT EXISTS checksum text;
	`); err != nil {
		log.Fatalf("create migration table: %v", err)
	}

	files, err := migrationFiles(migrationsDirectory)
	if err != nil {
		log.Fatal(err)
	}
	if err := verifyAppliedMigrations(ctx, connection, files); err != nil {
		log.Fatal(err)
	}
	for _, file := range files {
		if err := applyMigration(ctx, connection, file); err != nil {
			log.Fatal(err)
		}
	}
	if _, err := connection.Exec(ctx, "ALTER TABLE schema_migrations ALTER COLUMN checksum SET NOT NULL"); err != nil {
		log.Fatalf("require migration checksums: %v", err)
	}
	if err := ensureRuntimeRole(ctx, connection, runtimeUser, runtimePassword); err != nil {
		log.Fatal(err)
	}
	if err := grantRuntimePrivileges(ctx, connection, runtimeUser); err != nil {
		log.Fatal(err)
	}
}

func verifyAppliedMigrations(ctx context.Context, connection *pgxpool.Conn, files []string) error {
	checksums := make(map[string]string, len(files))
	for _, file := range files {
		checksum, err := migrationChecksum(file)
		if err != nil {
			return err
		}
		checksums[filepath.Base(file)] = checksum
	}

	rows, err := connection.Query(ctx, "SELECT version, checksum IS NULL, COALESCE(checksum, '') FROM schema_migrations ORDER BY version")
	if err != nil {
		return fmt.Errorf("list applied migrations: %w", err)
	}
	defer rows.Close()

	var applied []appliedMigration
	for rows.Next() {
		var migration appliedMigration
		if err := rows.Scan(&migration.version, &migration.checksumMissing, &migration.checksum); err != nil {
			return fmt.Errorf("scan applied migration: %w", err)
		}
		applied = append(applied, migration)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate applied migrations: %w", err)
	}
	rows.Close()
	legacyVersions, err := validateAppliedMigrationChecksums(checksums, applied)
	if err != nil {
		return err
	}
	for _, version := range legacyVersions {
		result, err := connection.Exec(ctx,
			"UPDATE schema_migrations SET checksum = $2 WHERE version = $1 AND checksum IS NULL",
			version,
			checksums[version],
		)
		if err != nil {
			return fmt.Errorf("baseline migration checksum %s: %w", version, err)
		}
		if result.RowsAffected() != 1 {
			return fmt.Errorf("baseline migration checksum %s: expected one updated row", version)
		}
		log.Printf("baselined checksum for legacy migration %s", version)
	}
	return nil
}

type appliedMigration struct {
	version         string
	checksum        string
	checksumMissing bool
}

func validateAppliedMigrationChecksums(available map[string]string, applied []appliedMigration) ([]string, error) {
	var legacyVersions []string
	for _, migration := range applied {
		currentChecksum, exists := available[migration.version]
		if !exists {
			return nil, fmt.Errorf("applied migration %s is missing from the release", migration.version)
		}
		if migration.checksumMissing {
			legacyVersions = append(legacyVersions, migration.version)
			continue
		}
		if migration.checksum != currentChecksum {
			return nil, fmt.Errorf("applied migration %s checksum mismatch; migrations are append-only", migration.version)
		}
	}
	return legacyVersions, nil
}

func migrationChecksum(file string) (string, error) {
	sqlBytes, err := os.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("read migration %s: %w", filepath.Base(file), err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(sqlBytes)), nil
}

func ensureRuntimeRole(ctx context.Context, connection *pgxpool.Conn, runtimeUser, runtimePassword string) error {
	var currentUser string
	if err := connection.QueryRow(ctx, "SELECT current_user").Scan(&currentUser); err != nil {
		return fmt.Errorf("read migration database user: %w", err)
	}
	if runtimeUser == currentUser {
		return fmt.Errorf("runtime database user must differ from migration owner %q", currentUser)
	}

	var statement string
	if err := connection.QueryRow(ctx, `
		SELECT CASE
			WHEN EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)
			THEN format(
				'ALTER ROLE %I WITH LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD %L',
				$1,
				$2
			)
			ELSE format(
				'CREATE ROLE %I WITH LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD %L',
				$1,
				$2
			)
		END
	`, runtimeUser, runtimePassword).Scan(&statement); err != nil {
		return fmt.Errorf("prepare runtime database role: %w", err)
	}
	if _, err := connection.Exec(ctx, statement); err != nil {
		return fmt.Errorf("ensure runtime database role: %w", err)
	}
	log.Printf("runtime database role ensured for %s", runtimeUser)
	return nil
}

func migrationFiles(directory string) ([]string, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".up.sql") {
			files = append(files, filepath.Join(directory, entry.Name()))
		}
	}
	sort.Strings(files)
	return files, nil
}

func applyMigration(ctx context.Context, connection *pgxpool.Conn, file string) error {
	version := filepath.Base(file)
	var alreadyApplied bool
	if err := connection.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)",
		version,
	).Scan(&alreadyApplied); err != nil {
		return fmt.Errorf("check migration %s: %w", version, err)
	}
	if alreadyApplied {
		log.Printf("skip %s", version)
		return nil
	}

	sqlBytes, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("read migration %s: %w", version, err)
	}
	checksum := fmt.Sprintf("%x", sha256.Sum256(sqlBytes))
	tx, err := connection.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", version, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
		return fmt.Errorf("apply migration %s: %w", version, err)
	}
	if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version, checksum) VALUES ($1, $2)", version, checksum); err != nil {
		return fmt.Errorf("record migration %s: %w", version, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration %s: %w", version, err)
	}
	log.Printf("applied %s", version)
	return nil
}

func grantRuntimePrivileges(ctx context.Context, connection *pgxpool.Conn, runtimeUser string) error {
	role := pgx.Identifier{runtimeUser}.Sanitize()
	statements := []string{
		"GRANT USAGE ON SCHEMA public TO " + role,
		"GRANT SELECT ON ALL TABLES IN SCHEMA public TO " + role,
		"GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO " + role,
		"GRANT INSERT, UPDATE ON tenants, users, user_sessions, tenant_counters, customers, routers, ppp_profiles, packages, package_prices, package_router_profiles, services, pppoe_accounts, sync_jobs, sync_conflicts, provisioning_commands, invoices, cash_shifts, payments TO " + role,
		"GRANT INSERT ON login_attempts, audit_logs, invoice_lines, payment_allocations, receipts TO " + role,
		"REVOKE DELETE ON ALL TABLES IN SCHEMA public FROM " + role,
		"REVOKE UPDATE, DELETE ON audit_logs, invoice_lines, payment_allocations, receipts FROM " + role,
		"REVOKE INSERT, UPDATE, DELETE ON schema_migrations FROM " + role,
		"ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO " + role,
		"ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO " + role,
	}
	for _, statement := range statements {
		if _, err := connection.Exec(ctx, statement); err != nil {
			return fmt.Errorf("grant runtime database privileges: %w", err)
		}
	}
	log.Printf("runtime privileges granted to %s", runtimeUser)
	return nil
}
