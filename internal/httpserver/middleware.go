package httpserver

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"
)

// RequestLogger creates a middleware that logs HTTP requests
func RequestLogger(logger zerolog.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := logger.With().
				Str("method", r.Method).
				Str("url", r.URL.String()).
				Str("remote_addr", r.RemoteAddr).
				Logger().
				WithContext(r.Context())
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// AccessLogger creates a middleware that logs access information
func AccessLogger(logger zerolog.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			duration := time.Since(start)
			logger.Info().
				Str("method", r.Method).
				Str("url", r.URL.String()).
				Int("status", ww.Status()).
				Int("size", ww.BytesWritten()).
				Dur("duration", duration).
				Msg("request")
		})
	}
}

// RequestID creates a middleware that adds a request ID to the context
func RequestID() func(next http.Handler) http.Handler {
	return middleware.RequestID
}

// Recoverer creates a middleware that recovers from panics
func Recoverer() func(next http.Handler) http.Handler {
	return middleware.Recoverer
}

// TraceID extracts trace ID from request headers or generates one
func TraceID(r *http.Request) string {
	if traceID := r.Header.Get("X-Request-Id"); traceID != "" {
		return traceID
	}
	if traceID := middleware.GetReqID(r.Context()); traceID != "" {
		return traceID
	}
	return ""
}
