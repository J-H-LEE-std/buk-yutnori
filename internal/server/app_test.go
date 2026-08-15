package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestNewHandlerRoutesAPIBeforeStaticClientAndSetsSecurityHeaders(t *testing.T) {
	t.Parallel()

	webRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(webRoot, "index.html"), []byte("client shell"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	authHandler := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusTeapot)
	})
	handler, err := NewHandler(authHandler, webRoot)
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}

	apiResponse := httptest.NewRecorder()
	handler.ServeHTTP(apiResponse, httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/auth/session", nil))
	if apiResponse.Code != http.StatusTeapot {
		t.Fatalf("API status = %d", apiResponse.Code)
	}

	staticResponse := httptest.NewRecorder()
	handler.ServeHTTP(staticResponse, httptest.NewRequest(http.MethodGet, "http://localhost/", nil))
	if staticResponse.Code != http.StatusOK || staticResponse.Body.String() != "client shell" {
		t.Fatalf("static response = %d %q", staticResponse.Code, staticResponse.Body.String())
	}
	if got := staticResponse.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
	if got := staticResponse.Header().Get("Cross-Origin-Opener-Policy"); got != "same-origin-allow-popups" {
		t.Fatalf("Cross-Origin-Opener-Policy = %q", got)
	}
}

func TestNewHandlerRejectsMissingDependencies(t *testing.T) {
	t.Parallel()

	if _, err := NewHandler(nil, t.TempDir()); err == nil {
		t.Fatal("NewHandler(nil) error = nil")
	}
	if _, err := NewHandler(http.NotFoundHandler(), ""); err == nil {
		t.Fatal("NewHandler(empty web root) error = nil")
	}
	if _, err := NewHandler(http.NotFoundHandler(), filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("NewHandler(missing web root) error = nil")
	}
	if _, err := NewHandler(http.NotFoundHandler(), t.TempDir()); err == nil {
		t.Fatal("NewHandler(web root without index.html) error = nil")
	}
}
