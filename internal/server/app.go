// Package server composes HTTP APIs and the generated browser client.
package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/net/html"
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
	return "default-src 'self'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; script-src " + strings.Join(scriptSources, " ") + "; style-src 'self' 'unsafe-inline' https://accounts.google.com/gsi/style; img-src 'self' data:; font-src 'self'; connect-src 'self' https://accounts.google.com; frame-src https://accounts.google.com; form-action 'self'", nil
}

func inlineScriptHashes(document []byte) ([]string, error) {
	var hashes []string
	tokenizer := html.NewTokenizer(bytes.NewReader(document))
	var script []byte
	inScript := false
	for {
		tokenType := tokenizer.Next()
		switch tokenType {
		case html.ErrorToken:
			if err := tokenizer.Err(); err != nil && !errors.Is(err, io.EOF) {
				return nil, fmt.Errorf("tokenize web index: %w", err)
			}
			if inScript {
				return nil, errors.New("unterminated script element")
			}
			return hashes, nil
		case html.StartTagToken, html.SelfClosingTagToken:
			name, _ := tokenizer.TagName()
			if bytes.Equal(name, []byte("script")) {
				hasSource := false
				for {
					key, _, moreAttr := tokenizer.TagAttr()
					if bytes.Equal(key, []byte("src")) {
						hasSource = true
					}
					if !moreAttr {
						break
					}
				}
				inScript = !hasSource
				script = script[:0]
			}
		case html.EndTagToken:
			name, _ := tokenizer.TagName()
			if inScript && bytes.Equal(name, []byte("script")) {
				content := normalizeInlineScriptForCSP(script)
				if len(bytes.TrimSpace(content)) > 0 {
					digest := sha256.Sum256(content)
					hashes = append(hashes, "'sha256-"+base64.StdEncoding.EncodeToString(digest[:])+"'")
				}
				inScript = false
				script = nil
			}
		case html.TextToken:
			if inScript {
				script = append(script, tokenizer.Text()...)
			}
		}
	}
}

func normalizeInlineScriptForCSP(content []byte) []byte {
	// The HTML tokenizer has already normalized line endings. Replace NUL with
	// U+FFFD to match browser HTML preprocessing before the CSP hash is applied.
	return bytes.ReplaceAll(content, []byte{0}, []byte("\xef\xbf\xbd"))
}
