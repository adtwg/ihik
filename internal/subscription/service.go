package subscription

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

func (service *Service) Create(ctx context.Context, tenantID string, input CreateInput) (Subscription, error) {
	input.CustomerID = strings.TrimSpace(input.CustomerID)
	input.PackageID = strings.TrimSpace(input.PackageID)
	if tenantID == "" || input.CustomerID == "" || input.PackageID == "" {
		return Subscription{}, ErrInvalidInput
	}
	created, err := service.repository.Create(ctx, tenantID, input)
	if err != nil {
		if errors.Is(err, ErrBadReference) {
			return Subscription{}, ErrBadReference
		}
		return Subscription{}, fmt.Errorf("create subscription: %w", err)
	}
	return created, nil
}

// Get mengambil satu layanan berdasarkan ID dengan tenant scoping.
func (service *Service) Get(ctx context.Context, tenantID, subscriptionID string) (Subscription, error) {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(subscriptionID) == "" {
		return Subscription{}, ErrInvalidInput
	}
	return service.repository.Get(ctx, tenantID, subscriptionID)
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
	allowedStatus := map[string]bool{"": true, "active": true, "isolated": true, "pending_provisioning": true}
	if !allowedStatus[query.Status] {
		return PageResult{}, ErrInvalidInput
	}
	allowedSorts := map[string]bool{"service_number": true, "customer_name": true, "created_at": true}
	if !allowedSorts[query.Sort] {
		query.Sort = "created_at"
	}
	query.Order = strings.ToLower(query.Order)
	if query.Order != "asc" && query.Order != "desc" {
		query.Order = "desc"
	}
	result, err := service.repository.List(ctx, tenantID, query)
	if err != nil {
		return PageResult{}, fmt.Errorf("list subscriptions: %w", err)
	}
	return result, nil
}

func (service *Service) Isolate(ctx context.Context, tenantID, subscriptionID string) error {
	return service.transition(ctx, tenantID, subscriptionID, service.repository.Isolate, "isolate")
}

func (service *Service) Restore(ctx context.Context, tenantID, subscriptionID string) error {
	return service.transition(ctx, tenantID, subscriptionID, service.repository.Restore, "restore")
}

func (service *Service) Archive(ctx context.Context, tenantID, subscriptionID string) error {
	return service.transition(ctx, tenantID, subscriptionID, service.repository.Archive, "archive")
}

func (service *Service) transition(ctx context.Context, tenantID, subscriptionID string, action func(context.Context, string, string) error, name string) error {
	if tenantID == "" || strings.TrimSpace(subscriptionID) == "" {
		return ErrInvalidInput
	}
	if err := action(ctx, tenantID, subscriptionID); err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrInvalidState) {
			return err
		}
		return fmt.Errorf("%s subscription: %w", name, err)
	}
	return nil
}
