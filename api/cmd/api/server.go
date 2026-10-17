package main

import (
	"net/http"
	"time"
)

// The server's time limits are links in a chain that must fire innermost
// first: query, request, write, nginx. TestTimeLimitsFireInnermostFirst
// checks the chain, including the numbers in web/nginx.conf.
const (
	serverReadHeaderTimeout = 10 * time.Second
	// serverReadTimeout bounds the whole request, not just its headers:
	// ReadHeaderTimeout alone leaves a slow body free to hold the
	// connection open indefinitely.
	serverReadTimeout = 15 * time.Second
	// serverWriteTimeout is a backstop behind httpadapter.RequestDeadline.
	serverWriteTimeout = 20 * time.Second
	// serverIdleTimeout must stay above nginx's upstream keepalive_timeout
	// in web/nginx.conf, so nginx always drops an idle connection first.
	// Don't leave it unset: net/http then uses ReadTimeout, which is
	// shorter than nginx's.
	serverIdleTimeout = 90 * time.Second
)

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: serverReadHeaderTimeout,
		ReadTimeout:       serverReadTimeout,
		WriteTimeout:      serverWriteTimeout,
		IdleTimeout:       serverIdleTimeout,
		// MaxHeaderBytes is set explicitly rather than left to net/http's
		// unstated default: it is the request-size bound on the *server*,
		// alongside the JSON-body bound (httpadapter's maxRequestBodyBytes,
		// in errors.go) that lives in the handler layer.
		MaxHeaderBytes: 1 << 20,
	}
}
