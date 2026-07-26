package report

import (
	"context"
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

// Summary menghasilkan ringkasan keuangan untuk rentang bulan [from, to] (format YYYY-MM).
func (service *Service) Summary(ctx context.Context, tenantID, fromRaw, toRaw string) (Summary, error) {
	if tenantID == "" {
		return Summary{}, ErrInvalidInput
	}
	now := time.Now().UTC()
	defaultTo := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	from := defaultTo.AddDate(0, -5, 0)
	to := defaultTo

	if strings.TrimSpace(fromRaw) != "" {
		parsed, err := time.Parse("2006-01", fromRaw)
		if err != nil {
			return Summary{}, ErrInvalidInput
		}
		from = parsed
	}
	if strings.TrimSpace(toRaw) != "" {
		parsed, err := time.Parse("2006-01", toRaw)
		if err != nil {
			return Summary{}, ErrInvalidInput
		}
		to = parsed
	}
	if to.Before(from) {
		return Summary{}, ErrInvalidInput
	}
	if to.After(from.AddDate(2, 0, 0)) {
		return Summary{}, ErrInvalidInput
	}

	summary, err := service.repository.Summary(ctx, tenantID, from, to)
	if err != nil {
		return Summary{}, fmt.Errorf("report summary: %w", err)
	}
	summary.From = from.Format("2006-01")
	summary.To = to.Format("2006-01")
	summary.GeneratedAt = time.Now().UTC()
	return summary, nil
}
