package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"isp-billing/internal/plan"
	"isp-billing/internal/subscription"
)

func (server *server) tenantID(response http.ResponseWriter, request *http.Request) (string, bool) {
	principal := principalFromContext(request.Context())
	if principal.TenantID == nil {
		writeError(response, http.StatusForbidden, "forbidden", "Akses tenant diperlukan.")
		return "", false
	}
	return *principal.TenantID, true
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
	result, err := server.subscriptions.List(request.Context(), tenantID, subscription.ListQuery{
		Page:            page,
		PageSize:        pageSize,
		Search:          queryValues.Get("search"),
		Sort:            queryValues.Get("sort"),
		Order:           queryValues.Get("order"),
		Status:          queryValues.Get("status"),
		IncludeArchived: queryValues.Get("archived") == "include",
	})
	if err != nil {
		if errors.Is(err, subscription.ErrInvalidInput) {
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
	var input subscription.CreateInput
	if err := decoder.Decode(&input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Data layanan tidak valid.")
		return
	}
	created, err := server.subscriptions.Create(request.Context(), tenantID, input)
	if err != nil {
		switch {
		case errors.Is(err, subscription.ErrBadReference):
			writeError(response, http.StatusBadRequest, "invalid_reference", "Pelanggan atau paket tidak ditemukan / tidak aktif.")
		case errors.Is(err, subscription.ErrInvalidInput):
			writeError(response, http.StatusBadRequest, "invalid_request", "Data layanan tidak valid.")
		default:
			writeError(response, http.StatusInternalServerError, "internal_error", "Terjadi kesalahan internal.")
		}
		return
	}
	writeJSON(response, http.StatusCreated, created)
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
		case errors.Is(err, subscription.ErrNotFound):
			writeError(response, http.StatusNotFound, "not_found", "Layanan tidak ditemukan.")
		case errors.Is(err, subscription.ErrInvalidState):
			writeError(response, http.StatusConflict, "invalid_state", "Status layanan tidak memungkinkan aksi ini.")
		default:
			writeError(response, http.StatusInternalServerError, "internal_error", "Terjadi kesalahan internal.")
		}
		return
	}
	response.WriteHeader(http.StatusNoContent)
}
