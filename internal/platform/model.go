package platform

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidInput      = errors.New("invalid platform input")
	ErrNotFound          = errors.New("tenant not found")
	ErrDuplicateCode     = errors.New("tenant code already exists")
	ErrDuplicateUsername = errors.New("username already exists")
)

type Tenant struct {
	ID            string     `json:"id"`
	Code          string     `json:"code"`
	Name          string     `json:"name"`
	Active        bool       `json:"active"`
	UserCount     int64      `json:"user_count"`
	CustomerCount int64      `json:"customer_count"`
	CreatedAt     time.Time  `json:"created_at"`
	ArchivedAt    *time.Time `json:"archived_at,omitempty"`
}

type CreateTenantInput struct {
	Code          string `json:"code"`
	Name          string `json:"name"`
	AdminUsername string `json:"admin_username"`
	AdminPassword string `json:"admin_password"`
}

type CreateTenantResult struct {
	Tenant        Tenant `json:"tenant"`
	AdminUsername string `json:"admin_username"`
}

type ListQuery struct {
	Page     int
	PageSize int
	Search   string
	Sort     string
	Order    string
}

type PageResult struct {
	Items         []Tenant `json:"items"`
	Page          int      `json:"page"`
	PageSize      int      `json:"page_size"`
	Total         int64    `json:"total"`
	FilteredTotal int64    `json:"filtered_total"`
	Sort          string   `json:"sort"`
	Order         string   `json:"order"`
}

type Repository interface {
	CreateTenant(ctx context.Context, input CreateTenantInput, passwordHash string) (Tenant, error)
	List(ctx context.Context, query ListQuery) (PageResult, error)
	SetTenantActive(ctx context.Context, tenantID string, active bool) error
}
