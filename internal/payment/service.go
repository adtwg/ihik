package payment

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (service *Service) Create(ctx context.Context, tenantID, receivedBy string, input CreateInput) (Payment, error) {
	input.InvoiceID = strings.TrimSpace(input.InvoiceID)
	input.Method = strings.TrimSpace(input.Method)
	if tenantID == "" || receivedBy == "" || input.InvoiceID == "" {
		return Payment{}, ErrInvalidInput
	}
	if input.Method != "cash" && input.Method != "manual_transfer" {
		return Payment{}, ErrInvalidInput
	}
	if input.Amount <= 0 || input.Amount > 1_000_000_000_000 {
		return Payment{}, ErrInvalidInput
	}
	created, err := service.repository.Create(ctx, tenantID, receivedBy, input)
	if err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrInvoiceClosed) || errors.Is(err, ErrOverAllocated) {
			return Payment{}, err
		}
		return Payment{}, fmt.Errorf("create payment: %w", err)
	}
	return created, nil
}

func (service *Service) List(ctx context.Context, tenantID string, query ListQuery) (PageResult, error) {
	if tenantID == "" {
		return PageResult{}, ErrInvalidInput
	}
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PageSize < 1 {
		query.PageSize = 25
	}
	if query.PageSize > 100 {
		query.PageSize = 100
	}
	query.Search = strings.TrimSpace(query.Search)
	if len(query.Search) > 200 {
		return PageResult{}, ErrInvalidInput
	}
	allowedMethod := map[string]bool{"": true, "cash": true, "manual_transfer": true}
	allowedStatus := map[string]bool{"": true, "posted": true, "voided": true}
	if !allowedMethod[query.Method] || !allowedStatus[query.Status] {
		return PageResult{}, ErrInvalidInput
	}
	allowedSorts := map[string]bool{"payment_number": true, "amount": true, "received_at": true}
	if !allowedSorts[query.Sort] {
		query.Sort = "received_at"
	}
	query.Order = strings.ToLower(query.Order)
	if query.Order != "asc" && query.Order != "desc" {
		query.Order = "desc"
	}
	result, err := service.repository.List(ctx, tenantID, query)
	if err != nil {
		return PageResult{}, fmt.Errorf("list payments: %w", err)
	}
	return result, nil
}

func (service *Service) Void(ctx context.Context, tenantID, voidedBy, paymentID string, input VoidInput) error {
	input.Reason = strings.TrimSpace(input.Reason)
	if tenantID == "" || voidedBy == "" || strings.TrimSpace(paymentID) == "" || input.Reason == "" || len(input.Reason) > 500 {
		return ErrInvalidInput
	}
	if err := service.repository.Void(ctx, tenantID, voidedBy, paymentID, input.Reason); err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrInvalidState) {
			return err
		}
		return fmt.Errorf("void payment: %w", err)
	}
	return nil
}
