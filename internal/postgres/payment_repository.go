package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"isp-billing/internal/payment"
)

type PaymentRepository struct {
	pool *pgxpool.Pool
}

func NewPaymentRepository(pool *pgxpool.Pool) *PaymentRepository {
	return &PaymentRepository{pool: pool}
}

const paymentSelectColumns = `
	pay.id, pay.payment_number, r.receipt_number,
	pa.invoice_id, i.invoice_number, c.name, c.customer_number,
	pay.amount::text, pay.method, pay.status,
	u.username, pay.received_at,
	COALESCE(pay.void_reason, ''), pay.voided_at
`

const paymentJoins = `
	JOIN receipts r ON r.tenant_id = pay.tenant_id AND r.payment_id = pay.id
	JOIN payment_allocations pa ON pa.tenant_id = pay.tenant_id AND pa.payment_id = pay.id
	JOIN invoices i ON i.tenant_id = pay.tenant_id AND i.id = pa.invoice_id
	JOIN customers c ON c.tenant_id = pay.tenant_id AND c.id = i.customer_id
	JOIN users u ON u.id = pay.received_by
`

func scanPayment(row pgx.Row) (payment.Payment, error) {
	var item payment.Payment
	err := row.Scan(
		&item.ID,
		&item.PaymentNumber,
		&item.ReceiptNumber,
		&item.InvoiceID,
		&item.InvoiceNumber,
		&item.CustomerName,
		&item.CustomerNumber,
		&item.Amount,
		&item.Method,
		&item.Status,
		&item.ReceivedByName,
		&item.ReceivedAt,
		&item.VoidReason,
		&item.VoidedAt,
	)
	return item, err
}

func (repository *PaymentRepository) Create(ctx context.Context, tenantID, receivedBy string, input payment.CreateInput) (payment.Payment, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return payment.Payment{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var status string
	var outstanding string
	err = tx.QueryRow(ctx, `
		SELECT i.status,
		       (i.total - COALESCE((
			SELECT sum(pa.amount)
			FROM payment_allocations pa
			JOIN payments pay ON pay.tenant_id = pa.tenant_id AND pay.id = pa.payment_id
			WHERE pa.tenant_id = i.tenant_id AND pa.invoice_id = i.id AND pay.status = 'posted'
		       ), 0))::text
		FROM invoices i
		WHERE i.tenant_id = $1 AND i.id = $2
		FOR UPDATE OF i
	`, tenantID, input.InvoiceID).Scan(&status, &outstanding)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || isInvalidUUID(err) {
			return payment.Payment{}, payment.ErrNotFound
		}
		return payment.Payment{}, err
	}
	if status == "void" || status == "paid" {
		return payment.Payment{}, payment.ErrInvoiceClosed
	}

	var withinOutstanding bool
	if err := tx.QueryRow(ctx,
		`SELECT round($1::numeric, 2) <= $2::numeric AND round($1::numeric, 2) > 0`,
		input.Amount, outstanding,
	).Scan(&withinOutstanding); err != nil {
		return payment.Payment{}, err
	}
	if !withinOutstanding {
		return payment.Payment{}, payment.ErrOverAllocated
	}

	var paymentSequence, receiptSequence int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO tenant_counters (tenant_id, counter_key, counter_value)
		VALUES ($1, 'payment_number', 1)
		ON CONFLICT (tenant_id, counter_key)
		DO UPDATE SET counter_value = tenant_counters.counter_value + 1
		RETURNING counter_value
	`, tenantID).Scan(&paymentSequence); err != nil {
		return payment.Payment{}, err
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO tenant_counters (tenant_id, counter_key, counter_value)
		VALUES ($1, 'receipt_number', 1)
		ON CONFLICT (tenant_id, counter_key)
		DO UPDATE SET counter_value = tenant_counters.counter_value + 1
		RETURNING counter_value
	`, tenantID).Scan(&receiptSequence); err != nil {
		return payment.Payment{}, err
	}

	paymentNumber := fmt.Sprintf("PAY-%06d", paymentSequence)
	receiptNumber := fmt.Sprintf("RCP-%06d", receiptSequence)

	var paymentID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO payments (tenant_id, payment_number, amount, method, received_by)
		VALUES ($1, $2, round($3::numeric, 2), $4, $5)
		RETURNING id
	`, tenantID, paymentNumber, input.Amount, input.Method, receivedBy).Scan(&paymentID); err != nil {
		return payment.Payment{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO payment_allocations (tenant_id, payment_id, invoice_id, amount)
		VALUES ($1, $2, $3, round($4::numeric, 2))
	`, tenantID, paymentID, input.InvoiceID, input.Amount); err != nil {
		return payment.Payment{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO receipts (tenant_id, payment_id, receipt_number)
		VALUES ($1, $2, $3)
	`, tenantID, paymentID, receiptNumber); err != nil {
		return payment.Payment{}, err
	}
	if err := recomputeInvoiceStatus(ctx, tx, tenantID, input.InvoiceID); err != nil {
		return payment.Payment{}, err
	}

	created, err := scanPayment(tx.QueryRow(ctx, `
		SELECT `+paymentSelectColumns+`
		FROM payments pay
		`+paymentJoins+`
		WHERE pay.tenant_id = $1 AND pay.id = $2
	`, tenantID, paymentID))
	if err != nil {
		return payment.Payment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return payment.Payment{}, err
	}
	return created, nil
}

func recomputeInvoiceStatus(ctx context.Context, tx pgx.Tx, tenantID, invoiceID string) error {
	_, err := tx.Exec(ctx, `
		UPDATE invoices i
		SET status = CASE
			WHEN paid.amount >= i.total THEN 'paid'
			WHEN paid.amount > 0 THEN 'partial'
			ELSE 'unpaid'
		END
		FROM (
			SELECT COALESCE(sum(pa.amount), 0) AS amount
			FROM payment_allocations pa
			JOIN payments pay ON pay.tenant_id = pa.tenant_id AND pay.id = pa.payment_id
			WHERE pa.tenant_id = $1 AND pa.invoice_id = $2 AND pay.status = 'posted'
		) paid
		WHERE i.tenant_id = $1 AND i.id = $2 AND i.status <> 'void'
	`, tenantID, invoiceID)
	return err
}

func (repository *PaymentRepository) List(ctx context.Context, tenantID string, query payment.ListQuery) (payment.PageResult, error) {
	sortColumns := map[string]string{
		"payment_number": "pay.payment_number",
		"amount":         "pay.amount",
		"received_at":    "pay.received_at",
	}
	sortColumn, exists := sortColumns[query.Sort]
	if !exists {
		sortColumn = "pay.received_at"
	}
	order := "DESC"
	if query.Order == "asc" {
		order = "ASC"
	}

	var total int64
	if err := repository.pool.QueryRow(ctx, `
		SELECT count(*) FROM payments pay WHERE pay.tenant_id = $1
	`, tenantID).Scan(&total); err != nil {
		return payment.PageResult{}, err
	}

	searchPattern := "%" + query.Search + "%"
	filterSQL := `
		FROM payments pay
		` + paymentJoins + `
		WHERE pay.tenant_id = $1
		  AND ($2 = '' OR pay.method = $2)
		  AND ($3 = '' OR pay.status = $3)
		  AND ($4 = '' OR pay.payment_number ILIKE $5 OR r.receipt_number ILIKE $5 OR i.invoice_number ILIKE $5 OR c.name ILIKE $5 OR c.customer_number ILIKE $5)
	`
	var filteredTotal int64
	if err := repository.pool.QueryRow(ctx, `SELECT count(*) `+filterSQL,
		tenantID, query.Method, query.Status, query.Search, searchPattern,
	).Scan(&filteredTotal); err != nil {
		return payment.PageResult{}, err
	}

	offset := (query.Page - 1) * query.PageSize
	listSQL := fmt.Sprintf(`
		SELECT `+paymentSelectColumns+`
		`+filterSQL+`
		ORDER BY %s %s, pay.id ASC
		LIMIT $6 OFFSET $7
	`, sortColumn, order)
	rows, err := repository.pool.Query(ctx, listSQL,
		tenantID, query.Method, query.Status, query.Search, searchPattern, query.PageSize, offset,
	)
	if err != nil {
		return payment.PageResult{}, err
	}
	defer rows.Close()

	items := make([]payment.Payment, 0)
	for rows.Next() {
		item, err := scanPayment(rows)
		if err != nil {
			return payment.PageResult{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return payment.PageResult{}, err
	}
	return payment.PageResult{
		Items:         items,
		Page:          query.Page,
		PageSize:      query.PageSize,
		Total:         total,
		FilteredTotal: filteredTotal,
		Sort:          query.Sort,
		Order:         query.Order,
	}, nil
}

func (repository *PaymentRepository) Void(ctx context.Context, tenantID, voidedBy, paymentID, reason string) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var status string
	var invoiceID string
	err = tx.QueryRow(ctx, `
		SELECT pay.status, pa.invoice_id
		FROM payments pay
		JOIN payment_allocations pa ON pa.tenant_id = pay.tenant_id AND pa.payment_id = pay.id
		WHERE pay.tenant_id = $1 AND pay.id = $2
		FOR UPDATE OF pay
	`, tenantID, paymentID).Scan(&status, &invoiceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || isInvalidUUID(err) {
			return payment.ErrNotFound
		}
		return err
	}
	if status != "posted" {
		return payment.ErrInvalidState
	}

	if _, err := tx.Exec(ctx, `
		UPDATE payments
		SET status = 'voided', void_reason = $3, voided_by = $4, voided_at = now()
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, paymentID, reason, voidedBy); err != nil {
		return err
	}
	if err := recomputeInvoiceStatus(ctx, tx, tenantID, invoiceID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

var _ payment.Repository = (*PaymentRepository)(nil)
