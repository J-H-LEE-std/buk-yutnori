// Package server composes HTTP APIs and the generated browser client.
package server

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

// NewHandler mounts versioned APIs before the generated static client.
func NewHandler(authHandler, websocketHandler http.Handler, webRoot string) (http.Handler, error) {
	if authHandler == nil || websocketHandler == nil || webRoot == "" {
		return nil, errors.New("invalid server configuration")
	}
	info, err := os.Stat(webRoot)
	if err != nil {
		return nil, fmt.Errorf("inspect web root: %w", err)
	}
	if !info.IsDir() {
		return nil, errors.New("web root is not a directory")
	}
	indexInfo, err := os.Stat(filepath.Join(webRoot, "index.html"))
	if err != nil {
		return nil, fmt.Errorf("inspect web index: %w", err)
	}
	if !indexInfo.Mode().IsRegular() {
		return nil, errors.New("web index is not a regular file")
	}

	mux := http.NewServeMux()
	mux.Handle("/api/v1/auth/", authHandler)
	mux.Handle("GET /api/v1/ws", websocketHandler)
	mux.Handle("/", http.FileServer(http.Dir(webRoot)))
	return securityHeaders(mux), nil
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.Header().Set("Referrer-Policy", "no-referrer")
		response.Header().Set("Cross-Origin-Opener-Policy", "same-origin-allow-popups")
		next.ServeHTTP(response, request)
	})
}
