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
	"github.com/domostack/arbiter/internal/policycache"
	"github.com/domostack/arbiter/internal/store"
	"github.com/rs/zerolog"
)

// Server wraps the HTTP server
type Server struct {
	httpServer *http.Server
	handlers   *Handlers
	logger     zerolog.Logger
}

// NewServer creates a new HTTP server
func NewServer(
	bindAddr string,
	engine *arbiter.Engine,
	cache *policycache.Cache,
	store store.Store,
	logger zerolog.Logger,
	killswitchPublicHost, gatekeeperPublicHost string,
	staticDir string,
) *Server {
	handlers := NewHandlers(engine, cache, store, logger, killswitchPublicHost, gatekeeperPublicHost)

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
