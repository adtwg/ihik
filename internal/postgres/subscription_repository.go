package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"isp-billing/internal/subscription"
)

type SubscriptionRepository struct {
	pool *pgxpool.Pool
}

func NewSubscriptionRepository(pool *pgxpool.Pool) *SubscriptionRepository {
	return &SubscriptionRepository{pool: pool}
}

const subscriptionSelectColumns = `
	s.id, s.service_number, s.customer_id, c.name, c.customer_number,
	s.package_id, p.name, p.code,
	COALESCE(price.amount::text, '0'),
	s.status::text, s.activated_at, s.isolated_at, s.created_at, s.archived_at
`

const subscriptionJoins = `
	JOIN customers c ON c.tenant_id = s.tenant_id AND c.id = s.customer_id
	JOIN packages p ON p.tenant_id = s.tenant_id AND p.id = s.package_id
	LEFT JOIN LATERAL (
		SELECT amount
		FROM package_prices pp
		WHERE pp.tenant_id = s.tenant_id
		  AND pp.package_id = s.package_id
		  AND pp.valid_from <= CURRENT_DATE
		  AND (pp.valid_until IS NULL OR pp.valid_until >= CURRENT_DATE)
		ORDER BY pp.valid_from DESC
		LIMIT 1
	) price ON true
`

func scanSubscription(row pgx.Row) (subscription.Subscription, error) {
	var item subscription.Subscription
	err := row.Scan(
		&item.ID,
		&item.ServiceNumber,
		&item.CustomerID,
		&item.CustomerName,
		&item.CustomerNumber,
		&item.PackageID,
		&item.PackageName,
		&item.PackageCode,
		&item.Price,
		&item.Status,
		&item.ActivatedAt,
		&item.IsolatedAt,
		&item.CreatedAt,
		&item.ArchivedAt,
	)
	return item, err
}

func (repository *SubscriptionRepository) Create(ctx context.Context, tenantID string, input subscription.CreateInput) (subscription.Subscription, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return subscription.Subscription{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var referenceOK bool
	err = tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM customers
			WHERE tenant_id = $1 AND id = $2 AND archived_at IS NULL
		) AND EXISTS (
			SELECT 1 FROM packages
			WHERE tenant_id = $1 AND id = $3 AND archived_at IS NULL AND active
		)
	`, tenantID, input.CustomerID, input.PackageID).Scan(&referenceOK)
	if err != nil {
		var pgError *pgconn.PgError
		if errors.As(err, &pgError) && pgError.Code == "22P02" {
			return subscription.Subscription{}, subscription.ErrBadReference
		}
		return subscription.Subscription{}, err
	}
	if !referenceOK {
		return subscription.Subscription{}, subscription.ErrBadReference
	}

	var sequence int64
	err = tx.QueryRow(ctx, `
		INSERT INTO tenant_counters (tenant_id, counter_key, counter_value)
		VALUES ($1, 'service_number', 1)
		ON CONFLICT (tenant_id, counter_key)
		DO UPDATE SET counter_value = tenant_counters.counter_value + 1
		RETURNING counter_value
	`, tenantID).Scan(&sequence)
	if err != nil {
		return subscription.Subscription{}, err
	}

	serviceNumber := fmt.Sprintf("SVC-%06d", sequence)
	var subscriptionID string
	err = tx.QueryRow(ctx, `
		INSERT INTO services (tenant_id, customer_id, package_id, service_number, status, activated_at)
		VALUES ($1, $2, $3, $4, 'active', now())
		RETURNING id
	`, tenantID, input.CustomerID, input.PackageID, serviceNumber).Scan(&subscriptionID)
	if err != nil {
		return subscription.Subscription{}, err
	}

	created, err := scanSubscription(tx.QueryRow(ctx, `
		SELECT `+subscriptionSelectColumns+`
		FROM services s
		`+subscriptionJoins+`
		WHERE s.tenant_id = $1 AND s.id = $2
	`, tenantID, subscriptionID))
	if err != nil {
		return subscription.Subscription{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return subscription.Subscription{}, err
	}
	return created, nil
}

func (repository *SubscriptionRepository) Get(ctx context.Context, tenantID, subscriptionID string) (subscription.Subscription, error) {
	var item subscription.Subscription
	err := repository.pool.QueryRow(ctx, `
		SELECT `+subscriptionSelectColumns+`
		FROM services s
		`+subscriptionJoins+`
		WHERE s.tenant_id = $1 AND s.id = $2 AND s.archived_at IS NULL
	`, tenantID, subscriptionID).Scan(
		&item.ID,
		&item.ServiceNumber,
		&item.CustomerID,
		&item.CustomerName,
		&item.CustomerNumber,
		&item.PackageID,
		&item.PackageName,
		&item.PackageCode,
		&item.Price,
		&item.Status,
		&item.ActivatedAt,
		&item.IsolatedAt,
		&item.CreatedAt,
		&item.ArchivedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return subscription.Subscription{}, subscription.ErrNotFound
		}
		return subscription.Subscription{}, err
	}
	return item, nil
}

func (repository *SubscriptionRepository) List(ctx context.Context, tenantID string, query subscription.ListQuery) (subscription.PageResult, error) {
	sortColumns := map[string]string{
		"service_number": "s.service_number",
		"customer_name":  "lower(c.name)",
		"created_at":     "s.created_at",
	}
	sortColumn, exists := sortColumns[query.Sort]
	if !exists {
		sortColumn = "s.created_at"
	}
	order := "DESC"
	if query.Order == "asc" {
		order = "ASC"
	}

	var total int64
	err := repository.pool.QueryRow(ctx, `
		SELECT count(*) FROM services s
		WHERE s.tenant_id = $1 AND ($2 OR s.archived_at IS NULL)
	`, tenantID, query.IncludeArchived).Scan(&total)
	if err != nil {
		return subscription.PageResult{}, err
	}

	searchPattern := "%" + query.Search + "%"
	filterSQL := `
		FROM services s
		` + subscriptionJoins + `
		WHERE s.tenant_id = $1
		  AND ($2 OR s.archived_at IS NULL)
		  AND ($3 = '' OR s.status::text = $3)
		  AND ($4 = '' OR s.service_number ILIKE $5 OR c.name ILIKE $5 OR c.customer_number ILIKE $5 OR p.name ILIKE $5)
	`
	var filteredTotal int64
	err = repository.pool.QueryRow(ctx, `SELECT count(*) `+filterSQL,
		tenantID, query.IncludeArchived, query.Status, query.Search, searchPattern,
	).Scan(&filteredTotal)
	if err != nil {
		return subscription.PageResult{}, err
	}

	offset := (query.Page - 1) * query.PageSize
	listSQL := fmt.Sprintf(`
		SELECT `+subscriptionSelectColumns+`
		`+filterSQL+`
		ORDER BY %s %s, s.id ASC
		LIMIT $6 OFFSET $7
	`, sortColumn, order)
	rows, err := repository.pool.Query(ctx, listSQL,
		tenantID, query.IncludeArchived, query.Status, query.Search, searchPattern, query.PageSize, offset,
	)
	if err != nil {
		return subscription.PageResult{}, err
	}
	defer rows.Close()

	items := make([]subscription.Subscription, 0)
	for rows.Next() {
		item, err := scanSubscription(rows)
		if err != nil {
			return subscription.PageResult{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return subscription.PageResult{}, err
	}
	return subscription.PageResult{
		Items:         items,
		Page:          query.Page,
		PageSize:      query.PageSize,
		Total:         total,
		FilteredTotal: filteredTotal,
		Sort:          query.Sort,
		Order:         query.Order,
	}, nil
}

func (repository *SubscriptionRepository) Isolate(ctx context.Context, tenantID, subscriptionID string) error {
	return repository.updateStatus(ctx, tenantID, subscriptionID, `
		UPDATE services
		SET status = 'isolated', isolated_at = now(), updated_at = now()
		WHERE tenant_id = $1 AND id = $2 AND archived_at IS NULL AND status = 'active'
	`)
}

func (repository *SubscriptionRepository) Restore(ctx context.Context, tenantID, subscriptionID string) error {
	return repository.updateStatus(ctx, tenantID, subscriptionID, `
		UPDATE services
		SET status = 'active', isolated_at = NULL, activated_at = COALESCE(activated_at, now()), updated_at = now()
		WHERE tenant_id = $1 AND id = $2 AND archived_at IS NULL AND status IN ('isolated', 'pending_provisioning')
	`)
}

func (repository *SubscriptionRepository) Archive(ctx context.Context, tenantID, subscriptionID string) error {
	return repository.updateStatus(ctx, tenantID, subscriptionID, `
		UPDATE services
		SET status = 'archived', archived_at = now(), updated_at = now()
		WHERE tenant_id = $1 AND id = $2 AND archived_at IS NULL
	`)
}

func (repository *SubscriptionRepository) updateStatus(ctx context.Context, tenantID, subscriptionID, updateSQL string) error {
	result, err := repository.pool.Exec(ctx, updateSQL, tenantID, subscriptionID)
	if err != nil {
		var pgError *pgconn.PgError
		if errors.As(err, &pgError) && pgError.Code == "22P02" {
			return subscription.ErrNotFound
		}
		return err
	}
	if result.RowsAffected() == 0 {
		var exists bool
		checkErr := repository.pool.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM services WHERE tenant_id = $1 AND id = $2 AND archived_at IS NULL)
		`, tenantID, subscriptionID).Scan(&exists)
		if checkErr == nil && exists {
			return subscription.ErrInvalidState
		}
		return subscription.ErrNotFound
	}
	return nil
}

var _ subscription.Repository = (*SubscriptionRepository)(nil)
