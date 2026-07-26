package payment

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidInput  = errors.New("invalid payment input")
	ErrNotFound      = errors.New("payment not found")
	ErrInvoiceClosed = errors.New("invoice is not payable")
	ErrOverAllocated = errors.New("amount exceeds invoice outstanding")
	ErrInvalidState  = errors.New("payment state does not allow this action")
)

type Payment struct {
	ID             string     `json:"id"`
	PaymentNumber  string     `json:"payment_number"`
	ReceiptNumber  string     `json:"receipt_number"`
	InvoiceID      string     `json:"invoice_id"`
	InvoiceNumber  string     `json:"invoice_number"`
	CustomerName   string     `json:"customer_name"`
	CustomerNumber string     `json:"customer_number"`
	Amount         string     `json:"amount"`
	Method         string     `json:"method"`
	Status         string     `json:"status"`
	ReceivedByName string     `json:"received_by_name"`
	ReceivedAt     time.Time  `json:"received_at"`
	VoidReason     string     `json:"void_reason,omitempty"`
	VoidedAt       *time.Time `json:"voided_at,omitempty"`
}

type CreateInput struct {
	InvoiceID string  `json:"invoice_id"`
	Amount    float64 `json:"amount"`
	Method    string  `json:"method"`
}

type VoidInput struct {
	Reason string `json:"reason"`
}

type ListQuery struct {
	Page     int
	PageSize int
	Search   string
	Sort     string
	Order    string
	Method   string
	Status   string
}

type PageResult struct {
	Items         []Payment `json:"items"`
	Page          int       `json:"page"`
	PageSize      int       `json:"page_size"`
	Total         int64     `json:"total"`
	FilteredTotal int64     `json:"filtered_total"`
	Sort          string    `json:"sort"`
	Order         string    `json:"order"`
}

type Repository interface {
	Create(ctx context.Context, tenantID, receivedBy string, input CreateInput) (Payment, error)
	List(ctx context.Context, tenantID string, query ListQuery) (PageResult, error)
	Void(ctx context.Context, tenantID, voidedBy, paymentID, reason string) error
}
