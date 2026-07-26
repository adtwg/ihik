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
		readiness:     deps.Readiness,
		cookieSecure:  deps.CookieSecure,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", api.health)
	mux.HandleFunc("GET /ready", api.ready)
	mux.HandleFunc("POST /api/v1/auth/login", api.login)
	mux.Handle("GET /api/v1/me", api.authenticate(http.HandlerFunc(api.me)))
	mux.Handle("POST /api/v1/auth/logout", api.authenticate(api.requireCSRF(http.HandlerFunc(api.logout))))
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
	return securityHeaders(mux)
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
		SameSite: http.SameSiteStrictMode,
	})
	http.SetCookie(response, &http.Cookie{
		Name:     csrfCookieName,
		Value:    result.CSRFToken,
		Path:     "/",
		Expires:  result.ExpiresAt,
		MaxAge:   int(time.Until(result.ExpiresAt).Seconds()),
		HttpOnly: false,
		Secure:   server.cookieSecure,
		SameSite: http.SameSiteStrictMode,
	})

	writeJSON(response, http.StatusOK, principalResponse(result.Principal))
}

func (server *server) me(response http.ResponseWriter, request *http.Request) {
	writeJSON(response, http.StatusOK, principalResponse(principalFromContext(request.Context())))
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
	principal := principalFromContext(request.Context())
	if principal.TenantID == nil {
		writeError(response, http.StatusForbidden, "forbidden", "Akses tenant diperlukan.")
		return
	}
	queryValues := request.URL.Query()
	page, pageErr := positiveInt(queryValues.Get("page"), 1)
	pageSize, pageSizeErr := positiveInt(queryValues.Get("page_size"), 25)
	if pageErr != nil || pageSizeErr != nil {
		writeError(response, http.StatusBadRequest, "invalid_query", "Parameter halaman tidak valid.")
		return
	}
	result, err := server.customers.List(request.Context(), *principal.TenantID, customer.ListQuery{
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
	principal := principalFromContext(request.Context())
	if principal.TenantID == nil {
		writeError(response, http.StatusForbidden, "forbidden", "Akses tenant diperlukan.")
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
	created, err := server.customers.Create(request.Context(), *principal.TenantID, input)
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
	principal := principalFromContext(request.Context())
	if principal.TenantID == nil {
		writeError(response, http.StatusForbidden, "forbidden", "Akses tenant diperlukan.")
		return
	}
	err := server.customers.Archive(request.Context(), *principal.TenantID, request.PathValue("customerID"))
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
			SameSite: http.SameSiteStrictMode,
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
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func (server *server) authorize(permission string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		principal := principalFromContext(request.Context())
		if !auth.HasPermission(principal.Role, permission) {
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
	principal := principalFromContext(request.Context())
	if principal.TenantID == nil {
		writeError(response, http.StatusForbidden, "forbidden", "Akses tenant diperlukan.")
		return
	}
	snapshot, err := server.dashboard.Snapshot(request.Context(), *principal.TenantID)
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "Dashboard belum dapat dimuat.")
		return
	}
	writeJSON(response, http.StatusOK, snapshot)
}
