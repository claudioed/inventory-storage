package http

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// RequestLogger returns chi middleware that logs each request at Info level
// (Error for 5xx responses) with method, path, status, duration, response
// size, and the chi request ID.
func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()

			next.ServeHTTP(ww, r)

			duration := time.Since(start)
			attrs := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"duration_ms", duration.Milliseconds(),
				"bytes", ww.BytesWritten(),
				"request_id", middleware.GetReqID(r.Context()),
			}

			// *Context variants, not the bare ones: the request context is
			// what carries the active span, and the telemetry slog handler
			// reads trace_id/span_id off it.
			if ww.Status() >= http.StatusInternalServerError {
				logger.ErrorContext(r.Context(), "http request", attrs...)
			} else {
				logger.InfoContext(r.Context(), "http request", attrs...)
			}
		})
	}
}
