// Package main provides the production entrypoint daemon for the Agent Communication Network (ACN) node.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/paulojauregui/acn-protocol/internal/api"
)

// DefaultPort defines the fallback listening port when PORT environment variable is unset.
const DefaultPort = "8000"

// Config encapsulates server runtime parameters and network timeouts.
//
// Pre-conditions: none.
// Post-conditions: provides validated port address and bounded timeouts.
type Config struct {
	Port              string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
}

// DefaultConfig constructs a configuration initialized with the PORT environment
// variable (fallback "8000") and canonical protocol timeouts.
//
// Pre-conditions: none.
// Post-conditions: returns Config initialized with specified timeouts.
func DefaultConfig() Config {
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = DefaultPort
	}
	return Config{
		Port:              port,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		ShutdownTimeout:   5 * time.Second,
	}
}

// BuildServer constructs an *http.Server instance configured with protocol timeouts and the API server handler.
//
// Pre-conditions: srv is non-nil.
// Post-conditions: returns an *http.Server with configured timeouts, address, and handler.
func BuildServer(cfg Config, srv *api.Server) *http.Server {
	addr := cfg.Port
	if !strings.Contains(addr, ":") {
		addr = ":" + addr
	}
	return &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	}
}

// Run executes the server lifecycle, blocking until the context is canceled,
// a fatal listener error occurs, or a termination signal is caught.
//
// Pre-conditions: ctx is non-nil.
// Post-conditions: returns nil on clean shutdown or an error if startup/shutdown fails.
func Run(ctx context.Context, cfg Config, ready chan<- net.Addr) error {
	srv := api.NewServer()
	httpServer := BuildServer(cfg, srv)

	addr := cfg.Port
	if !strings.Contains(addr, ":") {
		addr = ":" + addr
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to bind tcp listener on %s: %w", addr, err)
	}

	if ready != nil {
		ready <- ln.Addr()
	}

	serverErr := make(chan error, 1)
	go func() {
		if err := httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
		close(serverErr)
	}()

	select {
	case <-ctx.Done():
	case err := <-serverErr:
		if err != nil {
			return fmt.Errorf("server listener failure: %w", err)
		}
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown failed: %w", err)
	}

	return nil
}

// main executes the production command-line entrypoint.
//
// Pre-conditions: none.
// Post-conditions: exits 0 upon graceful SIGINT/SIGTERM termination, or exits 1 on failure.
func main() {
	cfg := DefaultConfig()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Printf("Starting ACN protocol node on port %s", cfg.Port)
	if err := Run(ctx, cfg, nil); err != nil {
		log.Fatalf("Fatal node execution error: %v", err)
	}
	log.Printf("ACN protocol node gracefully terminated")
}
