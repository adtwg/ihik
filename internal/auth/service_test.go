package auth

import (
	"context"
	"testing"
	"time"
)

type fakeRepository struct {
	user           User
	failures       int
	created        NewSession
	attemptSuccess []bool
}

func (repository *fakeRepository) FindUserByUsername(_ context.Context, username string) (User, error) {
	if repository.user.Username != username {
		return User{}, ErrNotFound
	}
	return repository.user, nil
}

func (repository *fakeRepository) CountRecentFailures(_ context.Context, _, _ string, _ time.Time) (int, error) {
	return repository.failures, nil
}

func (repository *fakeRepository) RecordLoginAttempt(_ context.Context, _ string, _ string, succeeded bool) error {
	repository.attemptSuccess = append(repository.attemptSuccess, succeeded)
	return nil
}

func (repository *fakeRepository) CreateSession(_ context.Context, session NewSession) error {
	repository.created = session
	return nil
}

func (repository *fakeRepository) FindSession(_ context.Context, _ []byte, _, _ time.Time) (Principal, error) {
	return Principal{}, ErrNotFound
}

func (repository *fakeRepository) DeleteSession(_ context.Context, _ []byte) error {
	return nil
}

func TestLoginCreatesOpaqueSessionForMitra(t *testing.T) {
	t.Parallel()

	passwordHash, err := HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatal(err)
	}
	tenantID := "tenant-1"
	repository := &fakeRepository{user: User{
		ID:           "user-1",
		TenantID:     &tenantID,
		Username:     "mitra@example.test",
		PasswordHash: passwordHash,
		Role:         RoleMitra,
		Active:       true,
	}}
	service, err := NewService(repository, 12*time.Hour, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	fixedNow := time.Date(2026, time.July, 23, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return fixedNow }

	result, err := service.Login(context.Background(), " MITRA@EXAMPLE.TEST ", "correct-horse-battery-staple", "127.0.0.1", "test")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if result.SessionToken == "" || result.CSRFToken == "" {
		t.Fatal("Login() did not return secure session tokens")
	}
	if string(repository.created.TokenHash) == result.SessionToken {
		t.Fatal("repository stored the raw session token")
	}
	if result.Principal.TenantID == nil || *result.Principal.TenantID != tenantID {
		t.Fatal("Login() lost the tenant boundary")
	}
	if !result.ExpiresAt.Equal(fixedNow.Add(12 * time.Hour)) {
		t.Fatalf("ExpiresAt = %v", result.ExpiresAt)
	}
}

func TestLoginIsThrottledAfterRepeatedFailures(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{failures: maxLoginFailures}
	service, err := NewService(repository, 12*time.Hour, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.Login(context.Background(), "user", "any-password", "127.0.0.1", "test")
	if err != ErrInvalidCredentials {
		t.Fatalf("Login() error = %v, want ErrInvalidCredentials", err)
	}
	if repository.created.UserID != "" {
		t.Fatal("Login() created a session for a throttled request")
	}
}