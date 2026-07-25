package customer

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidInput = errors.New("invalid customer input")
	ErrNotFound     = errors.New("customer not found")
)

type Customer struct {
	ID             string     `json:"id"`
	CustomerNumber string     `json:"customer_number"`
	Name           string     `json:"name"`
	Phone          string     `json:"phone,omitempty"`
	Email          string     `json:"email,omitempty"`
	Address        string     `json:"address,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	ArchivedAt     *time.Time `json:"archived_at,omitempty"`
}

type CreateInput struct {
	Name    string `json:"name"`
	Phone   string `json:"phone"`
	Email   string `json:"email"`
	Address string `json:"address"`
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
	Items         []Customer `json:"items"`
	Page          int        `json:"page"`
	PageSize      int        `json:"page_size"`
	Total         int64      `json:"total"`
	FilteredTotal int64      `json:"filtered_total"`
	Sort          string     `json:"sort"`
	Order         string     `json:"order"`
}

type Repository interface {
	Create(ctx context.Context, tenantID string, input CreateInput) (Customer, error)
	List(ctx context.Context, tenantID string, query ListQuery) (PageResult, error)
	Archive(ctx context.Context, tenantID, customerID string) error
}