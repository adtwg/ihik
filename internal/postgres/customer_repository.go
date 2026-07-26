package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"isp-billing/internal/customer"
)

type CustomerRepository struct {
	pool *pgxpool.Pool
}

func NewCustomerRepository(pool *pgxpool.Pool) *CustomerRepository {
	return &CustomerRepository{pool: pool}
}

func (repository *CustomerRepository) Create(ctx context.Context, tenantID string, input customer.CreateInput) (customer.Customer, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return customer.Customer{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var sequence int64
	err = tx.QueryRow(ctx, `
		INSERT INTO tenant_counters (tenant_id, counter_key, counter_value)
		VALUES ($1, 'customer_number', 1)
		ON CONFLICT (tenant_id, counter_key)
		DO UPDATE SET counter_value = tenant_counters.counter_value + 1
		RETURNING counter_value
	`, tenantID).Scan(&sequence)
	if err != nil {
		return customer.Customer{}, err
	}

	customerNumber := fmt.Sprintf("CUST-%06d", sequence)
	var created customer.Customer
	err = tx.QueryRow(ctx, `
		INSERT INTO customers (tenant_id, customer_number, name, phone, email, address)
		VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''))
		RETURNING id, customer_number, name, COALESCE(phone, ''), COALESCE(email, ''), COALESCE(address, ''), created_at, archived_at
	`, tenantID, customerNumber, input.Name, input.Phone, input.Email, input.Address).Scan(
		&created.ID,
		&created.CustomerNumber,
		&created.Name,
		&created.Phone,
		&created.Email,
		&created.Address,
		&created.CreatedAt,
		&created.ArchivedAt,
	)
	if err != nil {
		return customer.Customer{}, err
	}

	if input.PackageID != "" {
		var packageOK bool
		err = tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM packages
				WHERE tenant_id = $1 AND id = $2 AND archived_at IS NULL AND active
			)
		`, tenantID, input.PackageID).Scan(&packageOK)
		if err != nil {
			var pgError *pgconn.PgError
			if errors.As(err, &pgError) && pgError.Code == "22P02" {
				return customer.Customer{}, customer.ErrInvalidInput
			}
			return customer.Customer{}, err
		}
		if !packageOK {
			return customer.Customer{}, customer.ErrInvalidInput
		}
		var serviceSequence int64
		err = tx.QueryRow(ctx, `
			INSERT INTO tenant_counters (tenant_id, counter_key, counter_value)
			VALUES ($1, 'service_number', 1)
			ON CONFLICT (tenant_id, counter_key)
			DO UPDATE SET counter_value = tenant_counters.counter_value + 1
			RETURNING counter_value
		`, tenantID).Scan(&serviceSequence)
		if err != nil {
			return customer.Customer{}, err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO services (tenant_id, customer_id, package_id, service_number, status, activated_at)
			VALUES ($1, $2, $3, $4, 'active', now())
		`, tenantID, created.ID, input.PackageID, fmt.Sprintf("SVC-%06d", serviceSequence))
		if err != nil {
			return customer.Customer{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return customer.Customer{}, err
	}
	return created, nil
}

func (repository *CustomerRepository) List(ctx context.Context, tenantID string, query customer.ListQuery) (customer.PageResult, error) {
	sortColumns := map[string]string{
		"customer_number": "c.customer_number",
		"name":            "lower(c.name)",
		"created_at":      "c.created_at",
	}
	sortColumn, exists := sortColumns[query.Sort]
	if !exists {
		sortColumn = "c.created_at"
	}
	order := "DESC"
	if query.Order == "asc" {
		order = "ASC"
	}

	var total int64
	err := repository.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM customers
		WHERE tenant_id = $1 AND ($2 OR archived_at IS NULL)
	`, tenantID, query.IncludeArchived).Scan(&total)
	if err != nil {
		return customer.PageResult{}, err
	}

	searchPattern := "%" + query.Search + "%"
	filterSQL := `
		FROM customers c
		LEFT JOIN LATERAL (
			SELECT s.id, s.service_number, s.status::text AS status, s.package_id
			FROM services s
			WHERE s.tenant_id = c.tenant_id AND s.customer_id = c.id AND s.archived_at IS NULL
			ORDER BY s.created_at DESC
			LIMIT 1
		) svc ON true
		LEFT JOIN packages p ON p.tenant_id = c.tenant_id AND p.id = svc.package_id
		LEFT JOIN LATERAL (
			SELECT amount FROM package_prices pp
			WHERE pp.tenant_id = c.tenant_id AND pp.package_id = svc.package_id
			  AND pp.valid_from <= CURRENT_DATE
			  AND (pp.valid_until IS NULL OR pp.valid_until >= CURRENT_DATE)
			ORDER BY pp.valid_from DESC LIMIT 1
		) price ON true
		LEFT JOIN LATERAL (
			SELECT pa.username FROM pppoe_accounts pa
			WHERE pa.tenant_id = c.tenant_id AND pa.service_id = svc.id AND pa.archived_at IS NULL
			LIMIT 1
		) pppoe ON true
		LEFT JOIN LATERAL (
			SELECT count(*) AS open_count,
			       COALESCE(sum(i.total - COALESCE(paid.amount, 0)), 0) AS outstanding
			FROM invoices i
			LEFT JOIN LATERAL (
				SELECT sum(pa2.amount) AS amount
				FROM payment_allocations pa2
				JOIN payments pay ON pay.tenant_id = pa2.tenant_id AND pay.id = pa2.payment_id
				WHERE pa2.tenant_id = i.tenant_id AND pa2.invoice_id = i.id AND pay.status = 'posted'
			) paid ON true
			WHERE i.tenant_id = c.tenant_id AND i.customer_id = c.id AND i.status IN ('unpaid', 'partial')
		) billing ON true
		LEFT JOIN LATERAL (
			SELECT i.invoice_number,
			       CASE WHEN i.status IN ('unpaid', 'partial') AND i.due_at < now() THEN 'overdue' ELSE i.status END AS status
			FROM invoices i
			WHERE i.tenant_id = c.tenant_id AND i.customer_id = c.id AND i.status <> 'void'
			ORDER BY i.created_at DESC LIMIT 1
		) last_invoice ON true
		WHERE c.tenant_id = $1
		  AND ($2 OR c.archived_at IS NULL)
		  AND ($3 = '' OR c.customer_number ILIKE $4 OR c.name ILIKE $4 OR c.phone ILIKE $4 OR c.email ILIKE $4 OR pppoe.username ILIKE $4)
		  AND ($5 = ''
			OR ($5 = 'active' AND svc.status = 'active')
			OR ($5 = 'isolated' AND svc.status = 'isolated')
			OR ($5 = 'no_service' AND svc.id IS NULL)
			OR ($5 = 'unpaid' AND COALESCE(billing.open_count, 0) > 0))
	`

	var filteredTotal int64
	err = repository.pool.QueryRow(ctx, `SELECT count(*) `+filterSQL,
		tenantID, query.IncludeArchived, query.Search, searchPattern, query.Status,
	).Scan(&filteredTotal)
	if err != nil {
		return customer.PageResult{}, err
	}

	offset := (query.Page - 1) * query.PageSize
	listSQL := fmt.Sprintf(`
		SELECT c.id, c.customer_number, c.name, COALESCE(c.phone, ''), COALESCE(c.email, ''), COALESCE(c.address, ''), c.created_at, c.archived_at,
		       COALESCE(svc.id::text, ''), COALESCE(svc.service_number, ''), COALESCE(svc.status, ''),
		       COALESCE(svc.package_id::text, ''), COALESCE(p.name, ''), COALESCE(price.amount::text, ''),
		       COALESCE(pppoe.username, ''),
		       COALESCE(billing.open_count, 0), COALESCE(billing.outstanding, 0)::text,
		       COALESCE(last_invoice.invoice_number, ''), COALESCE(last_invoice.status, '')
		`+filterSQL+`
		ORDER BY %s %s, c.id ASC
		LIMIT $6 OFFSET $7
	`, sortColumn, order)
	rows, err := repository.pool.Query(ctx, listSQL, tenantID, query.IncludeArchived, query.Search, searchPattern, query.Status, query.PageSize, offset)
	if err != nil {
		return customer.PageResult{}, err
	}
	defer rows.Close()

	customers := make([]customer.Customer, 0)
	for rows.Next() {
		var item customer.Customer
		if err := rows.Scan(
			&item.ID,
			&item.CustomerNumber,
			&item.Name,
			&item.Phone,
			&item.Email,
			&item.Address,
			&item.CreatedAt,
			&item.ArchivedAt,
			&item.ServiceID,
			&item.ServiceNumber,
			&item.ServiceStatus,
			&item.PackageID,
			&item.PackageName,
			&item.PackagePrice,
			&item.PPPoEUsername,
			&item.OpenInvoices,
			&item.Outstanding,
			&item.LastInvoice,
			&item.LastInvoiceDue,
		); err != nil {
			return customer.PageResult{}, err
		}
		customers = append(customers, item)
	}
	if err := rows.Err(); err != nil {
		return customer.PageResult{}, err
	}
	return customer.PageResult{
		Items:         customers,
		Page:          query.Page,
		PageSize:      query.PageSize,
		Total:         total,
		FilteredTotal: filteredTotal,
		Sort:          query.Sort,
		Order:         query.Order,
	}, nil
}

func (repository *CustomerRepository) Archive(ctx context.Context, tenantID, customerID string) error {
	result, err := repository.pool.Exec(ctx, `
		UPDATE customers
		SET archived_at = now(), updated_at = now()
		WHERE tenant_id = $1 AND id = $2 AND archived_at IS NULL
	`, tenantID, customerID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return customer.ErrNotFound
	}
	return nil
}

var _ customer.Repository = (*CustomerRepository)(nil)
