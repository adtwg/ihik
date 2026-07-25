package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"isp-billing/internal/auth"
	"isp-billing/internal/config"
	"isp-billing/internal/customer"
	"isp-billing/internal/dashboard"
	"isp-billing/internal/httpapi"
	"isp-billing/internal/postgres"
)

func main() {
	applicationConfig, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	pool, err := pgxpool.New(ctx, applicationConfig.DatabaseURL)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("ping database: %v", err)
	}

	authService, err := auth.NewService(
		postgres.NewAuthRepository(pool),
		applicationConfig.SessionLifetime,
		applicationConfig.SessionIdleTime,
	)
	if err != nil {
		log.Fatalf("initialize auth service: %v", err)
	}
	customerService := customer.NewService(postgres.NewCustomerRepository(pool))
	dashboardService := dashboard.NewService(postgres.NewDashboardRepository(pool))

	server := &http.Server{
		Addr:              applicationConfig.Address,
		Handler:           httpapi.NewHandler(authService, customerService, dashboardService, pool.Ping, applicationConfig.CookieSecure),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("api listening on %s", applicationConfig.Address)
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("graceful shutdown failed: %v", err)
		}
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("serve api: %v", err)
		}
	}
}
