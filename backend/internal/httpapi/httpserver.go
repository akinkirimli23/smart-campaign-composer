package httpapi

import (
	"context"
	"net"
	"net/http"
	"time"
)

// NewHTTPServer wraps h in an *http.Server whose request contexts are
// cancelled as soon as Shutdown starts.
//
// http.Server.Shutdown stops accepting connections and then waits for
// in-flight requests to finish; it never cancels them. A long-lived stream
// (SSE) that waits on r.Context() would therefore hold Shutdown until its
// deadline. Deriving every request context from a base context that
// RegisterOnShutdown cancels makes such handlers return at once, so their
// connections go idle and Shutdown completes quickly.
func NewHTTPServer(addr string, h http.Handler) *http.Server {
	base, cancel := context.WithCancel(context.Background())
	srv := &http.Server{
		Addr:    addr,
		Handler: h,
		// Bounds how long a client may take to send headers (slowloris).
		ReadHeaderTimeout: 5 * time.Second,
		// No WriteTimeout on purpose: it would cut off long-lived SSE streams.
		IdleTimeout: 60 * time.Second,
		BaseContext: func(net.Listener) context.Context { return base },
	}
	srv.RegisterOnShutdown(cancel)
	return srv
}
