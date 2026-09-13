package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
)

const maxLoginFailures = 5

type Service struct {
	repository      Repository
	sessionLifetime time.Duration
	sessionIdleTime time.Duration
	dummyHash       string
	now             func() time.Time
}

type LoginResult struct {
	SessionToken string
	CSRFToken    string
	ExpiresAt    time.Time
	Principal    Principal
}

func NewService(repository Repository, sessionLifetime, sessionIdleTime time.Duration) (*Service, error) {
	dummyHash, err := HashPassword("not-a-real-password")
	if err != nil {
		return nil, fmt.Errorf("create dummy password hash: %w", err)
	}

	return &Service{
		repository:      repository,
		sessionLifetime: sessionLifetime,
		sessionIdleTime: sessionIdleTime,
		dummyHash:       dummyHash,
		now:             time.Now,
	}, nil
}

func (service *Service) Login(ctx context.Context, username, password, ipAddress, userAgent string) (LoginResult, error) {
	normalizedUsername := strings.ToLower(strings.TrimSpace(username))
	now := service.now().UTC()

	failures, err := service.repository.CountRecentFailures(ctx, normalizedUsername, ipAddress, now.Add(-15*time.Minute))
	if err != nil {
		return LoginResult{}, fmt.Errorf("count login failures: %w", err)
	}
	if failures >= maxLoginFailures {
		return LoginResult{}, ErrInvalidCredentials
	}

	user, findErr := service.repository.FindUserByUsername(ctx, normalizedUsername)
	passwordHash := service.dummyHash
	if findErr == nil {
		passwordHash = user.PasswordHash
	} else if !errors.Is(findErr, ErrNotFound) {
		return LoginResult{}, fmt.Errorf("find user: %w", findErr)
	}

	validPassword := VerifyPassword(password, passwordHash)
	if findErr != nil || !validPassword || !user.Active {
		_ = service.repository.RecordLoginAttempt(ctx, normalizedUsername, ipAddress, false)
		return LoginResult{}, ErrInvalidCredentials
	}

	sessionToken, sessionHash, err := newSecret()
	if err != nil {
		return LoginResult{}, err
	}
	csrfToken, csrfHash, err := newSecret()
	if err != nil {
		return LoginResult{}, err
	}

	expiresAt := now.Add(service.sessionLifetime)
	if err := service.repository.CreateSession(ctx, NewSession{
		UserID:    user.ID,
		TokenHash: sessionHash,
		CSRFHash:  csrfHash,
		ExpiresAt: expiresAt,
		IPAddress: ipAddress,
		UserAgent: userAgent,
	}); err != nil {
		return LoginResult{}, fmt.Errorf("create session: %w", err)
	}
	_ = service.repository.RecordLoginAttempt(ctx, normalizedUsername, ipAddress, true)

	return LoginResult{
		SessionToken: sessionToken,
		CSRFToken:    csrfToken,
		ExpiresAt:    expiresAt,
		Principal: Principal{
			UserID:   user.ID,
			TenantID: user.TenantID,
			Username: user.Username,
			Role:     user.Role,
		},
	}, nil
}

func (service *Service) Authenticate(ctx context.Context, rawToken string) (Principal, error) {
	tokenHash, err := hashSecret(rawToken)
	if err != nil {
		return Principal{}, ErrUnauthenticated
	}
	now := service.now().UTC()
	principal, err := service.repository.FindSession(ctx, tokenHash, now, now.Add(-service.sessionIdleTime))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Principal{}, ErrUnauthenticated
		}
		return Principal{}, fmt.Errorf("find session: %w", err)
	}
	principal.TokenHash = tokenHash
	return principal, nil
}

func (service *Service) Logout(ctx context.Context, tokenHash []byte) error {
	if err := service.repository.DeleteSession(ctx, tokenHash); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// ChangePassword memverifikasi password lama, menyimpan hash baru,
// lalu menghapus semua sesi lain milik user tersebut.
func (service *Service) ChangePassword(ctx context.Context, principal Principal, currentPassword, newPassword string) error {
	if len(newPassword) < 12 {
		return ErrInvalidInput
	}
	user, err := service.repository.FindUserByID(ctx, principal.UserID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrUnauthenticated
		}
		return fmt.Errorf("find user by id: %w", err)
	}
	if !VerifyPassword(currentPassword, user.PasswordHash) {
		return ErrWrongPassword
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return ErrInvalidInput
	}
	if err := service.repository.UpdateUserPassword(ctx, user.ID, hash); err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	if err := service.repository.DeleteOtherSessions(ctx, user.ID, principal.TokenHash); err != nil {
		return fmt.Errorf("revoke other sessions: %w", err)
	}
	return nil
}

func newSecret() (string, []byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("generate secure token: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(encoded))
	return encoded, hash[:], nil
}

func hashSecret(encoded string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(raw) != 32 {
		return nil, ErrUnauthenticated
	}
	hash := sha256.Sum256([]byte(encoded))
	return hash[:], nil
}