// Package server composes HTTP APIs and the generated browser client.
package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// NewHandler mounts versioned APIs before the generated static client.
func NewHandler(authHandler, profileHandler, roomsHandler, websocketHandler http.Handler, webRoot string) (http.Handler, error) {
	return NewHandlerWithSecurity(authHandler, profileHandler, roomsHandler, websocketHandler, webRoot, DefaultSecurityConfig())
}

// NewHandlerWithSecurity mounts the application with explicit public boundary controls.
func NewHandlerWithSecurity(authHandler, profileHandler, roomsHandler, websocketHandler http.Handler, webRoot string, securityConfig SecurityConfig) (http.Handler, error) {
	if authHandler == nil || profileHandler == nil || roomsHandler == nil || websocketHandler == nil || webRoot == "" {
		return nil, errors.New("invalid server configuration")
	}
	if err := securityConfig.validate(); err != nil {
		return nil, err
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
	contentSecurityPolicy, err := buildContentSecurityPolicy(filepath.Join(webRoot, "index.html"))
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.Handle("/api/v1/auth/", authHandler)
	mux.Handle("/api/v1/profile/", profileHandler)
	mux.Handle("/api/v1/profiles/", profileHandler)
	mux.Handle("/api/v1/rooms", roomsHandler)
	mux.Handle("/api/v1/rooms/", roomsHandler)
	mux.Handle("GET /api/v1/ws", websocketHandler)
	mux.Handle("/", http.FileServer(http.Dir(webRoot)))
	return securityHeaders(newBoundaryProtection(mux, securityConfig), contentSecurityPolicy), nil
}

func securityHeaders(next http.Handler, contentSecurityPolicy string) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.Header().Set("X-Frame-Options", "DENY")
		response.Header().Set("Referrer-Policy", "no-referrer")
		response.Header().Set("Cross-Origin-Opener-Policy", "same-origin-allow-popups")
		response.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
		response.Header().Set("Content-Security-Policy", contentSecurityPolicy)
		// The generated shell changes with every local WASM build. Avoid a
		// stale cached index hiding the latest client flow during development;
		// fingerprinted/static assets remain cacheable by the file server.
		if request.URL.Path == "/" || request.URL.Path == "/index.html" {
			response.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(response, request)
	})
}

func buildContentSecurityPolicy(indexPath string) (string, error) {
	index, err := os.ReadFile(indexPath)
	if err != nil {
		return "", fmt.Errorf("read web index for CSP: %w", err)
	}
	hashes, err := inlineScriptHashes(index)
	if err != nil {
		return "", fmt.Errorf("inspect web index scripts: %w", err)
	}
	scriptSources := []string{"'self'", "'wasm-unsafe-eval'", "https://accounts.google.com"}
	scriptSources = append(scriptSources, hashes...)
	return "default-src 'self'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; script-src " + strings.Join(scriptSources, " ") + "; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self' https://accounts.google.com; frame-src https://accounts.google.com; form-action 'self'", nil
}

func inlineScriptHashes(document []byte) ([]string, error) {
	const closeTag = "</script>"
	var hashes []string
	remaining := document
	for {
		start := bytes.Index(remaining, []byte("<script"))
		if start < 0 {
			return hashes, nil
		}
		openEnd := bytes.IndexByte(remaining[start:], '>')
		if openEnd < 0 {
			return nil, errors.New("unterminated script opening tag")
		}
		openEnd += start
		closeStart := bytes.Index(remaining[openEnd+1:], []byte(closeTag))
		if closeStart < 0 {
			return nil, errors.New("unterminated script element")
		}
		closeStart += openEnd + 1
		content := remaining[openEnd+1 : closeStart]
		if len(bytes.TrimSpace(content)) > 0 {
			digest := sha256.Sum256(content)
			hashes = append(hashes, "'sha256-"+base64.StdEncoding.EncodeToString(digest[:])+"'")
		}
		remaining = remaining[closeStart+len(closeTag):]
	}
}
