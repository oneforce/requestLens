package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"requestlens/internal/api"
	"requestlens/internal/config"
	"requestlens/internal/db"
	"requestlens/internal/proxy"
	"requestlens/web"
)

func main() {
	cfg := config.Load()

	store, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer store.Close()

	if err := store.CleanupOldLogs(context.Background(), cfg.LogRetentionDays); err != nil {
		log.Printf("cleanup old logs: %v", err)
	}

	apiHandler := api.NewHandler(store, cfg)
	proxyHandler := proxy.NewHandler(store, cfg)
	webHandler := web.NewHandler()
	protectedAPIHandler := requireManagementToken(cfg.AuthToken, apiHandler)

	root := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/"):
			protectedAPIHandler.ServeHTTP(w, r)
		case r.URL.Path == "/" || strings.HasPrefix(r.URL.Path, "/assets/") || r.URL.Path == "/favicon.ico":
			webHandler.ServeHTTP(w, r)
		default:
			proxyHandler.ServeHTTP(w, r)
		}
	})

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           root,
		ReadHeaderTimeout: cfg.ResponseHeaderTimeout,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("RequestLens listening on %s", cfg.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

func requireManagementToken(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPublicAPI(r.URL.Path) || tokenMatchesRequest(r, token) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"ok":false,"data":null,"error":{"code":"unauthorized","message":"需要访问 Token"}}`))
	})
}

func isPublicAPI(path string) bool {
	return path == "/api/auth/status"
}

func tokenMatchesRequest(r *http.Request, expected string) bool {
	candidates := []string{
		r.Header.Get("X-RequestLens-Token"),
		r.URL.Query().Get("token"),
	}
	if raw := r.Header.Get("Authorization"); strings.HasPrefix(strings.ToLower(raw), "bearer ") {
		candidates = append(candidates, strings.TrimSpace(raw[len("bearer "):]))
	}
	if cookie, err := r.Cookie("requestlens_token"); err == nil {
		candidates = append(candidates, cookie.Value)
	}
	for _, candidate := range candidates {
		if constantTimeEqual(candidate, expected) {
			return true
		}
	}
	return false
}

func constantTimeEqual(candidate string, expected string) bool {
	if candidate == "" || len(candidate) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(candidate), []byte(expected)) == 1
}
