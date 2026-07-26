package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"isp-billing/internal/plan"
)

type PlanRepository struct {
	pool *pgxpool.Pool
}

func NewPlanRepository(pool *pgxpool.Pool) *PlanRepository {
	return &PlanRepository{pool: pool}
}

const planSelectColumns = `
	p.id, p.code, p.name, p.billing_period_months, p.active, p.created_at, p.archived_at,
	COALESCE(price.amount::text, '0'), COALESCE(price.tax_percent::text, '0'),
	(SELECT count(*) FROM services s WHERE s.tenant_id = p.tenant_id AND s.package_id = p.id AND s.archived_at IS NULL)
`

const planCurrentPriceJoin = `
	LEFT JOIN LATERAL (
		SELECT amount, tax_percent
		FROM package_prices pp
		WHERE pp.tenant_id = p.tenant_id
		  AND pp.package_id = p.id
		  AND pp.valid_from <= CURRENT_DATE
		  AND (pp.valid_until IS NULL OR pp.valid_until >= CURRENT_DATE)
		ORDER BY pp.valid_from DESC
		LIMIT 1
	) price ON true
`

func scanPlan(row pgx.Row) (plan.Plan, error) {
	var item plan.Plan
	err := row.Scan(
		&item.ID,
		&item.Code,
		&item.Name,
		&item.BillingPeriodMonths,
		&item.Active,
		&item.CreatedAt,
		&item.ArchivedAt,
		&item.Price,
		&item.TaxPercent,
		&item.ServiceCount,
	)
	return item, err
}

func (repository *PlanRepository) Create(ctx context.Context, tenantID string, input plan.CreateInput) (plan.Plan, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return plan.Plan{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var planID string
	err = tx.QueryRow(ctx, `
		INSERT INTO packages (tenant_id, code, name)
		VALUES ($1, $2, $3)
		RETURNING id
	`, tenantID, input.Code, input.Name).Scan(&planID)
	if err != nil {
		if isUniqueViolation(err) {
			return plan.Plan{}, plan.ErrDuplicateCode
		}
		return plan.Plan{}, err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO package_prices (tenant_id, package_id, amount, tax_percent, valid_from)
		VALUES ($1, $2, round($3::numeric, 2), round($4::numeric, 4), CURRENT_DATE)
	`, tenantID, planID, input.Price, input.TaxPercent)
	if err != nil {
		return plan.Plan{}, err
	}

	created, err := scanPlan(tx.QueryRow(ctx, `
		SELECT `+planSelectColumns+`
		FROM packages p
		`+planCurrentPriceJoin+`
		WHERE p.tenant_id = $1 AND p.id = $2
	`, tenantID, planID))
	if err != nil {
		return plan.Plan{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return plan.Plan{}, err
	}
	return created, nil
}

func (repository *PlanRepository) Update(ctx context.Context, tenantID, planID string, input plan.UpdateInput) (plan.Plan, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return plan.Plan{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	result, err := tx.Exec(ctx, `
		UPDATE packages
		SET name = $3, active = $4, updated_at = now()
		WHERE tenant_id = $1 AND id = $2 AND archived_at IS NULL
	`, tenantID, planID, input.Name, input.Active)
	if err != nil {
		return plan.Plan{}, err
	}
	if result.RowsAffected() == 0 {
		return plan.Plan{}, plan.ErrNotFound
	}

	// Tutup harga berjalan lalu catat harga baru per hari ini (riwayat harga tetap utuh).
	_, err = tx.Exec(ctx, `
		UPDATE package_prices
		SET valid_until = CURRENT_DATE - 1
		WHERE tenant_id = $1 AND package_id = $2
		  AND valid_from < CURRENT_DATE
		  AND (valid_until IS NULL OR valid_until >= CURRENT_DATE)
	`, tenantID, planID)
	if err != nil {
		return plan.Plan{}, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO package_prices (tenant_id, package_id, amount, tax_percent, valid_from)
		VALUES ($1, $2, round($3::numeric, 2), round($4::numeric, 4), CURRENT_DATE)
		ON CONFLICT (tenant_id, package_id, valid_from)
		DO UPDATE SET amount = excluded.amount, tax_percent = excluded.tax_percent
	`, tenantID, planID, input.Price, input.TaxPercent)
	if err != nil {
		return plan.Plan{}, err
	}

	updated, err := scanPlan(tx.QueryRow(ctx, `
		SELECT `+planSelectColumns+`
		FROM packages p
		`+planCurrentPriceJoin+`
		WHERE p.tenant_id = $1 AND p.id = $2
	`, tenantID, planID))
	if err != nil {
		return plan.Plan{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return plan.Plan{}, err
	}
	return updated, nil
}

func (repository *PlanRepository) List(ctx context.Context, tenantID string, query plan.ListQuery) (plan.PageResult, error) {
	sortColumns := map[string]string{
		"code":       "p.code",
		"name":       "lower(p.name)",
		"created_at": "p.created_at",
	}
	sortColumn, exists := sortColumns[query.Sort]
	if !exists {
		sortColumn = "p.created_at"
	}
	order := "DESC"
	if query.Order == "asc" {
		order = "ASC"
	}

	var total int64
	err := repository.pool.QueryRow(ctx, `
		SELECT count(*) FROM packages p
		WHERE p.tenant_id = $1 AND ($2 OR p.archived_at IS NULL)
	`, tenantID, query.IncludeArchived).Scan(&total)
	if err != nil {
		return plan.PageResult{}, err
	}

	searchPattern := "%" + query.Search + "%"
	var filteredTotal int64
	err = repository.pool.QueryRow(ctx, `
		SELECT count(*) FROM packages p
		WHERE p.tenant_id = $1
		  AND ($2 OR p.archived_at IS NULL)
		  AND ($3 = '' OR p.code ILIKE $4 OR p.name ILIKE $4)
	`, tenantID, query.IncludeArchived, query.Search, searchPattern).Scan(&filteredTotal)
	if err != nil {
		return plan.PageResult{}, err
	}

	offset := (query.Page - 1) * query.PageSize
	listSQL := fmt.Sprintf(`
		SELECT `+planSelectColumns+`
		FROM packages p
		`+planCurrentPriceJoin+`
		WHERE p.tenant_id = $1
		  AND ($2 OR p.archived_at IS NULL)
		  AND ($3 = '' OR p.code ILIKE $4 OR p.name ILIKE $4)
		ORDER BY %s %s, p.id ASC
		LIMIT $5 OFFSET $6
	`, sortColumn, order)
	rows, err := repository.pool.Query(ctx, listSQL, tenantID, query.IncludeArchived, query.Search, searchPattern, query.PageSize, offset)
	if err != nil {
		return plan.PageResult{}, err
	}
	defer rows.Close()

	items := make([]plan.Plan, 0)
	for rows.Next() {
		item, err := scanPlan(rows)
		if err != nil {
			return plan.PageResult{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return plan.PageResult{}, err
	}
	return plan.PageResult{
		Items:         items,
		Page:          query.Page,
		PageSize:      query.PageSize,
		Total:         total,
		FilteredTotal: filteredTotal,
		Sort:          query.Sort,
		Order:         query.Order,
	}, nil
}

func (repository *PlanRepository) Archive(ctx context.Context, tenantID, planID string) error {
	result, err := repository.pool.Exec(ctx, `
		UPDATE packages
		SET archived_at = now(), active = false, updated_at = now()
		WHERE tenant_id = $1 AND id = $2 AND archived_at IS NULL
	`, tenantID, planID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return plan.ErrNotFound
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pgError *pgconn.PgError
	return errors.As(err, &pgError) && pgError.Code == "23505"
}

var _ plan.Repository = (*PlanRepository)(nil)
