package customer

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

func (service *Service) Create(ctx context.Context, tenantID string, input CreateInput) (Customer, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Phone = strings.TrimSpace(input.Phone)
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	input.Address = strings.TrimSpace(input.Address)
	input.PackageID = strings.TrimSpace(input.PackageID)
	if tenantID == "" || input.Name == "" || len(input.Name) > 200 || len(input.Phone) > 50 || len(input.Email) > 320 || len(input.Address) > 2_000 {
		return Customer{}, ErrInvalidInput
	}

	created, err := service.repository.Create(ctx, tenantID, input)
	if err != nil {
		return Customer{}, fmt.Errorf("create customer: %w", err)
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

	allowedStatus := map[string]bool{"": true, "active": true, "isolated": true, "no_service": true, "unpaid": true}
	if !allowedStatus[query.Status] {
		return PageResult{}, ErrInvalidInput
	}

	allowedSorts := map[string]bool{"customer_number": true, "name": true, "created_at": true}
	if !allowedSorts[query.Sort] {
		query.Sort = "created_at"
	}
	query.Order = strings.ToLower(query.Order)
	if query.Order != "asc" && query.Order != "desc" {
		query.Order = "desc"
	}

	result, err := service.repository.List(ctx, tenantID, query)
	if err != nil {
		return PageResult{}, fmt.Errorf("list customers: %w", err)
	}
	return result, nil
}

func (service *Service) Archive(ctx context.Context, tenantID, customerID string) error {
	if tenantID == "" || strings.TrimSpace(customerID) == "" {
		return ErrInvalidInput
	}
	if err := service.repository.Archive(ctx, tenantID, customerID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		return fmt.Errorf("archive customer: %w", err)
	}
	return nil
}
