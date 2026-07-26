package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"isp-billing/internal/auth"
	"isp-billing/internal/platform"
)

type PlatformRepository struct {
	pool *pgxpool.Pool
}

func NewPlatformRepository(pool *pgxpool.Pool) *PlatformRepository {
	return &PlatformRepository{pool: pool}
}

const tenantSelectColumns = `
	t.id, t.code, t.name, t.active, t.created_at, t.archived_at,
	(SELECT count(*) FROM users u WHERE u.tenant_id = t.id AND u.archived_at IS NULL),
	(SELECT count(*) FROM customers c WHERE c.tenant_id = t.id AND c.archived_at IS NULL)
`

func scanTenant(row pgx.Row) (platform.Tenant, error) {
	var item platform.Tenant
	err := row.Scan(
		&item.ID,
		&item.Code,
		&item.Name,
		&item.Active,
		&item.CreatedAt,
		&item.ArchivedAt,
		&item.UserCount,
		&item.CustomerCount,
	)
	return item, err
}

func (repository *PlatformRepository) CreateTenant(ctx context.Context, input platform.CreateTenantInput, passwordHash string) (platform.Tenant, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return platform.Tenant{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var tenantID string
	err = tx.QueryRow(ctx, `
		INSERT INTO tenants (code, name)
		VALUES ($1, $2)
		RETURNING id
	`, input.Code, input.Name).Scan(&tenantID)
	if err != nil {
		if isUniqueViolation(err) {
			return platform.Tenant{}, platform.ErrDuplicateCode
		}
		return platform.Tenant{}, err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO users (tenant_id, username, normalized_username, password_hash, role_code)
		VALUES ($1, $2, $2, $3, $4)
	`, tenantID, input.AdminUsername, passwordHash, auth.RoleMitra)
	if err != nil {
		if isUniqueViolation(err) {
			return platform.Tenant{}, platform.ErrDuplicateUsername
		}
		return platform.Tenant{}, err
	}

	created, err := scanTenant(tx.QueryRow(ctx, `
		SELECT `+tenantSelectColumns+`
		FROM tenants t
		WHERE t.id = $1
	`, tenantID))
	if err != nil {
		return platform.Tenant{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return platform.Tenant{}, err
	}
	return created, nil
}

func (repository *PlatformRepository) List(ctx context.Context, query platform.ListQuery) (platform.PageResult, error) {
	sortColumns := map[string]string{
		"code":       "t.code",
		"name":       "lower(t.name)",
		"created_at": "t.created_at",
	}
	sortColumn, exists := sortColumns[query.Sort]
	if !exists {
		sortColumn = "t.created_at"
	}
	order := "DESC"
	if query.Order == "asc" {
		order = "ASC"
	}

	var total int64
	if err := repository.pool.QueryRow(ctx, `SELECT count(*) FROM tenants`).Scan(&total); err != nil {
		return platform.PageResult{}, err
	}

	searchPattern := "%" + query.Search + "%"
	var filteredTotal int64
	if err := repository.pool.QueryRow(ctx, `
		SELECT count(*) FROM tenants t
		WHERE ($1 = '' OR t.code ILIKE $2 OR t.name ILIKE $2)
	`, query.Search, searchPattern).Scan(&filteredTotal); err != nil {
		return platform.PageResult{}, err
	}

	offset := (query.Page - 1) * query.PageSize
	listSQL := fmt.Sprintf(`
		SELECT `+tenantSelectColumns+`
		FROM tenants t
		WHERE ($1 = '' OR t.code ILIKE $2 OR t.name ILIKE $2)
		ORDER BY %s %s, t.id ASC
		LIMIT $3 OFFSET $4
	`, sortColumn, order)
	rows, err := repository.pool.Query(ctx, listSQL, query.Search, searchPattern, query.PageSize, offset)
	if err != nil {
		return platform.PageResult{}, err
	}
	defer rows.Close()

	items := make([]platform.Tenant, 0)
	for rows.Next() {
		item, err := scanTenant(rows)
		if err != nil {
			return platform.PageResult{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return platform.PageResult{}, err
	}
	return platform.PageResult{
		Items:         items,
		Page:          query.Page,
		PageSize:      query.PageSize,
		Total:         total,
		FilteredTotal: filteredTotal,
		Sort:          query.Sort,
		Order:         query.Order,
	}, nil
}

func (repository *PlatformRepository) SetTenantActive(ctx context.Context, tenantID string, active bool) error {
	result, err := repository.pool.Exec(ctx, `
		UPDATE tenants SET active = $2 WHERE id = $1 AND archived_at IS NULL
	`, tenantID, active)
	if err != nil {
		var pgError *pgconn.PgError
		if errors.As(err, &pgError) && pgError.Code == "22P02" {
			return platform.ErrNotFound
		}
		return err
	}
	if result.RowsAffected() == 0 {
		return platform.ErrNotFound
	}
	return nil
}

var _ platform.Repository = (*PlatformRepository)(nil)
