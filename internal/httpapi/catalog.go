package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"isp-billing/internal/auth"
	"isp-billing/internal/platform"
	"isp-billing/internal/plan"
	"isp-billing/internal/router"
	subscriptionpkg "isp-billing/internal/subscription"
)

// tenantID mengembalikan tenant konteks request. Mitra selalu terikat ke
// tenant-nya sendiri. Super Admin dapat bertindak atas nama sebuah mitra
// dengan mengirim header X-On-Behalf-Tenant berisi ID tenant aktif.
func (server *server) tenantID(response http.ResponseWriter, request *http.Request) (string, bool) {
	principal := principalFromContext(request.Context())
	if principal.TenantID != nil {
		return *principal.TenantID, true
	}
	if principal.Role != auth.RoleSuperAdmin {
		writeError(response, http.StatusForbidden, "forbidden", "Akses tenant diperlukan.")
		return "", false
	}
	onBehalf := strings.TrimSpace(request.Header.Get("X-On-Behalf-Tenant"))
	if onBehalf == "" {
		writeError(response, http.StatusBadRequest, "tenant_required", "Super Admin wajib mengirim header X-On-Behalf-Tenant berisi ID Mitra.")
		return "", false
	}
	active, err := server.platform.TenantActive(request.Context(), onBehalf)
	if err != nil {
		if errors.Is(err, platform.ErrNotFound) {
			writeError(response, http.StatusNotFound, "not_found", "Mitra tidak ditemukan.")
			return "", false
		}
		writeError(response, http.StatusInternalServerError, "internal_error", "Terjadi kesalahan internal.")
		return "", false
	}
	if !active {
		writeError(response, http.StatusConflict, "tenant_inactive", "Mitra sedang tidak aktif.")
		return "", false
	}
	return onBehalf, true
}

func (server *server) listPlans(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	queryValues := request.URL.Query()
	page, pageErr := positiveInt(queryValues.Get("page"), 1)
	pageSize, pageSizeErr := positiveInt(queryValues.Get("page_size"), 25)
	if pageErr != nil || pageSizeErr != nil {
		writeError(response, http.StatusBadRequest, "invalid_query", "Parameter halaman tidak valid.")
		return
	}
	result, err := server.plans.List(request.Context(), tenantID, plan.ListQuery{
		Page:            page,
		PageSize:        pageSize,
		Search:          queryValues.Get("search"),
		Sort:            queryValues.Get("sort"),
		Order:           queryValues.Get("order"),
		IncludeArchived: queryValues.Get("archived") == "include",
	})
	if err != nil {
		if errors.Is(err, plan.ErrInvalidInput) {
			writeError(response, http.StatusBadRequest, "invalid_query", "Parameter pencarian tidak valid.")
			return
		}
		writeError(response, http.StatusInternalServerError, "internal_error", "Terjadi kesalahan internal.")
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (server *server) createPlan(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, 32*1024)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var input plan.CreateInput
	if err := decoder.Decode(&input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Data paket tidak valid.")
		return
	}
	created, err := server.plans.Create(request.Context(), tenantID, input)
	if err != nil {
		switch {
		case errors.Is(err, plan.ErrDuplicateCode):
			writeError(response, http.StatusConflict, "duplicate_code", "Kode paket sudah digunakan.")
		case errors.Is(err, plan.ErrInvalidInput):
			writeError(response, http.StatusBadRequest, "invalid_request", "Kode, nama, atau harga paket tidak valid.")
		default:
			writeError(response, http.StatusInternalServerError, "internal_error", "Terjadi kesalahan internal.")
		}
		return
	}
	writeJSON(response, http.StatusCreated, created)
}

func (server *server) updatePlan(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, 32*1024)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var input plan.UpdateInput
	if err := decoder.Decode(&input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Data paket tidak valid.")
		return
	}
	updated, err := server.plans.Update(request.Context(), tenantID, request.PathValue("planID"), input)
	if err != nil {
		switch {
		case errors.Is(err, plan.ErrNotFound):
			writeError(response, http.StatusNotFound, "not_found", "Paket tidak ditemukan.")
		case errors.Is(err, plan.ErrInvalidInput):
			writeError(response, http.StatusBadRequest, "invalid_request", "Nama atau harga paket tidak valid.")
		default:
			writeError(response, http.StatusInternalServerError, "internal_error", "Terjadi kesalahan internal.")
		}
		return
	}
	writeJSON(response, http.StatusOK, updated)
}

func (server *server) archivePlan(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	if err := server.plans.Archive(request.Context(), tenantID, request.PathValue("planID")); err != nil {
		if errors.Is(err, plan.ErrNotFound) {
			writeError(response, http.StatusNotFound, "not_found", "Paket tidak ditemukan.")
			return
		}
		writeError(response, http.StatusInternalServerError, "internal_error", "Terjadi kesalahan internal.")
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (server *server) listSubscriptions(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	queryValues := request.URL.Query()
	page, pageErr := positiveInt(queryValues.Get("page"), 1)
	pageSize, pageSizeErr := positiveInt(queryValues.Get("page_size"), 25)
	if pageErr != nil || pageSizeErr != nil {
		writeError(response, http.StatusBadRequest, "invalid_query", "Parameter halaman tidak valid.")
		return
	}
	result, err := server.subscriptions.List(request.Context(), tenantID, subscriptionpkg.ListQuery{
		Page:            page,
		PageSize:        pageSize,
		Search:          queryValues.Get("search"),
		Sort:            queryValues.Get("sort"),
		Order:           queryValues.Get("order"),
		Status:          queryValues.Get("status"),
		IncludeArchived: queryValues.Get("archived") == "include",
	})
	if err != nil {
		if errors.Is(err, subscriptionpkg.ErrInvalidInput) {
			writeError(response, http.StatusBadRequest, "invalid_query", "Parameter pencarian tidak valid.")
			return
		}
		writeError(response, http.StatusInternalServerError, "internal_error", "Terjadi kesalahan internal.")
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (server *server) createSubscription(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, 16*1024)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var input subscriptionpkg.CreateInput
	if err := decoder.Decode(&input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Data layanan tidak valid.")
		return
	}
	created, err := server.subscriptions.Create(request.Context(), tenantID, input)
	if err != nil {
		switch {
		case errors.Is(err, subscriptionpkg.ErrBadReference):
			writeError(response, http.StatusBadRequest, "invalid_reference", "Pelanggan atau paket tidak ditemukan / tidak aktif.")
		case errors.Is(err, subscriptionpkg.ErrInvalidInput):
			writeError(response, http.StatusBadRequest, "invalid_request", "Data layanan tidak valid.")
		default:
			writeError(response, http.StatusInternalServerError, "internal_error", "Terjadi kesalahan internal.")
		}
		return
	}
	// Provisioning PPPoE otomatis (best effort): jika router/paket belum
	// dipetakan, layanan tetap dibuat dan user bisa provision manual.
	result := map[string]any{"subscription": created}
	if provision, err := server.router.ProvisionPPPoE(request.Context(), tenantID, router.ProvisionInput{
		ServiceID:      created.ID,
		CustomerNumber: created.CustomerNumber,
		PackageID:      created.PackageID,
	}); err == nil {
		result["pppoe"] = provision
	} else if !errors.Is(err, router.ErrNotConfigured) && !errors.Is(err, router.ErrNoEncryption) {
		result["pppoe_error"] = err.Error()
	}
	writeJSON(response, http.StatusCreated, result)
}

// provisionPPPoE membuat akun PPPoE di router untuk layanan yang sudah ada.
func (server *server) provisionPPPoE(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	serviceID := request.PathValue("serviceID")
	subscription, err := server.subscriptions.Get(request.Context(), tenantID, serviceID)
	if err != nil {
		if errors.Is(err, subscriptionpkg.ErrNotFound) {
			writeError(response, http.StatusNotFound, "not_found", "Layanan tidak ditemukan.")
			return
		}
		writeError(response, http.StatusInternalServerError, "internal_error", "Terjadi kesalahan internal.")
		return
	}
	result, err := server.router.ProvisionPPPoE(request.Context(), tenantID, router.ProvisionInput{
		ServiceID:      subscription.ID,
		CustomerNumber: subscription.CustomerNumber,
		PackageID:      subscription.PackageID,
	})
	if err != nil {
		routerErrorResponse(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, result)
}

func (server *server) isolateSubscription(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	serviceID := request.PathValue("serviceID")
	// Putus akses di router dulu; jika router bermasalah, jangan ubah status billing.
	if err := server.router.SetSecretDisabled(request.Context(), tenantID, serviceID, true); err != nil {
		routerErrorResponse(response, err)
		return
	}
	server.finishSubscriptionTransition(response, request, tenantID, serviceID, server.subscriptions.Isolate)
}

func (server *server) restoreSubscription(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	serviceID := request.PathValue("serviceID")
	if err := server.router.SetSecretDisabled(request.Context(), tenantID, serviceID, false); err != nil {
		routerErrorResponse(response, err)
		return
	}
	server.finishSubscriptionTransition(response, request, tenantID, serviceID, server.subscriptions.Restore)
}

func (server *server) archiveSubscription(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	server.finishSubscriptionTransition(response, request, tenantID, request.PathValue("serviceID"), server.subscriptions.Archive)
}

func (server *server) finishSubscriptionTransition(response http.ResponseWriter, request *http.Request, tenantID, subscriptionID string, action func(ctx context.Context, tenantID, subscriptionID string) error) {
	if err := action(request.Context(), tenantID, subscriptionID); err != nil {
		switch {
		case errors.Is(err, subscriptionpkg.ErrNotFound):
			writeError(response, http.StatusNotFound, "not_found", "Layanan tidak ditemukan.")
		case errors.Is(err, subscriptionpkg.ErrInvalidState):
			writeError(response, http.StatusConflict, "invalid_state", "Status layanan tidak memungkinkan aksi ini.")
		default:
			writeError(response, http.StatusInternalServerError, "internal_error", "Terjadi kesalahan internal.")
		}
		return
	}
	response.WriteHeader(http.StatusNoContent)
}
