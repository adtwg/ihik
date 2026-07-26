package platform

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"isp-billing/internal/auth"
)

var (
	tenantCodePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,49}$`)
	usernamePattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,63}$`)
)

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (service *Service) CreateTenant(ctx context.Context, input CreateTenantInput) (CreateTenantResult, error) {
	input.Code = strings.ToLower(strings.TrimSpace(input.Code))
	input.Name = strings.TrimSpace(input.Name)
	input.AdminUsername = strings.ToLower(strings.TrimSpace(input.AdminUsername))
	if !tenantCodePattern.MatchString(input.Code) || input.Name == "" || len(input.Name) > 200 {
		return CreateTenantResult{}, ErrInvalidInput
	}
	if !usernamePattern.MatchString(input.AdminUsername) {
		return CreateTenantResult{}, ErrInvalidInput
	}
	passwordHash, err := auth.HashPassword(input.AdminPassword)
	if err != nil {
		return CreateTenantResult{}, ErrInvalidInput
	}
	tenant, err := service.repository.CreateTenant(ctx, input, passwordHash)
	if err != nil {
		if errors.Is(err, ErrDuplicateCode) || errors.Is(err, ErrDuplicateUsername) {
			return CreateTenantResult{}, err
		}
		return CreateTenantResult{}, fmt.Errorf("create tenant: %w", err)
	}
	return CreateTenantResult{Tenant: tenant, AdminUsername: input.AdminUsername}, nil
}

func (service *Service) List(ctx context.Context, query ListQuery) (PageResult, error) {
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
	allowedSorts := map[string]bool{"code": true, "name": true, "created_at": true}
	if !allowedSorts[query.Sort] {
		query.Sort = "created_at"
	}
	query.Order = strings.ToLower(query.Order)
	if query.Order != "asc" && query.Order != "desc" {
		query.Order = "desc"
	}
	result, err := service.repository.List(ctx, query)
	if err != nil {
		return PageResult{}, fmt.Errorf("list tenants: %w", err)
	}
	return result, nil
}

func (service *Service) SetTenantActive(ctx context.Context, tenantID string, active bool) error {
	if strings.TrimSpace(tenantID) == "" {
		return ErrInvalidInput
	}
	if err := service.repository.SetTenantActive(ctx, tenantID, active); err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		return fmt.Errorf("set tenant active: %w", err)
	}
	return nil
}
