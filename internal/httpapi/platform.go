package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"isp-billing/internal/platform"
)

func (server *server) listTenants(response http.ResponseWriter, request *http.Request) {
	queryValues := request.URL.Query()
	page, pageErr := positiveInt(queryValues.Get("page"), 1)
	pageSize, pageSizeErr := positiveInt(queryValues.Get("page_size"), 25)
	if pageErr != nil || pageSizeErr != nil {
		writeError(response, http.StatusBadRequest, "invalid_query", "Parameter halaman tidak valid.")
		return
	}
	result, err := server.platform.List(request.Context(), platform.ListQuery{
		Page:     page,
		PageSize: pageSize,
		Search:   queryValues.Get("search"),
		Sort:     queryValues.Get("sort"),
		Order:    queryValues.Get("order"),
	})
	if err != nil {
		if errors.Is(err, platform.ErrInvalidInput) {
			writeError(response, http.StatusBadRequest, "invalid_query", "Parameter pencarian tidak valid.")
			return
		}
		writeError(response, http.StatusInternalServerError, "internal_error", "Terjadi kesalahan internal.")
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (server *server) createTenant(response http.ResponseWriter, request *http.Request) {
	request.Body = http.MaxBytesReader(response, request.Body, 16*1024)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var input platform.CreateTenantInput
	if err := decoder.Decode(&input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Data mitra tidak valid.")
		return
	}
	created, err := server.platform.CreateTenant(request.Context(), input)
	if err != nil {
		switch {
		case errors.Is(err, platform.ErrDuplicateCode):
			writeError(response, http.StatusConflict, "duplicate_code", "Kode mitra sudah digunakan.")
		case errors.Is(err, platform.ErrDuplicateUsername):
			writeError(response, http.StatusConflict, "duplicate_username", "Username admin sudah digunakan.")
		case errors.Is(err, platform.ErrInvalidInput):
			writeError(response, http.StatusBadRequest, "invalid_request", "Kode (huruf kecil/angka), nama, username, dan password minimal 12 karakter wajib valid.")
		default:
			writeError(response, http.StatusInternalServerError, "internal_error", "Terjadi kesalahan internal.")
		}
		return
	}
	writeJSON(response, http.StatusCreated, created)
}

func (server *server) setTenantActive(response http.ResponseWriter, request *http.Request) {
	request.Body = http.MaxBytesReader(response, request.Body, 4*1024)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var input struct {
		Active bool `json:"active"`
	}
	if err := decoder.Decode(&input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Status aktif tidak valid.")
		return
	}
	if err := server.platform.SetTenantActive(request.Context(), request.PathValue("tenantID"), input.Active); err != nil {
		if errors.Is(err, platform.ErrNotFound) {
			writeError(response, http.StatusNotFound, "not_found", "Mitra tidak ditemukan.")
			return
		}
		writeError(response, http.StatusInternalServerError, "internal_error", "Terjadi kesalahan internal.")
		return
	}
	response.WriteHeader(http.StatusNoContent)
}
