package main

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"isp-billing/internal/auth"
)

const (
	adminMissingExitCode            = 3
	adminAlreadyExistsExitCode      = 4
	adminCredentialMismatchExitCode = 5
)

type commandMode string

const (
	modeCreate commandMode = "create"
	modeCheck  commandMode = "check"
	modeVerify commandMode = "verify"
)

func main() {
	mode, err := parseMode(os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer pool.Close()

	if mode == modeCheck {
		checkSuperAdmin(ctx, pool)
		return
	}

	username := strings.TrimSpace(os.Getenv("BOOTSTRAP_ADMIN_USERNAME"))
	password := os.Getenv("BOOTSTRAP_ADMIN_PASSWORD")
	if username == "" || password == "" {
		log.Fatal("BOOTSTRAP_ADMIN_USERNAME and BOOTSTRAP_ADMIN_PASSWORD are required")
	}
	if mode == modeVerify {
		verifySuperAdmin(ctx, pool, username, password)
		return
	}

	createSuperAdmin(ctx, pool, username, password)
}

func createSuperAdmin(ctx context.Context, pool *pgxpool.Pool, username, password string) {
	normalizedUsername := strings.ToLower(username)
	tx, err := pool.Begin(ctx)
	if err != nil {
		log.Fatalf("begin bootstrap transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	lockContext, cancelLock := context.WithTimeout(ctx, 2*time.Minute)
	if _, err := tx.Exec(lockContext, "SELECT pg_advisory_xact_lock(hashtext('isp_billing_bootstrap_admin'))"); err != nil {
		cancelLock()
		log.Fatalf("lock Super Admin bootstrap: %v", err)
	}
	cancelLock()

	var existingUsername, existingRole string
	err = tx.QueryRow(ctx, `
		SELECT username, role_code
		FROM users
		WHERE normalized_username = $1
	`, normalizedUsername).Scan(&existingUsername, &existingRole)
	if err == nil {
		if existingRole == "super_admin" {
			_ = tx.Rollback(ctx)
			log.Printf("Super Admin already exists: %s", existingUsername)
			os.Exit(adminAlreadyExistsExitCode)
		}
		log.Fatalf("username %q already belongs to role %s", username, existingRole)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		log.Fatalf("check bootstrap username: %v", err)
	}

	err = tx.QueryRow(ctx, `
		SELECT username
		FROM users
		WHERE role_code = 'super_admin'
		ORDER BY created_at
		LIMIT 1
	`).Scan(&existingUsername)
	if err == nil {
		_ = tx.Rollback(ctx)
		log.Printf("Super Admin bootstrap already completed by %s", existingUsername)
		os.Exit(adminAlreadyExistsExitCode)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		log.Fatalf("check existing Super Admin: %v", err)
	}

	passwordHash, err := auth.HashPassword(password)
	if err != nil {
		log.Fatalf("hash password: %v", err)
	}

	var userID string
	err = tx.QueryRow(ctx, `
		INSERT INTO users (username, normalized_username, password_hash, role_code)
		VALUES ($1, $2, $3, 'super_admin')
		RETURNING id
	`, username, normalizedUsername, passwordHash).Scan(&userID)
	if err != nil {
		log.Fatalf("create Super Admin: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		log.Fatalf("commit Super Admin: %v", err)
	}
	log.Printf("Super Admin created with id %s", userID)
}

func checkSuperAdmin(ctx context.Context, pool *pgxpool.Pool) {
	var username string
	err := pool.QueryRow(ctx, `
		SELECT username
		FROM users
		WHERE role_code = 'super_admin'
		ORDER BY created_at
		LIMIT 1
	`).Scan(&username)
	if errors.Is(err, pgx.ErrNoRows) {
		log.Print("Super Admin has not been created")
		os.Exit(adminMissingExitCode)
	}
	if err != nil {
		log.Fatalf("check Super Admin: %v", err)
	}
	log.Printf("Super Admin already exists: %s", username)
}

func verifySuperAdmin(ctx context.Context, pool *pgxpool.Pool, username, password string) {
	var passwordHash, roleCode string
	var active bool
	err := pool.QueryRow(ctx, `
		SELECT password_hash, role_code, active
		FROM users
		WHERE normalized_username = $1
	`, strings.ToLower(username)).Scan(&passwordHash, &roleCode, &active)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (roleCode != "super_admin" || !active || !auth.VerifyPassword(password, passwordHash))) {
		log.Print("Super Admin credentials do not match")
		os.Exit(adminCredentialMismatchExitCode)
	}
	if err != nil {
		log.Fatalf("verify Super Admin: %v", err)
	}
	log.Printf("Super Admin credentials verified: %s", username)
}

func parseMode(arguments []string) (commandMode, error) {
	if len(arguments) == 0 {
		return modeCreate, nil
	}
	if len(arguments) == 1 && arguments[0] == "--check" {
		return modeCheck, nil
	}
	if len(arguments) == 1 && arguments[0] == "--verify" {
		return modeVerify, nil
	}
	return "", errors.New("usage: bootstrap-admin [--check|--verify]")
}
