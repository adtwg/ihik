package plan

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var codePattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9_-]{1,49}$`)

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (service *Service) Create(ctx context.Context, tenantID string, input CreateInput) (Plan, error) {
	input.Code = strings.ToUpper(strings.TrimSpace(input.Code))
	input.Name = strings.TrimSpace(input.Name)
	if tenantID == "" || !codePattern.MatchString(input.Code) || input.Name == "" || len(input.Name) > 200 {
		return Plan{}, ErrInvalidInput
	}
	if input.Price < 0 || input.Price > 1_000_000_000_000 || input.TaxPercent < 0 || input.TaxPercent > 100 {
		return Plan{}, ErrInvalidInput
	}
	created, err := service.repository.Create(ctx, tenantID, input)
	if err != nil {
		if errors.Is(err, ErrDuplicateCode) {
			return Plan{}, ErrDuplicateCode
		}
		return Plan{}, fmt.Errorf("create plan: %w", err)
	}
	return created, nil
}

func (service *Service) Update(ctx context.Context, tenantID, planID string, input UpdateInput) (Plan, error) {
	input.Name = strings.TrimSpace(input.Name)
	if tenantID == "" || strings.TrimSpace(planID) == "" || input.Name == "" || len(input.Name) > 200 {
		return Plan{}, ErrInvalidInput
	}
	if input.Price < 0 || input.Price > 1_000_000_000_000 || input.TaxPercent < 0 || input.TaxPercent > 100 {
		return Plan{}, ErrInvalidInput
	}
	updated, err := service.repository.Update(ctx, tenantID, planID, input)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Plan{}, ErrNotFound
		}
		return Plan{}, fmt.Errorf("update plan: %w", err)
	}
	return updated, nil
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
	allowedSorts := map[string]bool{"code": true, "name": true, "created_at": true}
	if !allowedSorts[query.Sort] {
		query.Sort = "created_at"
	}
	query.Order = strings.ToLower(query.Order)
	if query.Order != "asc" && query.Order != "desc" {
		query.Order = "desc"
	}
	result, err := service.repository.List(ctx, tenantID, query)
	if err != nil {
		return PageResult{}, fmt.Errorf("list plans: %w", err)
	}
	return result, nil
}

func (service *Service) Archive(ctx context.Context, tenantID, planID string) error {
	if tenantID == "" || strings.TrimSpace(planID) == "" {
		return ErrInvalidInput
	}
	if err := service.repository.Archive(ctx, tenantID, planID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		return fmt.Errorf("archive plan: %w", err)
	}
	return nil
}
