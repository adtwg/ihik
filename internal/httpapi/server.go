package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"isp-billing/internal/auth"
	"isp-billing/internal/billing"
	"isp-billing/internal/customer"
	"isp-billing/internal/dashboard"
	"isp-billing/internal/olt"
	"isp-billing/internal/payment"
	"isp-billing/internal/plan"
	"isp-billing/internal/platform"
	"isp-billing/internal/report"
	"isp-billing/internal/router"
	"isp-billing/internal/subscription"
)

const (
	sessionCookieName = "isp_session"
	csrfCookieName    = "isp_csrf"
)

type contextKey string

const principalContextKey contextKey = "principal"

type server struct {
	auth          *auth.Service
	customers     *customer.Service
	dashboard     *dashboard.Service
	plans         *plan.Service
	subscriptions *subscription.Service
	billing       *billing.Service
	payments      *payment.Service
	platform      *platform.Service
	reports       *report.Service
	router        *router.Service
	olts          *olt.Service
	readiness     func(context.Context) error
	cookieSecure  bool
}

type Dependencies struct {
	Auth          *auth.Service
	Customers     *customer.Service
	Dashboard     *dashboard.Service
	Plans         *plan.Service
	Subscriptions *subscription.Service
	Billing       *billing.Service
	Payments      *payment.Service
	Platform      *platform.Service
	Reports       *report.Service
	Router        *router.Service
	OLTs          *olt.Service
	Readiness     func(context.Context) error
	CookieSecure  bool
}

func NewHandler(deps Dependencies) http.Handler {
	api := &server{
		auth:          deps.Auth,
		customers:     deps.Customers,
		dashboard:     deps.Dashboard,
		plans:         deps.Plans,
		subscriptions: deps.Subscriptions,
		billing:       deps.Billing,
		payments:      deps.Payments,
		platform:      deps.Platform,
		reports:       deps.Reports,
		router:        deps.Router,
		olts:          deps.OLTs,
		readiness:     deps.Readiness,
		cookieSecure:  deps.CookieSecure,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", api.health)
	mux.HandleFunc("GET /ready", api.ready)
	mux.HandleFunc("POST /api/v1/auth/login", api.login)
	mux.Handle("GET /api/v1/me", api.authenticate(http.HandlerFunc(api.me)))
	mux.Handle("POST /api/v1/auth/logout", api.authenticate(api.requireCSRF(http.HandlerFunc(api.logout))))
	mux.Handle("POST /api/v1/auth/change-password", api.authenticate(api.requireCSRF(http.HandlerFunc(api.changePassword))))
	mux.Handle("GET /api/v1/customers", api.authenticate(api.authorize(auth.PermissionCustomerManage, http.HandlerFunc(api.listCustomers))))
	mux.Handle("POST /api/v1/customers", api.authenticate(api.authorize(auth.PermissionCustomerManage, api.requireCSRF(http.HandlerFunc(api.createCustomer)))))
	mux.Handle("POST /api/v1/customers/{customerID}/archive", api.authenticate(api.authorize(auth.PermissionCustomerManage, api.requireCSRF(http.HandlerFunc(api.archiveCustomer)))))
	mux.Handle("POST /api/v1/customers/sync-pppoe", api.authenticate(api.authorize(auth.PermissionCustomerManage, api.requireCSRF(http.HandlerFunc(api.syncPPPoE)))))
	mux.Handle("GET /api/v1/customers/{customerID}/connection", api.authenticate(api.authorize(auth.PermissionCustomerManage, http.HandlerFunc(api.checkCustomerConnection))))
	mux.Handle("GET /api/v1/dashboard", api.authenticate(api.authorize(auth.PermissionCustomerManage, http.HandlerFunc(api.mitraDashboard))))

	mux.Handle("GET /api/v1/router", api.authenticate(api.authorize(auth.PermissionRouterManage, http.HandlerFunc(api.getRouter))))
	mux.Handle("PUT /api/v1/router", api.authenticate(api.authorize(auth.PermissionRouterManage, api.requireCSRF(http.HandlerFunc(api.saveRouter)))))
	mux.Handle("POST /api/v1/router/test", api.authenticate(api.authorize(auth.PermissionRouterManage, api.requireCSRF(http.HandlerFunc(api.testRouter)))))

	mux.Handle("GET /api/v1/plans", api.authenticate(api.authorize(auth.PermissionPackageManage, http.HandlerFunc(api.listPlans))))
	mux.Handle("POST /api/v1/plans", api.authenticate(api.authorize(auth.PermissionPackageManage, api.requireCSRF(http.HandlerFunc(api.createPlan)))))
	mux.Handle("PUT /api/v1/plans/{planID}", api.authenticate(api.authorize(auth.PermissionPackageManage, api.requireCSRF(http.HandlerFunc(api.updatePlan)))))
	mux.Handle("POST /api/v1/plans/{planID}/archive", api.authenticate(api.authorize(auth.PermissionPackageManage, api.requireCSRF(http.HandlerFunc(api.archivePlan)))))

	mux.Handle("GET /api/v1/services", api.authenticate(api.authorize(auth.PermissionCustomerManage, http.HandlerFunc(api.listSubscriptions))))
	mux.Handle("POST /api/v1/services", api.authenticate(api.authorize(auth.PermissionCustomerManage, api.requireCSRF(http.HandlerFunc(api.createSubscription)))))
	mux.Handle("POST /api/v1/services/{serviceID}/provision-pppoe", api.authenticate(api.authorize(auth.PermissionCustomerManage, api.requireCSRF(http.HandlerFunc(api.provisionPPPoE)))))
	mux.Handle("POST /api/v1/services/{serviceID}/isolate", api.authenticate(api.authorize(auth.PermissionCustomerManage, api.requireCSRF(http.HandlerFunc(api.isolateSubscription)))))
	mux.Handle("POST /api/v1/services/{serviceID}/restore", api.authenticate(api.authorize(auth.PermissionCustomerManage, api.requireCSRF(http.HandlerFunc(api.restoreSubscription)))))
	mux.Handle("POST /api/v1/services/{serviceID}/archive", api.authenticate(api.authorize(auth.PermissionCustomerManage, api.requireCSRF(http.HandlerFunc(api.archiveSubscription)))))

	mux.Handle("GET /api/v1/invoices", api.authenticate(api.authorize(auth.PermissionBillingManage, http.HandlerFunc(api.listInvoices))))
	mux.Handle("POST /api/v1/invoices/generate", api.authenticate(api.authorize(auth.PermissionBillingManage, api.requireCSRF(http.HandlerFunc(api.generateInvoices)))))
	mux.Handle("GET /api/v1/invoices/{invoiceID}", api.authenticate(api.authorize(auth.PermissionBillingManage, http.HandlerFunc(api.getInvoice))))
	mux.Handle("POST /api/v1/invoices/{invoiceID}/void", api.authenticate(api.authorize(auth.PermissionBillingManage, api.requireCSRF(http.HandlerFunc(api.voidInvoice)))))

	mux.Handle("GET /api/v1/payments", api.authenticate(api.authorize(auth.PermissionBillingManage, http.HandlerFunc(api.listPayments))))
	mux.Handle("GET /api/v1/payments/{paymentID}", api.authenticate(api.authorize(auth.PermissionBillingManage, http.HandlerFunc(api.getPayment))))
	mux.Handle("POST /api/v1/payments", api.authenticate(api.authorize(auth.PermissionBillingManage, api.requireCSRF(http.HandlerFunc(api.createPayment)))))
	mux.Handle("POST /api/v1/payments/{paymentID}/void", api.authenticate(api.authorize(auth.PermissionBillingManage, api.requireCSRF(http.HandlerFunc(api.voidPayment)))))

	mux.Handle("GET /api/v1/reports/financial", api.authenticate(api.authorize(auth.PermissionFinanceRead, http.HandlerFunc(api.financialReport))))

	mux.Handle("GET /api/v1/platform/tenants", api.authenticate(api.authorize(auth.PermissionPlatformManage, http.HandlerFunc(api.listTenants))))
	mux.Handle("POST /api/v1/platform/tenants", api.authenticate(api.authorize(auth.PermissionPlatformManage, api.requireCSRF(http.HandlerFunc(api.createTenant)))))
	mux.Handle("POST /api/v1/platform/tenants/{tenantID}/active", api.authenticate(api.authorize(auth.PermissionPlatformManage, api.requireCSRF(http.HandlerFunc(api.setTenantActive)))))

	// Manajemen OLT ZTE (C320/C300) via SNMP.
	mux.Handle("GET /api/v1/olts", api.authenticate(api.authorize(auth.PermissionRouterManage, http.HandlerFunc(api.listOLTs))))
	mux.Handle("POST /api/v1/olts", api.authenticate(api.authorize(auth.PermissionRouterManage, api.requireCSRF(http.HandlerFunc(api.createOLT)))))
	mux.Handle("GET /api/v1/olts/{oltID}", api.authenticate(api.authorize(auth.PermissionRouterManage, http.HandlerFunc(api.getOLT))))
	mux.Handle("PUT /api/v1/olts/{oltID}", api.authenticate(api.authorize(auth.PermissionRouterManage, api.requireCSRF(http.HandlerFunc(api.updateOLT)))))
	mux.Handle("DELETE /api/v1/olts/{oltID}", api.authenticate(api.authorize(auth.PermissionRouterManage, api.requireCSRF(http.HandlerFunc(api.deleteOLT)))))
	mux.Handle("POST /api/v1/olts/{oltID}/test", api.authenticate(api.authorize(auth.PermissionRouterManage, api.requireCSRF(http.HandlerFunc(api.testOLT)))))
	mux.Handle("POST /api/v1/olts/{oltID}/test-cli", api.authenticate(api.authorize(auth.PermissionRouterManage, api.requireCSRF(http.HandlerFunc(api.testOLTCLI)))))
	mux.Handle("GET /api/v1/olts/{oltID}/onus", api.authenticate(api.authorize(auth.PermissionRouterManage, http.HandlerFunc(api.listONUS))))
	mux.Handle("GET /api/v1/olts/{oltID}/onus-paged", api.authenticate(api.authorize(auth.PermissionRouterManage, http.HandlerFunc(api.listONUSPaged))))
	mux.Handle("GET /api/v1/olts/{oltID}/onus-stats", api.authenticate(api.authorize(auth.PermissionRouterManage, http.HandlerFunc(api.onuStats))))
	mux.Handle("GET /api/v1/olts/{oltID}/health", api.authenticate(api.authorize(auth.PermissionRouterManage, http.HandlerFunc(api.oltHealth))))
	mux.Handle("GET /api/v1/olts/{oltID}/chassis", api.authenticate(api.authorize(auth.PermissionRouterManage, http.HandlerFunc(api.oltChassis))))
	mux.Handle("GET /api/v1/olts/{oltID}/debug-walk", api.authenticate(api.authorize(auth.PermissionRouterManage, http.HandlerFunc(api.oltDebugWalk))))
	mux.Handle("POST /api/v1/olts/{oltID}/oct-scan", api.authenticate(api.authorize(auth.PermissionRouterManage, http.HandlerFunc(api.oltOctScan))))
	mux.Handle("GET /api/v1/olts/{oltID}/oct-scan", api.authenticate(api.authorize(auth.PermissionRouterManage, http.HandlerFunc(api.oltOctScan))))
	mux.Handle("GET /api/v1/olts/{oltID}/ifname-traffic", api.authenticate(api.authorize(auth.PermissionRouterManage, http.HandlerFunc(api.oltIfNameTraffic))))
	mux.Handle("GET /api/v1/olts/{oltID}/onus/daily", api.authenticate(api.authorize(auth.PermissionRouterManage, http.HandlerFunc(api.onuDaily))))
	mux.Handle("GET /api/v1/olts/{oltID}/onus/intraday", api.authenticate(api.authorize(auth.PermissionRouterManage, http.HandlerFunc(api.onuIntraday))))
	mux.Handle("POST /api/v1/olts/{oltID}/refresh-traffic", api.authenticate(api.authorize(auth.PermissionRouterManage, api.requireCSRF(http.HandlerFunc(api.refreshTraffic)))))
	mux.Handle("POST /api/v1/olts/{oltID}/sync-onus", api.authenticate(api.authorize(auth.PermissionRouterManage, api.requireCSRF(http.HandlerFunc(api.syncONUS)))))
	mux.Handle("POST /api/v1/olts/{oltID}/sync-optical", api.authenticate(api.authorize(auth.PermissionRouterManage, api.requireCSRF(http.HandlerFunc(api.syncOpticalONUS)))))
	mux.Handle("POST /api/v1/olts/{oltID}/onu-sync", api.authenticate(api.authorize(auth.PermissionRouterManage, api.requireCSRF(http.HandlerFunc(api.onuSyncOne)))))
	mux.Handle("POST /api/v1/olts/{oltID}/onus/{action}", api.authenticate(api.authorize(auth.PermissionRouterManage, api.requireCSRF(http.HandlerFunc(api.onuAction)))))
	mux.Handle("GET /api/v1/olts/{oltID}/uncfg", api.authenticate(api.authorize(auth.PermissionRouterManage, http.HandlerFunc(api.listUnconfiguredONUS))))
	mux.Handle("POST /api/v1/olts/{oltID}/provision-onu", api.authenticate(api.authorize(auth.PermissionRouterManage, api.requireCSRF(http.HandlerFunc(api.provisionONU)))))
	mux.Handle("POST /api/v1/olts/{oltID}/onus-cli/{action}", api.authenticate(api.authorize(auth.PermissionRouterManage, api.requireCSRF(http.HandlerFunc(api.onuActionCli)))))
	mux.Handle("GET /api/v1/olts/{oltID}/onu-traffic-cli", api.authenticate(api.authorize(auth.PermissionRouterManage, http.HandlerFunc(api.onuTrafficCLI))))
	mux.Handle("GET /api/v1/olts/{oltID}/onu-detail-cli", api.authenticate(api.authorize(auth.PermissionRouterManage, http.HandlerFunc(api.onuDetailCLI))))
	mux.Handle("POST /api/v1/olts/{oltID}/onu-config-cli", api.authenticate(api.authorize(auth.PermissionRouterManage, api.requireCSRF(http.HandlerFunc(api.onuConfigCLI)))))
	// Internal/local-only debugging helpers (loopback only).
	mux.Handle("GET /internal/olts/{oltID}/optical-raw", loopbackOnly(http.HandlerFunc(api.internalOpticalRaw)))
	mux.Handle("POST /internal/olts/{oltID}/onu-sync", loopbackOnly(http.HandlerFunc(api.internalONUSyncOne)))
	mux.Handle("GET /internal/olts/{oltID}/optical-table", loopbackOnly(http.HandlerFunc(api.internalOpticalTable)))
	mux.Handle("GET /internal/olts/{oltID}/onu-traffic-cli", loopbackOnly(http.HandlerFunc(api.internalONUTrafficCLI)))
	mux.Handle("GET /internal/olts/{oltID}/cli-raw", loopbackOnly(http.HandlerFunc(api.internalCLIRaw)))
	mux.Handle("GET /internal/olts/{oltID}/onu-oid-scan", loopbackOnly(http.HandlerFunc(api.internalONUOIDScan)))
	mux.Handle("POST /internal/olts/{oltID}/sync-optical", loopbackOnly(http.HandlerFunc(api.internalSyncOptical)))

	return securityHeaders(mux)
}

func loopbackOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientIP := r.Header.Get("X-Forwarded-For")
		if clientIP == "" {
			clientIP = r.Header.Get("X-Real-Ip")
		}
		if i := strings.Index(clientIP, ","); i >= 0 {
			clientIP = clientIP[:i]
		}
		clientIP = strings.TrimSpace(clientIP)
		if clientIP == "" {
			var err error
			clientIP, _, err = net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				clientIP = r.RemoteAddr
			}
		}
		// Akses internal diizinkan dari loopback ATAU jaringan private
		// (gateway container biasanya 172.x saat reverse_proxy ke API).
		ip := net.ParseIP(clientIP)
		if ip == nil || (!ip.IsLoopback() && !ip.IsPrivate()) {
			writeError(w, http.StatusForbidden, "forbidden", "internal endpoint")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (server *server) health(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]string{"status": "ok"})
}

func (server *server) ready(response http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
	defer cancel()
	if err := server.readiness(ctx); err != nil {
		writeError(response, http.StatusServiceUnavailable, "not_ready", "Layanan belum siap.")
		return
	}
	writeJSON(response, http.StatusOK, map[string]string{"status": "ready"})
}

func (server *server) login(response http.ResponseWriter, request *http.Request) {
	request.Body = http.MaxBytesReader(response, request.Body, 16*1024)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decoder.Decode(&input); err != nil || strings.TrimSpace(input.Username) == "" || input.Password == "" {
		writeError(response, http.StatusBadRequest, "invalid_request", "Username dan password wajib diisi.")
		return
	}

	result, err := server.auth.Login(
		request.Context(),
		input.Username,
		input.Password,
		clientIP(request),
		truncate(request.UserAgent(), 512),
	)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			writeError(response, http.StatusUnauthorized, "invalid_credentials", "Username atau password salah.")
			return
		}
		writeError(response, http.StatusInternalServerError, "internal_error", "Terjadi kesalahan internal.")
		return
	}

	http.SetCookie(response, &http.Cookie{
		Name:     sessionCookieName,
		Value:    result.SessionToken,
		Path:     "/",
		Expires:  result.ExpiresAt,
		MaxAge:   int(time.Until(result.ExpiresAt).Seconds()),
		HttpOnly: true,
		Secure:   server.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(response, &http.Cookie{
		Name:     csrfCookieName,
		Value:    result.CSRFToken,
		Path:     "/",
		Expires:  result.ExpiresAt,
		MaxAge:   int(time.Until(result.ExpiresAt).Seconds()),
		HttpOnly: false,
		Secure:   server.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})

	writeJSON(response, http.StatusOK, principalResponse(result.Principal))
}

func (server *server) me(response http.ResponseWriter, request *http.Request) {
	writeJSON(response, http.StatusOK, principalResponse(principalFromContext(request.Context())))
}

func (server *server) changePassword(response http.ResponseWriter, request *http.Request) {
	principal := principalFromContext(request.Context())
	request.Body = http.MaxBytesReader(response, request.Body, 8*1024)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var input struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := decoder.Decode(&input); err != nil || input.CurrentPassword == "" || input.NewPassword == "" {
		writeError(response, http.StatusBadRequest, "invalid_request", "Password lama dan baru wajib diisi.")
		return
	}
	err := server.auth.ChangePassword(request.Context(), principal, input.CurrentPassword, input.NewPassword)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrWrongPassword):
			writeError(response, http.StatusBadRequest, "wrong_password", "Password saat ini salah.")
		case errors.Is(err, auth.ErrInvalidInput):
			writeError(response, http.StatusBadRequest, "invalid_request", "Password baru minimal 12 karakter.")
		default:
			writeError(response, http.StatusInternalServerError, "internal_error", "Terjadi kesalahan internal.")
		}
		return
	}
	writeJSON(response, http.StatusOK, map[string]string{"status": "password_changed"})
}

func (server *server) logout(response http.ResponseWriter, request *http.Request) {
	principal := principalFromContext(request.Context())
	if err := server.auth.Logout(request.Context(), principal.TokenHash); err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "Terjadi kesalahan internal.")
		return
	}
	server.clearAuthCookies(response)
	response.WriteHeader(http.StatusNoContent)
}

func (server *server) listCustomers(response http.ResponseWriter, request *http.Request) {
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
	result, err := server.customers.List(request.Context(), tenantID, customer.ListQuery{
		Page:            page,
		PageSize:        pageSize,
		Search:          queryValues.Get("search"),
		Sort:            queryValues.Get("sort"),
		Order:           queryValues.Get("order"),
		Status:          queryValues.Get("status"),
		IncludeArchived: queryValues.Get("archived") == "include",
	})
	if err != nil {
		if errors.Is(err, customer.ErrInvalidInput) {
			writeError(response, http.StatusBadRequest, "invalid_query", "Parameter pencarian tidak valid.")
			return
		}
		writeError(response, http.StatusInternalServerError, "internal_error", "Terjadi kesalahan internal.")
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (server *server) createCustomer(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, 32*1024)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var input customer.CreateInput
	if err := decoder.Decode(&input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Data pelanggan tidak valid.")
		return
	}
	created, err := server.customers.Create(request.Context(), tenantID, input)
	if err != nil {
		if errors.Is(err, customer.ErrInvalidInput) {
			writeError(response, http.StatusBadRequest, "invalid_request", "Nama atau data pelanggan tidak valid.")
			return
		}
		writeError(response, http.StatusInternalServerError, "internal_error", "Terjadi kesalahan internal.")
		return
	}
	writeJSON(response, http.StatusCreated, created)
}

func (server *server) archiveCustomer(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	err := server.customers.Archive(request.Context(), tenantID, request.PathValue("customerID"))
	if err != nil {
		if errors.Is(err, customer.ErrNotFound) {
			writeError(response, http.StatusNotFound, "not_found", "Pelanggan tidak ditemukan.")
			return
		}
		writeError(response, http.StatusInternalServerError, "internal_error", "Terjadi kesalahan internal.")
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (server *server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		cookie, err := request.Cookie(sessionCookieName)
		if err != nil {
			writeError(response, http.StatusUnauthorized, "unauthenticated", "Sesi login diperlukan.")
			return
		}
		principal, err := server.auth.Authenticate(request.Context(), cookie.Value)
		if err != nil {
			server.clearAuthCookies(response)
			writeError(response, http.StatusUnauthorized, "unauthenticated", "Sesi login tidak valid atau telah berakhir.")
			return
		}
		ctx := context.WithValue(request.Context(), principalContextKey, principal)
		next.ServeHTTP(response, request.WithContext(ctx))
	})
}

func (server *server) requireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		cookie, err := request.Cookie(csrfCookieName)
		headerToken := request.Header.Get("X-CSRF-Token")
		if err != nil || headerToken == "" || cookie.Value != headerToken {
			writeError(response, http.StatusForbidden, "csrf_failed", "Token keamanan request tidak valid.")
			return
		}
		hash := sha256.Sum256([]byte(headerToken))
		principal := principalFromContext(request.Context())
		if subtle.ConstantTimeCompare(hash[:], principal.CSRFHash) != 1 {
			writeError(response, http.StatusForbidden, "csrf_failed", "Token keamanan request tidak valid.")
			return
		}
		next.ServeHTTP(response, request)
	})
}

func (server *server) clearAuthCookies(response http.ResponseWriter) {
	for _, name := range []string{sessionCookieName, csrfCookieName} {
		http.SetCookie(response, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: name == sessionCookieName,
			Secure:   server.cookieSecure,
			SameSite: http.SameSiteLaxMode,
		})
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		response.Header().Set("Referrer-Policy", "no-referrer")
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(response, request)
	})
}

func principalFromContext(ctx context.Context) auth.Principal {
	principal, _ := ctx.Value(principalContextKey).(auth.Principal)
	return principal
}

func principalResponse(principal auth.Principal) map[string]any {
	return map[string]any{
		"id":        principal.UserID,
		"tenant_id": principal.TenantID,
		"username":  principal.Username,
		"role":      principal.Role,
	}
}

func clientIP(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil || net.ParseIP(host) == nil {
		return "127.0.0.1"
	}
	return host
}

func truncate(value string, maxLength int) string {
	if len(value) <= maxLength {
		return value
	}
	return value[:maxLength]
}

func writeError(response http.ResponseWriter, status int, code, message string) {
	writeJSON(response, status, map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	response.Header().Set("Pragma", "no-cache")
	response.Header().Set("Expires", "0")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func (server *server) authorize(permission string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		principal := principalFromContext(request.Context())
		// Super Admin adalah pemilik platform dan boleh bertindak atas
		// nama Mitra (tenant tetap divalidasi lewat server.tenantID).
		if principal.Role != auth.RoleSuperAdmin && !auth.HasPermission(principal.Role, permission) {
			writeError(response, http.StatusForbidden, "forbidden", "Anda tidak memiliki izin untuk tindakan ini.")
			return
		}
		next.ServeHTTP(response, request)
	})
}

func positiveInt(raw string, defaultValue int) (int, error) {
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return 0, errors.New("value must be a positive integer")
	}
	return value, nil
}

func (server *server) mitraDashboard(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	snapshot, err := server.dashboard.Snapshot(request.Context(), tenantID)
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "Dashboard belum dapat dimuat.")
		return
	}
	writeJSON(response, http.StatusOK, snapshot)
}
