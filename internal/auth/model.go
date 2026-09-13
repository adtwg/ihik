package auth

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrUnauthenticated    = errors.New("authentication required")
	ErrForbidden          = errors.New("permission denied")
	ErrNotFound           = errors.New("not found")
	ErrWrongPassword      = errors.New("current password is incorrect")
	ErrInvalidInput       = errors.New("invalid input")
)

const (
	RoleSuperAdmin = "super_admin"
	RoleMitra      = "mitra"
)

type User struct {
	ID           string
	TenantID     *string
	Username     string
	PasswordHash string
	Role         string
	Active       bool
}

type Principal struct {
	UserID    string
	TenantID  *string
	Username  string
	Role      string
	TokenHash []byte
	CSRFHash  []byte
}

type NewSession struct {
	UserID    string
	TokenHash []byte
	CSRFHash  []byte
	ExpiresAt time.Time
	IPAddress string
	UserAgent string
}

type Repository interface {
	FindUserByUsername(ctx context.Context, username string) (User, error)
	FindUserByID(ctx context.Context, userID string) (User, error)
	UpdateUserPassword(ctx context.Context, userID, passwordHash string) error
	CountRecentFailures(ctx context.Context, username, ipAddress string, since time.Time) (int, error)
	RecordLoginAttempt(ctx context.Context, username, ipAddress string, succeeded bool) error
	CreateSession(ctx context.Context, session NewSession) error
	FindSession(ctx context.Context, tokenHash []byte, now, idleCutoff time.Time) (Principal, error)
	DeleteSession(ctx context.Context, tokenHash []byte) error
	DeleteOtherSessions(ctx context.Context, userID string, keepTokenHash []byte) error
}