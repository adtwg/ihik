package billing

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidInput = errors.New("invalid billing input")
	ErrNotFound     = errors.New("invoice not found")
	ErrInvalidState = errors.New("invoice state does not allow this action")
)

type Invoice struct {
	ID             string    `json:"id"`
	InvoiceNumber  string    `json:"invoice_number"`
	CustomerID     string    `json:"customer_id"`
	CustomerName   string    `json:"customer_name"`
	CustomerNumber string    `json:"customer_number"`
	ServiceID      string    `json:"service_id"`
	ServiceNumber  string    `json:"service_number"`
	PeriodStart    time.Time `json:"period_start"`
	PeriodEnd      time.Time `json:"period_end"`
	IssuedAt       time.Time `json:"issued_at"`
	DueAt          time.Time `json:"due_at"`
	Status         string    `json:"status"`
	Subtotal       string    `json:"subtotal"`
	TaxAmount      string    `json:"tax_amount"`
	Total          string    `json:"total"`
	PaidAmount     string    `json:"paid_amount"`
	CreatedAt      time.Time `json:"created_at"`
}

type InvoiceLine struct {
	ID          string `json:"id"`
	ItemCode    string `json:"item_code"`
	Description string `json:"description"`
	Quantity    string `json:"quantity"`
	UnitPrice   string `json:"unit_price"`
	TaxPercent  string `json:"tax_percent"`
	LineTotal   string `json:"line_total"`
}

type InvoicePayment struct {
	PaymentID     string    `json:"payment_id"`
	PaymentNumber string    `json:"payment_number"`
	Amount        string    `json:"amount"`
	Method        string    `json:"method"`
	ReceivedAt    time.Time `json:"received_at"`
	Status        string    `json:"status"`
}

type InvoiceDetail struct {
	Invoice
	Lines    []InvoiceLine    `json:"lines"`
	Payments []InvoicePayment `json:"payments"`
}

type GenerateInput struct {
	Period  string `json:"period"`   // format YYYY-MM
	DueDays int    `json:"due_days"` // default 14
}

type GenerateResult struct {
	Period    string `json:"period"`
	Created   int64  `json:"created"`
	Skipped   int64  `json:"skipped"`
	NoPriceCt int64  `json:"without_price"`
}

type ListQuery struct {
	Page     int
	PageSize int
	Search   string
	Sort     string
	Order    string
	Status   string
	Period   string // YYYY-MM, filters period_start month
}

type PageResult struct {
	Items         []Invoice `json:"items"`
	Page          int       `json:"page"`
	PageSize      int       `json:"page_size"`
	Total         int64     `json:"total"`
	FilteredTotal int64     `json:"filtered_total"`
	Sort          string    `json:"sort"`
	Order         string    `json:"order"`
}

type Repository interface {
	Generate(ctx context.Context, tenantID string, periodStart, periodEnd time.Time, dueDays int) (GenerateResult, error)
	List(ctx context.Context, tenantID string, query ListQuery) (PageResult, error)
	Get(ctx context.Context, tenantID, invoiceID string) (InvoiceDetail, error)
	Void(ctx context.Context, tenantID, invoiceID string) error
}
