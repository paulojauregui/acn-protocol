package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/paulojauregui/acn-protocol/internal/api"
)

// TestDefaultConfig verifies default port and timeout configuration initialization.
//
// Pre-conditions: none.
// Post-conditions: confirms canonical timeouts and environment variable override logic.
func TestDefaultConfig(t *testing.T) {
	origPort := os.Getenv("PORT")
	defer func() {
		_ = os.Setenv("PORT", origPort)
	}()

	_ = os.Unsetenv("PORT")
	cfg := DefaultConfig()
	if cfg.Port != DefaultPort {
		t.Fatalf("expected default port %s, got %s", DefaultPort, cfg.Port)
	}
	if cfg.ReadHeaderTimeout != 5*time.Second {
		t.Fatalf("expected ReadHeaderTimeout 5s, got %v", cfg.ReadHeaderTimeout)
	}
	if cfg.ReadTimeout != 15*time.Second {
		t.Fatalf("expected ReadTimeout 15s, got %v", cfg.ReadTimeout)
	}
	if cfg.WriteTimeout != 15*time.Second {
		t.Fatalf("expected WriteTimeout 15s, got %v", cfg.WriteTimeout)
	}
	if cfg.IdleTimeout != 60*time.Second {
		t.Fatalf("expected IdleTimeout 60s, got %v", cfg.IdleTimeout)
	}
	if cfg.ShutdownTimeout != 5*time.Second {
		t.Fatalf("expected ShutdownTimeout 5s, got %v", cfg.ShutdownTimeout)
	}

	_ = os.Setenv("PORT", "9095")
	cfgOverride := DefaultConfig()
	if cfgOverride.Port != "9095" {
		t.Fatalf("expected overridden port 9095, got %s", cfgOverride.Port)
	}
}

// TestBuildServer verifies constructing an http.Server with configured timeouts.
//
// Pre-conditions: none.
// Post-conditions: confirms http.Server fields match configuration parameters.
func TestBuildServer(t *testing.T) {
	cfg := DefaultConfig()
	srv := api.NewServer()
	httpServer := BuildServer(cfg, srv)

	if httpServer.Addr != ":8000" {
		t.Fatalf("expected Addr :8000, got %s", httpServer.Addr)
	}
	if httpServer.ReadHeaderTimeout != 5*time.Second {
		t.Fatalf("expected ReadHeaderTimeout 5s, got %v", httpServer.ReadHeaderTimeout)
	}
	if httpServer.ReadTimeout != 15*time.Second {
		t.Fatalf("expected ReadTimeout 15s, got %v", httpServer.ReadTimeout)
	}
	if httpServer.WriteTimeout != 15*time.Second {
		t.Fatalf("expected WriteTimeout 15s, got %v", httpServer.WriteTimeout)
	}
	if httpServer.IdleTimeout != 60*time.Second {
		t.Fatalf("expected IdleTimeout 60s, got %v", httpServer.IdleTimeout)
	}
	if httpServer.Handler == nil {
		t.Fatalf("expected non-nil Handler")
	}
}

// TestRun_LifecycleAndEndpoints verifies node startup, HTTP endpoint serving, and graceful shutdown.
//
// Pre-conditions: none.
// Post-conditions: asserts endpoints /healthz, /ready, /, and /api/v1/overview function correctly,
// and context cancellation triggers bounded graceful shutdown without error.
func TestRun_LifecycleAndEndpoints(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := Config{
		Port:              "127.0.0.1:0",
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		ShutdownTimeout:   5 * time.Second,
	}

	ready := make(chan net.Addr, 1)
	errCh := make(chan error, 1)

	go func() {
		errCh <- Run(ctx, cfg, ready)
	}()

	var addr net.Addr
	select {
	case addr = <-ready:
	case err := <-errCh:
		t.Fatalf("server failed to start: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for server listener to bind")
	}

	baseURL := "http://" + addr.String()
	client := &http.Client{Timeout: 3 * time.Second}

	// 1. Check /healthz probe
	respHealth, err := client.Get(baseURL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz failed: %v", err)
	}
	defer respHealth.Body.Close()
	if respHealth.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK from /healthz, got %d", respHealth.StatusCode)
	}
	var health api.HealthResponse
	if err := json.NewDecoder(respHealth.Body).Decode(&health); err != nil {
		t.Fatalf("failed to decode health response: %v", err)
	}
	if health.Status != "healthy" {
		t.Fatalf("unexpected health status: %s", health.Status)
	}

	// 2. Check /ready probe
	respReady, err := client.Get(baseURL + "/ready")
	if err != nil {
		t.Fatalf("GET /ready failed: %v", err)
	}
	defer respReady.Body.Close()
	if respReady.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK from /ready, got %d", respReady.StatusCode)
	}

	// 3. Check embedded UI at GET /
	respUI, err := client.Get(baseURL + "/")
	if err != nil {
		t.Fatalf("GET / failed: %v", err)
	}
	defer respUI.Body.Close()
	if respUI.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK from /, got %d", respUI.StatusCode)
	}
	uiBody, err := io.ReadAll(respUI.Body)
	if err != nil {
		t.Fatalf("failed to read / response body: %v", err)
	}
	if !strings.Contains(string(uiBody), "ACN Protocol") {
		t.Fatalf("expected page body to contain 'ACN Protocol', got: %s", string(uiBody))
	}

	// 4. Check /api/v1/overview
	respOverview, err := client.Get(baseURL + "/api/v1/overview")
	if err != nil {
		t.Fatalf("GET /api/v1/overview failed: %v", err)
	}
	defer respOverview.Body.Close()
	if respOverview.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK from /api/v1/overview, got %d", respOverview.StatusCode)
	}

	// Trigger graceful cancellation
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected clean shutdown, got error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for graceful shutdown to finish")
	}
}

// TestRun_InvalidPort verifies error handling when listener port cannot be bound.
//
// Pre-conditions: none.
// Post-conditions: confirms Run returns non-nil error when port binding fails.
func TestRun_InvalidPort(t *testing.T) {
	ctx := context.Background()
	cfg := Config{
		Port:              "99999999",
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		ShutdownTimeout:   1 * time.Second,
	}

	err := Run(ctx, cfg, nil)
	if err == nil {
		t.Fatalf("expected error binding to invalid port, got nil")
	}
}

// TestRun_PortConflict verifies error handling when listener port is already occupied.
//
// Pre-conditions: none.
// Post-conditions: confirms Run returns error when TCP address is already bound.
func TestRun_PortConflict(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to bind test listener: %v", err)
	}
	defer ln.Close()

	ctx := context.Background()
	cfg := Config{
		Port:              ln.Addr().String(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		ShutdownTimeout:   1 * time.Second,
	}

	runErr := Run(ctx, cfg, nil)
	if runErr == nil {
		t.Fatalf("expected address conflict error, got nil")
	}
}
