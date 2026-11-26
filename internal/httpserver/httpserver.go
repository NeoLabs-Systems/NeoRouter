package httpserver

import (
	"log/slog"
	"net/http"
	"time"
)

// NewHTTPServer builds the listener configuration of server/index.js: headers
// time out after 65s, the request timeout is unlimited because gateway
// responses stream, and server errors go to the slog logger.
func NewHTTPServer(addr string, h http.Handler, log *slog.Logger) *http.Server {
	if log == nil {
		log = slog.Default()
	}
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 65 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
}
