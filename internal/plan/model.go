package plan

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidInput  = errors.New("invalid plan input")
	ErrNotFound      = errors.New("plan not found")
	ErrDuplicateCode = errors.New("plan code already exists")
)

type Plan struct {
	ID                  string     `json:"id"`
	Code                string     `json:"code"`
	Name                string     `json:"name"`
	BillingPeriodMonths int        `json:"billing_period_months"`
	Price               string     `json:"price"`
	TaxPercent          string     `json:"tax_percent"`
	Active              bool       `json:"active"`
	ServiceCount        int64      `json:"service_count"`
	CreatedAt           time.Time  `json:"created_at"`
	ArchivedAt          *time.Time `json:"archived_at,omitempty"`
}

type CreateInput struct {
	Code       string  `json:"code"`
	Name       string  `json:"name"`
	Price      float64 `json:"price"`
	TaxPercent float64 `json:"tax_percent"`
}

type UpdateInput struct {
	Name       string  `json:"name"`
	Price      float64 `json:"price"`
	TaxPercent float64 `json:"tax_percent"`
	Active     bool    `json:"active"`
}

type ListQuery struct {
	Page            int
	PageSize        int
	Search          string
	Sort            string
	Order           string
	IncludeArchived bool
}

type PageResult struct {
	Items         []Plan `json:"items"`
	Page          int    `json:"page"`
	PageSize      int    `json:"page_size"`
	Total         int64  `json:"total"`
	FilteredTotal int64  `json:"filtered_total"`
	Sort          string `json:"sort"`
	Order         string `json:"order"`
}

type Repository interface {
	Create(ctx context.Context, tenantID string, input CreateInput) (Plan, error)
	Update(ctx context.Context, tenantID, planID string, input UpdateInput) (Plan, error)
	List(ctx context.Context, tenantID string, query ListQuery) (PageResult, error)
	Archive(ctx context.Context, tenantID, planID string) error
}
