package billing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (service *Service) Generate(ctx context.Context, tenantID string, input GenerateInput) (GenerateResult, error) {
	if tenantID == "" {
		return GenerateResult{}, ErrInvalidInput
	}
	periodStart, err := time.Parse("2006-01", strings.TrimSpace(input.Period))
	if err != nil {
		return GenerateResult{}, ErrInvalidInput
	}
	now := time.Now().UTC()
	earliest := time.Date(now.Year()-1, now.Month(), 1, 0, 0, 0, 0, time.UTC)
	latest := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)
	if periodStart.Before(earliest) || periodStart.After(latest) {
		return GenerateResult{}, ErrInvalidInput
	}
	periodEnd := periodStart.AddDate(0, 1, -1)
	dueDays := input.DueDays
	if dueDays == 0 {
		dueDays = 14
	}
	if dueDays < 1 || dueDays > 60 {
		return GenerateResult{}, ErrInvalidInput
	}
	result, err := service.repository.Generate(ctx, tenantID, periodStart, periodEnd, dueDays)
	if err != nil {
		return GenerateResult{}, fmt.Errorf("generate invoices: %w", err)
	}
	result.Period = periodStart.Format("2006-01")
	return result, nil
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
	allowedStatus := map[string]bool{"": true, "unpaid": true, "partial": true, "paid": true, "overdue": true, "void": true}
	if !allowedStatus[query.Status] {
		return PageResult{}, ErrInvalidInput
	}
	if query.Period != "" {
		if _, err := time.Parse("2006-01", query.Period); err != nil {
			return PageResult{}, ErrInvalidInput
		}
	}
	allowedSorts := map[string]bool{"invoice_number": true, "customer_name": true, "due_at": true, "total": true, "created_at": true}
	if !allowedSorts[query.Sort] {
		query.Sort = "created_at"
	}
	query.Order = strings.ToLower(query.Order)
	if query.Order != "asc" && query.Order != "desc" {
		query.Order = "desc"
	}
	result, err := service.repository.List(ctx, tenantID, query)
	if err != nil {
		return PageResult{}, fmt.Errorf("list invoices: %w", err)
	}
	return result, nil
}

func (service *Service) Get(ctx context.Context, tenantID, invoiceID string) (InvoiceDetail, error) {
	if tenantID == "" || strings.TrimSpace(invoiceID) == "" {
		return InvoiceDetail{}, ErrInvalidInput
	}
	detail, err := service.repository.Get(ctx, tenantID, invoiceID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return InvoiceDetail{}, ErrNotFound
		}
		return InvoiceDetail{}, fmt.Errorf("get invoice: %w", err)
	}
	return detail, nil
}

func (service *Service) Void(ctx context.Context, tenantID, invoiceID string) error {
	if tenantID == "" || strings.TrimSpace(invoiceID) == "" {
		return ErrInvalidInput
	}
	if err := service.repository.Void(ctx, tenantID, invoiceID); err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrInvalidState) {
			return err
		}
		return fmt.Errorf("void invoice: %w", err)
	}
	return nil
}
