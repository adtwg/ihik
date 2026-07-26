package subscription

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidInput = errors.New("invalid subscription input")
	ErrNotFound     = errors.New("subscription not found")
	ErrInvalidState = errors.New("subscription state does not allow this action")
	ErrBadReference = errors.New("customer or plan reference is invalid")
)

type Subscription struct {
	ID             string     `json:"id"`
	ServiceNumber  string     `json:"service_number"`
	CustomerID     string     `json:"customer_id"`
	CustomerName   string     `json:"customer_name"`
	CustomerNumber string     `json:"customer_number"`
	PackageID      string     `json:"package_id"`
	PackageName    string     `json:"package_name"`
	PackageCode    string     `json:"package_code"`
	Price          string     `json:"price"`
	Status         string     `json:"status"`
	ActivatedAt    *time.Time `json:"activated_at,omitempty"`
	IsolatedAt     *time.Time `json:"isolated_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	ArchivedAt     *time.Time `json:"archived_at,omitempty"`
}

type CreateInput struct {
	CustomerID string `json:"customer_id"`
	PackageID  string `json:"package_id"`
}

type ListQuery struct {
	Page            int
	PageSize        int
	Search          string
	Sort            string
	Order           string
	Status          string
	IncludeArchived bool
}

type PageResult struct {
	Items         []Subscription `json:"items"`
	Page          int            `json:"page"`
	PageSize      int            `json:"page_size"`
	Total         int64          `json:"total"`
	FilteredTotal int64          `json:"filtered_total"`
	Sort          string         `json:"sort"`
	Order         string         `json:"order"`
}

type Repository interface {
	Create(ctx context.Context, tenantID string, input CreateInput) (Subscription, error)
	List(ctx context.Context, tenantID string, query ListQuery) (PageResult, error)
	Isolate(ctx context.Context, tenantID, subscriptionID string) error
	Restore(ctx context.Context, tenantID, subscriptionID string) error
	Archive(ctx context.Context, tenantID, subscriptionID string) error
}
