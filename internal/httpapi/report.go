package httpapi

import (
	"errors"
	"net/http"

	"isp-billing/internal/report"
)

func (server *server) financialReport(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	queryValues := request.URL.Query()
	summary, err := server.reports.Summary(request.Context(), tenantID, queryValues.Get("from"), queryValues.Get("to"))
	if err != nil {
		if errors.Is(err, report.ErrInvalidInput) {
			writeError(response, http.StatusBadRequest, "invalid_query", "Rentang laporan harus berformat YYYY-MM dan maksimal 24 bulan.")
			return
		}
		writeError(response, http.StatusInternalServerError, "internal_error", "Laporan belum dapat dimuat.")
		return
	}
	writeJSON(response, http.StatusOK, summary)
}
