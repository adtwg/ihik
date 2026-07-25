package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"isp-billing/internal/dashboard"
)

type DashboardRepository struct {
	pool *pgxpool.Pool
}

func NewDashboardRepository(pool *pgxpool.Pool) *DashboardRepository {
	return &DashboardRepository{pool: pool}
}

func (repository *DashboardRepository) Snapshot(ctx context.Context, tenantID string) (dashboard.Snapshot, error) {
	var snapshot dashboard.Snapshot
	err := repository.pool.QueryRow(ctx, `
		WITH service_metrics AS (
			SELECT
				count(*) FILTER (WHERE status = 'active' AND archived_at IS NULL) AS active,
				count(*) FILTER (WHERE status = 'isolated' AND archived_at IS NULL) AS isolated,
				count(*) FILTER (WHERE status IN ('pending_provisioning', 'provisioning_failed') AND archived_at IS NULL) AS pending,
				count(*) FILTER (WHERE archived_at IS NOT NULL OR status = 'archived') AS archived
			FROM services WHERE tenant_id = $1
		), billing_metrics AS (
			SELECT
				count(*) FILTER (WHERE issued_at >= date_trunc('month', now())) AS invoices_this_month,
				count(*) FILTER (WHERE status = 'paid' AND issued_at >= date_trunc('month', now())) AS paid_this_month,
				count(*) FILTER (WHERE status = 'overdue' OR (status IN ('unpaid', 'partial') AND due_at < now())) AS overdue,
				COALESCE(sum(total) FILTER (WHERE status IN ('unpaid', 'partial', 'overdue')), 0)::text AS outstanding
			FROM invoices WHERE tenant_id = $1
		), payment_metrics AS (
			SELECT
				COALESCE(sum(amount) FILTER (WHERE received_at >= current_date), 0)::text AS received_today,
				COALESCE(sum(amount) FILTER (WHERE received_at >= date_trunc('month', now())), 0)::text AS received_this_month
			FROM payments WHERE tenant_id = $1 AND status = 'posted'
		), network_metrics AS (
			SELECT
				count(*) FILTER (WHERE active AND archived_at IS NULL AND last_connected_at >= now() - interval '5 minutes') AS online,
				count(*) FILTER (WHERE active AND archived_at IS NULL AND (last_connected_at IS NULL OR last_connected_at < now() - interval '5 minutes')) AS offline
			FROM routers WHERE tenant_id = $1
		), conflict_metrics AS (
			SELECT count(*) AS unresolved FROM sync_conflicts WHERE tenant_id = $1 AND resolved_at IS NULL
		), command_metrics AS (
			SELECT count(*) AS failed FROM provisioning_commands WHERE tenant_id = $1 AND status = 'failed'
		)
		SELECT now(), s.active, s.isolated, s.pending, s.archived,
		       b.invoices_this_month, b.paid_this_month, b.overdue, b.outstanding,
		       p.received_today, p.received_this_month,
		       n.online, n.offline, c.unresolved, cmd.failed
		FROM service_metrics s, billing_metrics b, payment_metrics p,
		     network_metrics n, conflict_metrics c, command_metrics cmd
	`, tenantID).Scan(
		&snapshot.GeneratedAt,
		&snapshot.Services.Active,
		&snapshot.Services.Isolated,
		&snapshot.Services.Pending,
		&snapshot.Services.Archived,
		&snapshot.Billing.InvoicesThisMonth,
		&snapshot.Billing.PaidThisMonth,
		&snapshot.Billing.Overdue,
		&snapshot.Billing.Outstanding,
		&snapshot.Billing.ReceivedToday,
		&snapshot.Billing.ReceivedThisMonth,
		&snapshot.Network.RoutersOnline,
		&snapshot.Network.RoutersOffline,
		&snapshot.Network.UnresolvedConflict,
		&snapshot.Network.FailedProvisioning,
	)
	if err != nil {
		return dashboard.Snapshot{}, err
	}

	rows, err := repository.pool.Query(ctx, `
		WITH months AS (
			SELECT generate_series(
				date_trunc('month', now()) - interval '5 months',
				date_trunc('month', now()), interval '1 month'
			) AS month
		), invoice_totals AS (
			SELECT date_trunc('month', issued_at) AS month, sum(total) AS amount
			FROM invoices
			WHERE tenant_id = $1 AND issued_at >= date_trunc('month', now()) - interval '5 months'
			GROUP BY 1
		), payment_totals AS (
			SELECT date_trunc('month', received_at) AS month, sum(amount) AS amount
			FROM payments
			WHERE tenant_id = $1 AND status = 'posted'
			  AND received_at >= date_trunc('month', now()) - interval '5 months'
			GROUP BY 1
		)
		SELECT to_char(m.month, 'YYYY-MM'), COALESCE(i.amount, 0)::text, COALESCE(p.amount, 0)::text
		FROM months m
		LEFT JOIN invoice_totals i ON i.month = m.month
		LEFT JOIN payment_totals p ON p.month = m.month
		ORDER BY m.month
	`, tenantID)
	if err != nil {
		return dashboard.Snapshot{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var point dashboard.TrendPoint
		if err := rows.Scan(&point.Month, &point.Invoiced, &point.Received); err != nil {
			return dashboard.Snapshot{}, err
		}
		snapshot.Trend = append(snapshot.Trend, point)
	}
	return snapshot, rows.Err()
}

var _ dashboard.Repository = (*DashboardRepository)(nil)