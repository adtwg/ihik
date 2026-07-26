package report

import (
	"context"
	"errors"
	"time"
)

var ErrInvalidInput = errors.New("invalid report input")

type Totals struct {
	InvoicedAmount    string `json:"invoiced_amount"`
	InvoicedCount     int64  `json:"invoiced_count"`
	CollectedAmount   string `json:"collected_amount"`
	CollectedCount    int64  `json:"collected_count"`
	OutstandingAmount string `json:"outstanding_amount"`
	OverdueAmount     string `json:"overdue_amount"`
	OverdueCount      int64  `json:"overdue_count"`
}

type StatusMetric struct {
	Status string `json:"status"`
	Count  int64  `json:"count"`
	Amount string `json:"amount"`
}

type MethodMetric struct {
	Method string `json:"method"`
	Count  int64  `json:"count"`
	Amount string `json:"amount"`
}

type MonthMetric struct {
	Month     string `json:"month"`
	Invoiced  string `json:"invoiced"`
	Collected string `json:"collected"`
}

type Summary struct {
	From            string         `json:"from"`
	To              string         `json:"to"`
	GeneratedAt     time.Time      `json:"generated_at"`
	Totals          Totals         `json:"totals"`
	StatusBreakdown []StatusMetric `json:"status_breakdown"`
	MethodBreakdown []MethodMetric `json:"method_breakdown"`
	Monthly         []MonthMetric  `json:"monthly"`
}

type Repository interface {
	Summary(ctx context.Context, tenantID string, from, to time.Time) (Summary, error)
}
