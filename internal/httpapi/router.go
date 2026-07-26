package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"isp-billing/internal/router"
)

func routerErrorResponse(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, router.ErrNoEncryption):
		writeError(response, http.StatusServiceUnavailable, "no_encryption_key", "Kunci enkripsi belum dikonfigurasi di server (APP_ENCRYPTION_KEY).")
	case errors.Is(err, router.ErrNotConfigured):
		writeError(response, http.StatusConflict, "router_not_configured", "Router belum dikonfigurasi. Isi pengaturan router terlebih dahulu.")
	case errors.Is(err, router.ErrUnreachable):
		writeError(response, http.StatusBadGateway, "router_unreachable", "Router tidak dapat dihubungi: "+err.Error())
	case errors.Is(err, router.ErrNoAccount):
		writeError(response, http.StatusNotFound, "no_pppoe_account", "Pelanggan belum memiliki akun PPPoE. Jalankan Sync PPPoE terlebih dahulu.")
	case errors.Is(err, router.ErrInvalidInput):
		writeError(response, http.StatusBadRequest, "invalid_request", "Data router tidak valid.")
	default:
		writeError(response, http.StatusInternalServerError, "internal_error", "Terjadi kesalahan internal.")
	}
}

func (server *server) getRouter(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	config, err := server.router.Get(request.Context(), tenantID)
	if err != nil {
		routerErrorResponse(response, err)
		return
	}
	writeJSON(response, http.StatusOK, config)
}

func (server *server) saveRouter(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, 16*1024)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var input router.SaveInput
	if err := decoder.Decode(&input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Data router tidak valid.")
		return
	}
	saved, err := server.router.Save(request.Context(), tenantID, input)
	if err != nil {
		routerErrorResponse(response, err)
		return
	}
	writeJSON(response, http.StatusOK, saved)
}

func (server *server) testRouter(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	result, err := server.router.Test(request.Context(), tenantID)
	if err != nil {
		routerErrorResponse(response, err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (server *server) syncPPPoE(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	summary, err := server.router.SyncPPPoE(request.Context(), tenantID)
	if err != nil {
		routerErrorResponse(response, err)
		return
	}
	writeJSON(response, http.StatusOK, summary)
}

func (server *server) checkCustomerConnection(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	status, err := server.router.CheckConnection(request.Context(), tenantID, request.PathValue("customerID"))
	if err != nil {
		routerErrorResponse(response, err)
		return
	}
	writeJSON(response, http.StatusOK, status)
}
