package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"isp-billing/internal/report"
)

type ReportRepository struct {
	pool *pgxpool.Pool
}

func NewReportRepository(pool *pgxpool.Pool) *ReportRepository {
	return &ReportRepository{pool: pool}
}

func (repository *ReportRepository) Summary(ctx context.Context, tenantID string, from, to time.Time) (report.Summary, error) {
	rangeEnd := to.AddDate(0, 1, 0)
	var summary report.Summary

	err := repository.pool.QueryRow(ctx, `
		WITH paid_per_invoice AS (
			SELECT pa.invoice_id, sum(pa.amount) AS amount
			FROM payment_allocations pa
			JOIN payments pay ON pay.tenant_id = pa.tenant_id AND pay.id = pa.payment_id
			WHERE pa.tenant_id = $1 AND pay.status = 'posted'
			GROUP BY pa.invoice_id
		)
		SELECT
			COALESCE((
				SELECT sum(i.total) FROM invoices i
				WHERE i.tenant_id = $1 AND i.status <> 'void'
				  AND i.period_start >= $2::date AND i.period_start < $3::date
			), 0)::text,
			COALESCE((
				SELECT count(*) FROM invoices i
				WHERE i.tenant_id = $1 AND i.status <> 'void'
				  AND i.period_start >= $2::date AND i.period_start < $3::date
			), 0),
			COALESCE((
				SELECT sum(pay.amount) FROM payments pay
				WHERE pay.tenant_id = $1 AND pay.status = 'posted'
				  AND pay.received_at >= $2::date AND pay.received_at < $3::date
			), 0)::text,
			COALESCE((
				SELECT count(*) FROM payments pay
				WHERE pay.tenant_id = $1 AND pay.status = 'posted'
				  AND pay.received_at >= $2::date AND pay.received_at < $3::date
			), 0),
			COALESCE((
				SELECT sum(i.total - COALESCE(p.amount, 0)) FROM invoices i
				LEFT JOIN paid_per_invoice p ON p.invoice_id = i.id
				WHERE i.tenant_id = $1 AND i.status IN ('unpaid', 'partial')
			), 0)::text,
			COALESCE((
				SELECT sum(i.total - COALESCE(p.amount, 0)) FROM invoices i
				LEFT JOIN paid_per_invoice p ON p.invoice_id = i.id
				WHERE i.tenant_id = $1 AND i.status IN ('unpaid', 'partial') AND i.due_at < now()
			), 0)::text,
			COALESCE((
				SELECT count(*) FROM invoices i
				WHERE i.tenant_id = $1 AND i.status IN ('unpaid', 'partial') AND i.due_at < now()
			), 0)
	`, tenantID, from, rangeEnd).Scan(
		&summary.Totals.InvoicedAmount,
		&summary.Totals.InvoicedCount,
		&summary.Totals.CollectedAmount,
		&summary.Totals.CollectedCount,
		&summary.Totals.OutstandingAmount,
		&summary.Totals.OverdueAmount,
		&summary.Totals.OverdueCount,
	)
	if err != nil {
		return report.Summary{}, err
	}

	summary.StatusBreakdown = make([]report.StatusMetric, 0)
	statusRows, err := repository.pool.Query(ctx, `
		SELECT
			CASE WHEN i.status IN ('unpaid', 'partial') AND i.due_at < now() THEN 'overdue' ELSE i.status END AS effective_status,
			count(*),
			COALESCE(sum(i.total), 0)::text
		FROM invoices i
		WHERE i.tenant_id = $1
		  AND i.period_start >= $2::date AND i.period_start < $3::date
		GROUP BY effective_status
		ORDER BY effective_status
	`, tenantID, from, rangeEnd)
	if err != nil {
		return report.Summary{}, err
	}
	defer statusRows.Close()
	for statusRows.Next() {
		var metric report.StatusMetric
		if err := statusRows.Scan(&metric.Status, &metric.Count, &metric.Amount); err != nil {
			return report.Summary{}, err
		}
		summary.StatusBreakdown = append(summary.StatusBreakdown, metric)
	}
	if err := statusRows.Err(); err != nil {
		return report.Summary{}, err
	}

	summary.MethodBreakdown = make([]report.MethodMetric, 0)
	methodRows, err := repository.pool.Query(ctx, `
		SELECT pay.method, count(*), COALESCE(sum(pay.amount), 0)::text
		FROM payments pay
		WHERE pay.tenant_id = $1 AND pay.status = 'posted'
		  AND pay.received_at >= $2::date AND pay.received_at < $3::date
		GROUP BY pay.method
		ORDER BY pay.method
	`, tenantID, from, rangeEnd)
	if err != nil {
		return report.Summary{}, err
	}
	defer methodRows.Close()
	for methodRows.Next() {
		var metric report.MethodMetric
		if err := methodRows.Scan(&metric.Method, &metric.Count, &metric.Amount); err != nil {
			return report.Summary{}, err
		}
		summary.MethodBreakdown = append(summary.MethodBreakdown, metric)
	}
	if err := methodRows.Err(); err != nil {
		return report.Summary{}, err
	}

	summary.Monthly = make([]report.MonthMetric, 0)
	monthRows, err := repository.pool.Query(ctx, `
		WITH months AS (
			SELECT generate_series($2::date, $3::date - interval '1 month', interval '1 month')::date AS month_start
		)
		SELECT
			to_char(m.month_start, 'YYYY-MM'),
			COALESCE((
				SELECT sum(i.total) FROM invoices i
				WHERE i.tenant_id = $1 AND i.status <> 'void'
				  AND i.period_start >= m.month_start
				  AND i.period_start < m.month_start + interval '1 month'
			), 0)::text,
			COALESCE((
				SELECT sum(pay.amount) FROM payments pay
				WHERE pay.tenant_id = $1 AND pay.status = 'posted'
				  AND pay.received_at >= m.month_start
				  AND pay.received_at < m.month_start + interval '1 month'
			), 0)::text
		FROM months m
		ORDER BY m.month_start
	`, tenantID, from, rangeEnd)
	if err != nil {
		return report.Summary{}, err
	}
	defer monthRows.Close()
	for monthRows.Next() {
		var metric report.MonthMetric
		if err := monthRows.Scan(&metric.Month, &metric.Invoiced, &metric.Collected); err != nil {
			return report.Summary{}, err
		}
		summary.Monthly = append(summary.Monthly, metric)
	}
	if err := monthRows.Err(); err != nil {
		return report.Summary{}, err
	}
	return summary, nil
}

var _ report.Repository = (*ReportRepository)(nil)
