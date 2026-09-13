package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"isp-billing/internal/auth"
)

type AuthRepository struct {
	pool *pgxpool.Pool
}

func NewAuthRepository(pool *pgxpool.Pool) *AuthRepository {
	return &AuthRepository{pool: pool}
}

func (repository *AuthRepository) FindUserByUsername(ctx context.Context, username string) (auth.User, error) {
	var user auth.User
	err := repository.pool.QueryRow(ctx, `
		SELECT id, tenant_id, username, password_hash, role_code, active
		FROM users
		WHERE normalized_username = $1
	`, username).Scan(&user.ID, &user.TenantID, &user.Username, &user.PasswordHash, &user.Role, &user.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.User{}, auth.ErrNotFound
	}
	return user, err
}

func (repository *AuthRepository) CountRecentFailures(ctx context.Context, username, ipAddress string, since time.Time) (int, error) {
	var count int
	err := repository.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM login_attempts
		WHERE normalized_username = $1
		  AND ip_address = $2::inet
		  AND succeeded = false
		  AND attempted_at >= $3
	`, username, ipAddress, since).Scan(&count)
	return count, err
}

func (repository *AuthRepository) RecordLoginAttempt(ctx context.Context, username, ipAddress string, succeeded bool) error {
	_, err := repository.pool.Exec(ctx, `
		INSERT INTO login_attempts (normalized_username, ip_address, succeeded)
		VALUES ($1, $2::inet, $3)
	`, username, ipAddress, succeeded)
	return err
}

func (repository *AuthRepository) CreateSession(ctx context.Context, session auth.NewSession) error {
	_, err := repository.pool.Exec(ctx, `
		INSERT INTO user_sessions (user_id, token_hash, csrf_hash, expires_at, ip_address, user_agent)
		VALUES ($1, $2, $3, $4, $5::inet, $6)
	`, session.UserID, session.TokenHash, session.CSRFHash, session.ExpiresAt, session.IPAddress, session.UserAgent)
	return err
}

func (repository *AuthRepository) FindSession(ctx context.Context, tokenHash []byte, now, idleCutoff time.Time) (auth.Principal, error) {
	var principal auth.Principal
	err := repository.pool.QueryRow(ctx, `
		UPDATE user_sessions AS session
		SET last_seen_at = $2
		FROM users AS app_user
		WHERE session.user_id = app_user.id
		  AND session.token_hash = $1
		  AND session.revoked_at IS NULL
		  AND session.expires_at > $2
		  AND session.last_seen_at > $3
		  AND app_user.active = true
		RETURNING app_user.id, app_user.tenant_id, app_user.username, app_user.role_code, session.csrf_hash
	`, tokenHash, now, idleCutoff).Scan(
		&principal.UserID,
		&principal.TenantID,
		&principal.Username,
		&principal.Role,
		&principal.CSRFHash,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.Principal{}, auth.ErrNotFound
	}
	return principal, err
}

func (repository *AuthRepository) DeleteSession(ctx context.Context, tokenHash []byte) error {
	_, err := repository.pool.Exec(ctx, `
		UPDATE user_sessions
		SET revoked_at = now()
		WHERE token_hash = $1 AND revoked_at IS NULL
	`, tokenHash)
	return err
}

func (repository *AuthRepository) FindUserByID(ctx context.Context, userID string) (auth.User, error) {
	var user auth.User
	err := repository.pool.QueryRow(ctx, `
		SELECT id::text, tenant_id::text, username, password_hash, role_code::text, active
		FROM users
		WHERE id = $1
	`, userID).Scan(&user.ID, &user.TenantID, &user.Username, &user.PasswordHash, &user.Role, &user.Active)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return auth.User{}, auth.ErrNotFound
		}
		return auth.User{}, err
	}
	return user, nil
}

func (repository *AuthRepository) UpdateUserPassword(ctx context.Context, userID, passwordHash string) error {
	_, err := repository.pool.Exec(ctx, `
		UPDATE users SET password_hash = $2 WHERE id = $1
	`, userID, passwordHash)
	return err
}

func (repository *AuthRepository) DeleteOtherSessions(ctx context.Context, userID string, keepTokenHash []byte) error {
	_, err := repository.pool.Exec(ctx, `
		UPDATE user_sessions
		SET revoked_at = now()
		WHERE user_id = $1 AND token_hash <> $2 AND revoked_at IS NULL
	`, userID, keepTokenHash)
	return err
}