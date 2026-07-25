package dashboard

import (
	"context"
	"fmt"
	"time"
)

type ServiceMetrics struct {
	Active   int64 `json:"active"`
	Isolated int64 `json:"isolated"`
	Pending  int64 `json:"pending"`
	Archived int64 `json:"archived"`
}

type BillingMetrics struct {
	InvoicesThisMonth int64  `json:"invoices_this_month"`
	PaidThisMonth     int64  `json:"paid_this_month"`
	Overdue           int64  `json:"overdue"`
	Outstanding       string `json:"outstanding"`
	ReceivedToday     string `json:"received_today"`
	ReceivedThisMonth string `json:"received_this_month"`
}

type NetworkMetrics struct {
	RoutersOnline      int64 `json:"routers_online"`
	RoutersOffline     int64 `json:"routers_offline"`
	UnresolvedConflict int64 `json:"unresolved_conflicts"`
	FailedProvisioning int64 `json:"failed_provisioning"`
}

type TrendPoint struct {
	Month    string `json:"month"`
	Invoiced string `json:"invoiced"`
	Received string `json:"received"`
}

type Snapshot struct {
	GeneratedAt time.Time       `json:"generated_at"`
	Services    ServiceMetrics  `json:"services"`
	Billing     BillingMetrics  `json:"billing"`
	Network     NetworkMetrics  `json:"network"`
	Trend       []TrendPoint    `json:"trend"`
}

type Repository interface {
	Snapshot(ctx context.Context, tenantID string) (Snapshot, error)
}

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (service *Service) Snapshot(ctx context.Context, tenantID string) (Snapshot, error) {
	if tenantID == "" {
		return Snapshot{}, fmt.Errorf("tenant is required")
	}
	snapshot, err := service.repository.Snapshot(ctx, tenantID)
	if err != nil {
		return Snapshot{}, fmt.Errorf("load dashboard snapshot: %w", err)
	}
	return snapshot, nil
}