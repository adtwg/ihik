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

	// Ringkasan layanan & tagihan (diisi saat listing).
	ServiceID      string `json:"service_id,omitempty"`
	ServiceNumber  string `json:"service_number,omitempty"`
	ServiceStatus  string `json:"service_status,omitempty"`
	PackageID      string `json:"package_id,omitempty"`
	PackageName    string `json:"package_name,omitempty"`
	PackagePrice   string `json:"package_price,omitempty"`
	PPPoEUsername  string `json:"pppoe_username,omitempty"`
	OpenInvoices   int64  `json:"open_invoices"`
	Outstanding    string `json:"outstanding"`
	LastInvoice    string `json:"last_invoice_number,omitempty"`
	LastInvoiceDue string `json:"last_invoice_status,omitempty"`
}

type CreateInput struct {
	Name      string `json:"name"`
	Phone     string `json:"phone"`
	Email     string `json:"email"`
	Address   string `json:"address"`
	PackageID string `json:"package_id"`
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