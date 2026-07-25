package postgres

import (
	"context"
	"fmt"

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
	if err := tx.Commit(ctx); err != nil {
		return customer.Customer{}, err
	}
	return created, nil
}

func (repository *CustomerRepository) List(ctx context.Context, tenantID string, query customer.ListQuery) (customer.PageResult, error) {
	sortColumns := map[string]string{
		"customer_number": "customer_number",
		"name":            "lower(name)",
		"created_at":      "created_at",
	}
	sortColumn, exists := sortColumns[query.Sort]
	if !exists {
		sortColumn = "created_at"
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
	var filteredTotal int64
	err = repository.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM customers
		WHERE tenant_id = $1
		  AND ($2 OR archived_at IS NULL)
		  AND ($3 = '' OR customer_number ILIKE $4 OR name ILIKE $4 OR phone ILIKE $4 OR email ILIKE $4)
	`, tenantID, query.IncludeArchived, query.Search, searchPattern).Scan(&filteredTotal)
	if err != nil {
		return customer.PageResult{}, err
	}

	offset := (query.Page - 1) * query.PageSize
	listSQL := fmt.Sprintf(`
		SELECT id, customer_number, name, COALESCE(phone, ''), COALESCE(email, ''), COALESCE(address, ''), created_at, archived_at
		FROM customers
		WHERE tenant_id = $1
		  AND ($2 OR archived_at IS NULL)
		  AND ($3 = '' OR customer_number ILIKE $4 OR name ILIKE $4 OR phone ILIKE $4 OR email ILIKE $4)
		ORDER BY %s %s, id ASC
		LIMIT $5 OFFSET $6
	`, sortColumn, order)
	rows, err := repository.pool.Query(ctx, listSQL, tenantID, query.IncludeArchived, query.Search, searchPattern, query.PageSize, offset)
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
