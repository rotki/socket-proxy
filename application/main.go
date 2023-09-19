package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"
)

// allowedPaths is a list of path substrings that are allowed to be proxied.
// If the request URL path does not contain any of these substrings, the request is blocked.
var allowedPaths = []string{
	"version",
	"events",
	"containers",
}

var (
	allowedNetwork *net.IPNet
	socketProxy    *httputil.ReverseProxy
)

func main() {
	slog.Info("starting socket-proxy", "version", version, "os", runtime.GOOS, "arch", runtime.GOARCH, "runtime", runtime.Version(), "URL", programUrl)
	initConfig()
	slog.Info("configuration is", "socketpath", socketPath, "proxyport", proxyPort, "loglevel", logLevel, "logjson", logJSON, "allowcidr", allowFrom)

	// define the reverse proxy
	socketUrlDummy, _ := url.Parse("http://localhost") // dummy URL - we use the unix socket
	socketProxy = httputil.NewSingleHostReverseProxy(socketUrlDummy)
	socketProxy.Transport = &http.Transport{
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("unix", socketPath)
		},
	}

	// start the server in a goroutine
	srv := &http.Server{
		Addr:    ":" + proxyPort,
		Handler: http.HandlerFunc(handleHttpRequest),
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http server problem", "error", err)
			os.Exit(2)
		}
	}()

	// Wait for stop signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	// Try to shut down gracefully
	slog.Info("received stop signal - shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), maxGracefulShutdownTime*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Warn("timeout stopping server (maybe client still running?) - forcing shutdown", "error", err)
		os.Exit(3)
	}
	slog.Info("graceful shutdown complete - exiting")
}
