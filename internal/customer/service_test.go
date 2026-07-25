package customer

import (
	"context"
	"testing"
)

type fakeRepository struct {
	createTenantID  string
	listTenantID    string
	listQuery       ListQuery
	archiveTenantID string
}

func (repository *fakeRepository) Create(_ context.Context, tenantID string, input CreateInput) (Customer, error) {
	repository.createTenantID = tenantID
	return Customer{ID: "customer-1", CustomerNumber: "CUST-000001", Name: input.Name}, nil
}

func (repository *fakeRepository) List(_ context.Context, tenantID string, query ListQuery) (PageResult, error) {
	repository.listTenantID = tenantID
	repository.listQuery = query
	return PageResult{Items: []Customer{}, Page: query.Page, PageSize: query.PageSize}, nil
}

func (repository *fakeRepository) Archive(_ context.Context, tenantID, _ string) error {
	repository.archiveTenantID = tenantID
	return nil
}

func TestServiceScopesEveryOperationToTenant(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{}
	service := NewService(repository)
	ctx := context.Background()

	created, err := service.Create(ctx, "tenant-a", CreateInput{Name: " Pelanggan Satu ", Email: " USER@EXAMPLE.TEST "})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Name != "Pelanggan Satu" || repository.createTenantID != "tenant-a" {
		t.Fatal("Create() did not normalize input or retain tenant scope")
	}
	if _, err := service.List(ctx, "tenant-a", ListQuery{}); err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if err := service.Archive(ctx, "tenant-a", "customer-1"); err != nil {
		t.Fatalf("Archive() error = %v", err)
	}
	if repository.listTenantID != "tenant-a" || repository.archiveTenantID != "tenant-a" {
		t.Fatal("List() or Archive() lost the tenant scope")
	}
}

func TestCreateRejectsMissingTenantOrName(t *testing.T) {
	t.Parallel()

	service := NewService(&fakeRepository{})
	if _, err := service.Create(context.Background(), "", CreateInput{Name: "Customer"}); err != ErrInvalidInput {
		t.Fatalf("Create() missing tenant error = %v", err)
	}
	if _, err := service.Create(context.Background(), "tenant-a", CreateInput{}); err != ErrInvalidInput {
		t.Fatalf("Create() missing name error = %v", err)
	}
}

func TestListNormalizesServerSideParameters(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{}
	service := NewService(repository)
	_, err := service.List(context.Background(), "tenant-a", ListQuery{
		Page:     0,
		PageSize: 1_000,
		Sort:     "unsafe_sql",
		Order:    "sideways",
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if repository.listQuery.Page != 1 || repository.listQuery.PageSize != 100 {
		t.Fatalf("pagination = page %d size %d", repository.listQuery.Page, repository.listQuery.PageSize)
	}
	if repository.listQuery.Sort != "created_at" || repository.listQuery.Order != "desc" {
		t.Fatalf("sorting = %s %s", repository.listQuery.Sort, repository.listQuery.Order)
	}
}