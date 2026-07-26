package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"isp-billing/internal/billing"
)

type BillingRepository struct {
	pool *pgxpool.Pool
}

func NewBillingRepository(pool *pgxpool.Pool) *BillingRepository {
	return &BillingRepository{pool: pool}
}

// effectiveStatusSQL menampilkan status jatuh tempo tanpa mengubah data tersimpan.
const effectiveStatusSQL = `
	CASE
		WHEN i.status IN ('unpaid', 'partial') AND i.due_at < now() THEN 'overdue'
		ELSE i.status
	END
`

const invoiceSelectColumns = `
	i.id, i.invoice_number, i.customer_id, c.name, c.customer_number,
	i.service_id, s.service_number,
	i.period_start, i.period_end, i.issued_at, i.due_at,
	` + effectiveStatusSQL + `,
	i.subtotal::text, i.tax_amount::text, i.total::text,
	COALESCE(paid.amount, 0)::text,
	i.created_at
`

const invoiceJoins = `
	JOIN customers c ON c.tenant_id = i.tenant_id AND c.id = i.customer_id
	JOIN services s ON s.tenant_id = i.tenant_id AND s.id = i.service_id
	LEFT JOIN LATERAL (
		SELECT sum(pa.amount) AS amount
		FROM payment_allocations pa
		JOIN payments pay ON pay.tenant_id = pa.tenant_id AND pay.id = pa.payment_id
		WHERE pa.tenant_id = i.tenant_id AND pa.invoice_id = i.id AND pay.status = 'posted'
	) paid ON true
`

func scanInvoice(row pgx.Row) (billing.Invoice, error) {
	var item billing.Invoice
	err := row.Scan(
		&item.ID,
		&item.InvoiceNumber,
		&item.CustomerID,
		&item.CustomerName,
		&item.CustomerNumber,
		&item.ServiceID,
		&item.ServiceNumber,
		&item.PeriodStart,
		&item.PeriodEnd,
		&item.IssuedAt,
		&item.DueAt,
		&item.Status,
		&item.Subtotal,
		&item.TaxAmount,
		&item.Total,
		&item.PaidAmount,
		&item.CreatedAt,
	)
	return item, err
}

func (repository *BillingRepository) Generate(ctx context.Context, tenantID string, periodStart, periodEnd time.Time, dueDays int) (billing.GenerateResult, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return billing.GenerateResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Kunci counter tenant agar penomoran faktur tidak balapan antar-request.
	if _, err := tx.Exec(ctx, `
		INSERT INTO tenant_counters (tenant_id, counter_key, counter_value)
		VALUES ($1, 'invoice_number', 0)
		ON CONFLICT (tenant_id, counter_key) DO NOTHING
	`, tenantID); err != nil {
		return billing.GenerateResult{}, err
	}
	if _, err := tx.Exec(ctx, `
		SELECT counter_value FROM tenant_counters
		WHERE tenant_id = $1 AND counter_key = 'invoice_number'
		FOR UPDATE
	`, tenantID); err != nil {
		return billing.GenerateResult{}, err
	}

	rows, err := tx.Query(ctx, `
		SELECT s.id, s.customer_id, p.name, p.code,
		       price.amount, price.tax_percent
		FROM services s
		JOIN packages p ON p.tenant_id = s.tenant_id AND p.id = s.package_id
		LEFT JOIN LATERAL (
			SELECT amount, tax_percent
			FROM package_prices pp
			WHERE pp.tenant_id = s.tenant_id
			  AND pp.package_id = s.package_id
			  AND pp.valid_from <= $2::date
			  AND (pp.valid_until IS NULL OR pp.valid_until >= $2::date)
			ORDER BY pp.valid_from DESC
			LIMIT 1
		) price ON true
		WHERE s.tenant_id = $1
		  AND s.archived_at IS NULL
		  AND s.status IN ('active', 'isolated')
		  AND NOT EXISTS (
			SELECT 1 FROM invoices i
			WHERE i.tenant_id = s.tenant_id
			  AND i.service_id = s.id
			  AND i.period_start = $2::date
			  AND i.period_end = $3::date
			  AND i.status <> 'void'
		  )
		ORDER BY s.created_at, s.id
	`, tenantID, periodStart, periodEnd)
	if err != nil {
		return billing.GenerateResult{}, err
	}

	type billable struct {
		serviceID   string
		customerID  string
		packageName string
		packageCode string
		amount      *string
		taxPercent  *string
	}
	candidates := make([]billable, 0)
	for rows.Next() {
		var item billable
		if err := rows.Scan(&item.serviceID, &item.customerID, &item.packageName, &item.packageCode, &item.amount, &item.taxPercent); err != nil {
			rows.Close()
			return billing.GenerateResult{}, err
		}
		candidates = append(candidates, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return billing.GenerateResult{}, err
	}

	var alreadyBilled int64
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM invoices i
		JOIN services s ON s.tenant_id = i.tenant_id AND s.id = i.service_id
		WHERE i.tenant_id = $1 AND i.period_start = $2::date AND i.period_end = $3::date
		  AND i.status <> 'void'
		  AND s.archived_at IS NULL AND s.status IN ('active', 'isolated')
	`, tenantID, periodStart, periodEnd).Scan(&alreadyBilled); err != nil {
		return billing.GenerateResult{}, err
	}

	result := billing.GenerateResult{Skipped: alreadyBilled}
	periodLabel := periodStart.Format("2006-01")
	for _, candidate := range candidates {
		if candidate.amount == nil {
			result.NoPriceCt++
			continue
		}
		var sequence int64
		if err := tx.QueryRow(ctx, `
			UPDATE tenant_counters
			SET counter_value = counter_value + 1
			WHERE tenant_id = $1 AND counter_key = 'invoice_number'
			RETURNING counter_value
		`, tenantID).Scan(&sequence); err != nil {
			return billing.GenerateResult{}, err
		}
		invoiceNumber := fmt.Sprintf("INV-%s-%06d", periodStart.Format("200601"), sequence)

		var invoiceID string
		if err := tx.QueryRow(ctx, `
			INSERT INTO invoices (
				tenant_id, customer_id, service_id, invoice_number,
				period_start, period_end, issued_at, due_at, status,
				subtotal, tax_amount, total
			)
			SELECT $1, $2, $3, $4,
			       $5::date, $6::date, now(), now() + make_interval(days => $7), 'unpaid',
			       round($8::numeric, 2),
			       round(round($8::numeric, 2) * $9::numeric / 100, 2),
			       round($8::numeric, 2) + round(round($8::numeric, 2) * $9::numeric / 100, 2)
			RETURNING id
		`, tenantID, candidate.customerID, candidate.serviceID, invoiceNumber,
			periodStart, periodEnd, dueDays, *candidate.amount, *candidate.taxPercent,
		).Scan(&invoiceID); err != nil {
			return billing.GenerateResult{}, err
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO invoice_lines (
				tenant_id, invoice_id, item_code, description, quantity,
				unit_price, tax_percent, line_subtotal, line_tax, line_total
			)
			SELECT $1, $2, $3, $4, 1,
			       round($5::numeric, 2), round($6::numeric, 4),
			       round($5::numeric, 2),
			       round(round($5::numeric, 2) * $6::numeric / 100, 2),
			       round($5::numeric, 2) + round(round($5::numeric, 2) * $6::numeric / 100, 2)
		`, tenantID, invoiceID, candidate.packageCode,
			fmt.Sprintf("%s (periode %s)", candidate.packageName, periodLabel),
			*candidate.amount, *candidate.taxPercent,
		); err != nil {
			return billing.GenerateResult{}, err
		}
		result.Created++
	}

	if err := tx.Commit(ctx); err != nil {
		return billing.GenerateResult{}, err
	}
	return result, nil
}

func (repository *BillingRepository) List(ctx context.Context, tenantID string, query billing.ListQuery) (billing.PageResult, error) {
	sortColumns := map[string]string{
		"invoice_number": "i.invoice_number",
		"customer_name":  "lower(c.name)",
		"due_at":         "i.due_at",
		"total":          "i.total",
		"created_at":     "i.created_at",
	}
	sortColumn, exists := sortColumns[query.Sort]
	if !exists {
		sortColumn = "i.created_at"
	}
	order := "DESC"
	if query.Order == "asc" {
		order = "ASC"
	}

	var total int64
	if err := repository.pool.QueryRow(ctx, `
		SELECT count(*) FROM invoices i WHERE i.tenant_id = $1
	`, tenantID).Scan(&total); err != nil {
		return billing.PageResult{}, err
	}

	searchPattern := "%" + query.Search + "%"
	filterSQL := `
		FROM invoices i
		` + invoiceJoins + `
		WHERE i.tenant_id = $1
		  AND ($2 = '' OR ` + effectiveStatusSQL + ` = $2)
		  AND ($3 = '' OR to_char(i.period_start, 'YYYY-MM') = $3)
		  AND ($4 = '' OR i.invoice_number ILIKE $5 OR c.name ILIKE $5 OR c.customer_number ILIKE $5 OR s.service_number ILIKE $5)
	`
	var filteredTotal int64
	if err := repository.pool.QueryRow(ctx, `SELECT count(*) `+filterSQL,
		tenantID, query.Status, query.Period, query.Search, searchPattern,
	).Scan(&filteredTotal); err != nil {
		return billing.PageResult{}, err
	}

	offset := (query.Page - 1) * query.PageSize
	listSQL := fmt.Sprintf(`
		SELECT `+invoiceSelectColumns+`
		`+filterSQL+`
		ORDER BY %s %s, i.id ASC
		LIMIT $6 OFFSET $7
	`, sortColumn, order)
	rows, err := repository.pool.Query(ctx, listSQL,
		tenantID, query.Status, query.Period, query.Search, searchPattern, query.PageSize, offset,
	)
	if err != nil {
		return billing.PageResult{}, err
	}
	defer rows.Close()

	items := make([]billing.Invoice, 0)
	for rows.Next() {
		item, err := scanInvoice(rows)
		if err != nil {
			return billing.PageResult{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return billing.PageResult{}, err
	}
	return billing.PageResult{
		Items:         items,
		Page:          query.Page,
		PageSize:      query.PageSize,
		Total:         total,
		FilteredTotal: filteredTotal,
		Sort:          query.Sort,
		Order:         query.Order,
	}, nil
}

func (repository *BillingRepository) Get(ctx context.Context, tenantID, invoiceID string) (billing.InvoiceDetail, error) {
	invoice, err := scanInvoice(repository.pool.QueryRow(ctx, `
		SELECT `+invoiceSelectColumns+`
		FROM invoices i
		`+invoiceJoins+`
		WHERE i.tenant_id = $1 AND i.id = $2
	`, tenantID, invoiceID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || isInvalidUUID(err) {
			return billing.InvoiceDetail{}, billing.ErrNotFound
		}
		return billing.InvoiceDetail{}, err
	}

	detail := billing.InvoiceDetail{Invoice: invoice, Lines: make([]billing.InvoiceLine, 0), Payments: make([]billing.InvoicePayment, 0)}

	lineRows, err := repository.pool.Query(ctx, `
		SELECT id, item_code, description, quantity::text, unit_price::text, tax_percent::text, line_total::text
		FROM invoice_lines
		WHERE tenant_id = $1 AND invoice_id = $2
		ORDER BY item_code, id
	`, tenantID, invoiceID)
	if err != nil {
		return billing.InvoiceDetail{}, err
	}
	defer lineRows.Close()
	for lineRows.Next() {
		var line billing.InvoiceLine
		if err := lineRows.Scan(&line.ID, &line.ItemCode, &line.Description, &line.Quantity, &line.UnitPrice, &line.TaxPercent, &line.LineTotal); err != nil {
			return billing.InvoiceDetail{}, err
		}
		detail.Lines = append(detail.Lines, line)
	}
	if err := lineRows.Err(); err != nil {
		return billing.InvoiceDetail{}, err
	}

	paymentRows, err := repository.pool.Query(ctx, `
		SELECT pay.id, pay.payment_number, pa.amount::text, pay.method, pay.received_at, pay.status
		FROM payment_allocations pa
		JOIN payments pay ON pay.tenant_id = pa.tenant_id AND pay.id = pa.payment_id
		WHERE pa.tenant_id = $1 AND pa.invoice_id = $2
		ORDER BY pay.received_at DESC
	`, tenantID, invoiceID)
	if err != nil {
		return billing.InvoiceDetail{}, err
	}
	defer paymentRows.Close()
	for paymentRows.Next() {
		var item billing.InvoicePayment
		if err := paymentRows.Scan(&item.PaymentID, &item.PaymentNumber, &item.Amount, &item.Method, &item.ReceivedAt, &item.Status); err != nil {
			return billing.InvoiceDetail{}, err
		}
		detail.Payments = append(detail.Payments, item)
	}
	if err := paymentRows.Err(); err != nil {
		return billing.InvoiceDetail{}, err
	}
	return detail, nil
}

func (repository *BillingRepository) Void(ctx context.Context, tenantID, invoiceID string) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var status string
	var paidAmount string
	err = tx.QueryRow(ctx, `
		SELECT i.status, COALESCE((
			SELECT sum(pa.amount)
			FROM payment_allocations pa
			JOIN payments pay ON pay.tenant_id = pa.tenant_id AND pay.id = pa.payment_id
			WHERE pa.tenant_id = i.tenant_id AND pa.invoice_id = i.id AND pay.status = 'posted'
		), 0)::text
		FROM invoices i
		WHERE i.tenant_id = $1 AND i.id = $2
		FOR UPDATE OF i
	`, tenantID, invoiceID).Scan(&status, &paidAmount)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || isInvalidUUID(err) {
			return billing.ErrNotFound
		}
		return err
	}
	if status == "void" || paidAmount != "0" {
		return billing.ErrInvalidState
	}

	if _, err := tx.Exec(ctx, `
		UPDATE invoices SET status = 'void'
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, invoiceID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func isInvalidUUID(err error) bool {
	var pgError *pgconn.PgError
	return errors.As(err, &pgError) && pgError.Code == "22P02"
}

var _ billing.Repository = (*BillingRepository)(nil)
