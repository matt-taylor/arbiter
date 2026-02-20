package httpserver

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/domostack/arbiter/internal/arbiter"
	"github.com/domostack/arbiter/internal/config"
	"github.com/domostack/arbiter/internal/policycache"
	"github.com/domostack/arbiter/internal/store"
	"github.com/domostack/arbiter/internal/telemetry"
	"github.com/domostack/arbiter/internal/telemetry/query"
	"github.com/rs/zerolog"
)

// Server wraps the HTTP server
type Server struct {
	httpServer *http.Server
	handlers   *Handlers
	logger     zerolog.Logger
}

// NewServer creates a new HTTP server.
// telemetryRepo may be nil when the telemetry query API is disabled.
func NewServer(
	bindAddr string,
	engine *arbiter.Engine,
	cache *policycache.Cache,
	store store.Store,
	logger zerolog.Logger,
	killswitchPublicHost, gatekeeperPublicHost string,
	staticDir string,
	publisher telemetry.Publisher,
	telemetryRepo *query.Repository,
	telemetryAPICfg config.TelemetryAPIConfig,
) *Server {
	handlers := NewHandlers(engine, cache, store, logger, killswitchPublicHost, gatekeeperPublicHost, publisher)

	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(RequestLogger(logger))
	r.Use(AccessLogger(logger))

	// API routes
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/check", handlers.HandleCheck)
		r.Get("/policies", handlers.HandleListPolicies)
		r.Post("/policies", handlers.HandleCreatePolicy)
		r.Get("/policies/{id}", handlers.HandleGetPolicy)
		r.Patch("/policies/{id}", handlers.HandleUpdatePolicy)
		r.Delete("/policies/{id}", handlers.HandleDeletePolicy)
		r.Get("/effective", handlers.HandleEffective)
		r.Post("/test/check", handlers.HandleTestCheck)

		// Telemetry Query API (read-only, optional)
		if telemetryRepo != nil {
			// SECURITY: These endpoints expose internal traffic intelligence (top IPs,
			// paths, request volumes). If Arbiter is publicly accessible, require
			// upstream authentication (e.g. NGINX auth) or add auth middleware in a
			// future phase (Phase 3.5). No RBAC is implemented now — rate limiting
			// is the only abuse mitigation.
			th := NewTelemetryHandlersWithThresholds(telemetryRepo, logger, telemetryAPICfg.MaxWindowMinutes, telemetryAPICfg.MaxLimit, OverviewThresholds{
				ScannerPathThreshold: telemetryAPICfg.ScannerPathThreshold,
				SprayerHostThreshold: telemetryAPICfg.SprayerHostThreshold,
				BurstinessThreshold:  telemetryAPICfg.BurstinessThreshold,
				PeakRPSThreshold:     telemetryAPICfg.PeakRPSThreshold,
			})
			r.Route("/telemetry", func(r chi.Router) {
				r.Use(TelemetryRateLimiter(10, 20, telemetryAPICfg.TrustProxyHeaders, logger))
			r.Get("/hosts/{host}/top-ips", th.HandleTopIPs)
			r.Get("/hosts/{host}/ips/{ip}/top-paths", th.HandleTopPaths)
			r.Get("/hosts/{host}/summary", th.HandleSummary)

			// Overview endpoints (Phase 3.5)
			r.Get("/overview/top-hosts", th.HandleOverviewTopHosts)
			r.Get("/overview/suspicious-scanners", th.HandleOverviewSuspiciousScanners)
			r.Get("/overview/suspicious-sprayers", th.HandleOverviewSuspiciousSprayers)
			})
		}
	})

	// Health check endpoints
	r.Get("/healthz", handlers.HandleHealthz)
	r.Get("/readyz", handlers.HandleReadyz)

	// Serve static files if directory exists
	if staticDir != "" {
		if _, err := os.Stat(staticDir); err == nil {
			// Serve static files
			fs := http.FileServer(http.Dir(staticDir))
			r.Handle("/*", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Don't serve static files for API routes
				if r.URL.Path == "/api" || len(r.URL.Path) > 4 && r.URL.Path[:4] == "/api" {
					http.NotFound(w, r)
					return
				}
				// Serve index.html for non-API routes
				if r.URL.Path != "/" && filepath.Ext(r.URL.Path) == "" {
					r.URL.Path = "/"
				}
				fs.ServeHTTP(w, r)
			}))
		}
	}

	httpServer := &http.Server{
		Addr:         bindAddr,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	return &Server{
		httpServer: httpServer,
		handlers:   handlers,
		logger:     logger,
	}
}

// Start starts the HTTP server
func (s *Server) Start() error {
	s.logger.Info().Str("addr", s.httpServer.Addr).Msg("starting HTTP server")
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info().Msg("shutting down HTTP server")
	return s.httpServer.Shutdown(ctx)
}
