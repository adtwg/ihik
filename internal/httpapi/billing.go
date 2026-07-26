package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"isp-billing/internal/billing"
	"isp-billing/internal/payment"
)

func (server *server) listInvoices(response http.ResponseWriter, request *http.Request) {
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
	result, err := server.billing.List(request.Context(), tenantID, billing.ListQuery{
		Page:     page,
		PageSize: pageSize,
		Search:   queryValues.Get("search"),
		Sort:     queryValues.Get("sort"),
		Order:    queryValues.Get("order"),
		Status:   queryValues.Get("status"),
		Period:   queryValues.Get("period"),
	})
	if err != nil {
		if errors.Is(err, billing.ErrInvalidInput) {
			writeError(response, http.StatusBadRequest, "invalid_query", "Parameter pencarian tidak valid.")
			return
		}
		writeError(response, http.StatusInternalServerError, "internal_error", "Terjadi kesalahan internal.")
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (server *server) generateInvoices(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, 16*1024)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var input billing.GenerateInput
	if err := decoder.Decode(&input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Periode tagihan tidak valid.")
		return
	}
	result, err := server.billing.Generate(request.Context(), tenantID, input)
	if err != nil {
		if errors.Is(err, billing.ErrInvalidInput) {
			writeError(response, http.StatusBadRequest, "invalid_request", "Periode harus berformat YYYY-MM dan tidak lebih dari satu bulan ke depan.")
			return
		}
		writeError(response, http.StatusInternalServerError, "internal_error", "Terjadi kesalahan internal.")
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (server *server) getInvoice(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	detail, err := server.billing.Get(request.Context(), tenantID, request.PathValue("invoiceID"))
	if err != nil {
		if errors.Is(err, billing.ErrNotFound) {
			writeError(response, http.StatusNotFound, "not_found", "Tagihan tidak ditemukan.")
			return
		}
		writeError(response, http.StatusInternalServerError, "internal_error", "Terjadi kesalahan internal.")
		return
	}
	writeJSON(response, http.StatusOK, detail)
}

func (server *server) voidInvoice(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	if err := server.billing.Void(request.Context(), tenantID, request.PathValue("invoiceID")); err != nil {
		switch {
		case errors.Is(err, billing.ErrNotFound):
			writeError(response, http.StatusNotFound, "not_found", "Tagihan tidak ditemukan.")
		case errors.Is(err, billing.ErrInvalidState):
			writeError(response, http.StatusConflict, "invalid_state", "Tagihan yang sudah memiliki pembayaran tidak dapat dibatalkan.")
		default:
			writeError(response, http.StatusInternalServerError, "internal_error", "Terjadi kesalahan internal.")
		}
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (server *server) listPayments(response http.ResponseWriter, request *http.Request) {
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
	result, err := server.payments.List(request.Context(), tenantID, payment.ListQuery{
		Page:     page,
		PageSize: pageSize,
		Search:   queryValues.Get("search"),
		Sort:     queryValues.Get("sort"),
		Order:    queryValues.Get("order"),
		Method:   queryValues.Get("method"),
		Status:   queryValues.Get("status"),
	})
	if err != nil {
		if errors.Is(err, payment.ErrInvalidInput) {
			writeError(response, http.StatusBadRequest, "invalid_query", "Parameter pencarian tidak valid.")
			return
		}
		writeError(response, http.StatusInternalServerError, "internal_error", "Terjadi kesalahan internal.")
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (server *server) createPayment(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	principal := principalFromContext(request.Context())
	request.Body = http.MaxBytesReader(response, request.Body, 16*1024)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var input payment.CreateInput
	if err := decoder.Decode(&input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Data pembayaran tidak valid.")
		return
	}
	created, err := server.payments.Create(request.Context(), tenantID, principal.UserID, input)
	if err != nil {
		switch {
		case errors.Is(err, payment.ErrNotFound):
			writeError(response, http.StatusNotFound, "not_found", "Tagihan tidak ditemukan.")
		case errors.Is(err, payment.ErrInvoiceClosed):
			writeError(response, http.StatusConflict, "invoice_closed", "Tagihan sudah lunas atau dibatalkan.")
		case errors.Is(err, payment.ErrOverAllocated):
			writeError(response, http.StatusBadRequest, "over_allocated", "Jumlah melebihi sisa tagihan.")
		case errors.Is(err, payment.ErrInvalidInput):
			writeError(response, http.StatusBadRequest, "invalid_request", "Data pembayaran tidak valid.")
		default:
			writeError(response, http.StatusInternalServerError, "internal_error", "Terjadi kesalahan internal.")
		}
		return
	}
	writeJSON(response, http.StatusCreated, created)
}

func (server *server) voidPayment(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	principal := principalFromContext(request.Context())
	request.Body = http.MaxBytesReader(response, request.Body, 16*1024)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var input payment.VoidInput
	if err := decoder.Decode(&input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Alasan pembatalan wajib diisi.")
		return
	}
	if err := server.payments.Void(request.Context(), tenantID, principal.UserID, request.PathValue("paymentID"), input); err != nil {
		switch {
		case errors.Is(err, payment.ErrNotFound):
			writeError(response, http.StatusNotFound, "not_found", "Pembayaran tidak ditemukan.")
		case errors.Is(err, payment.ErrInvalidState):
			writeError(response, http.StatusConflict, "invalid_state", "Pembayaran sudah dibatalkan.")
		case errors.Is(err, payment.ErrInvalidInput):
			writeError(response, http.StatusBadRequest, "invalid_request", "Alasan pembatalan wajib diisi.")
		default:
			writeError(response, http.StatusInternalServerError, "internal_error", "Terjadi kesalahan internal.")
		}
		return
	}
	response.WriteHeader(http.StatusNoContent)
}
