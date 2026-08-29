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
	profileHandler := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusCreated)
	})
	roomsHandler := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusAccepted)
	})
	websocketHandler := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusSwitchingProtocols)
	})
	handler, err := NewHandler(authHandler, profileHandler, roomsHandler, websocketHandler, webRoot)
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}

	apiResponse := httptest.NewRecorder()
	handler.ServeHTTP(apiResponse, httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/auth/session", nil))
	if apiResponse.Code != http.StatusTeapot {
		t.Fatalf("API status = %d", apiResponse.Code)
	}
	profileResponse := httptest.NewRecorder()
	handler.ServeHTTP(profileResponse, httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/profiles/usr_EREREREREREREREREREREQ", nil))
	if profileResponse.Code != http.StatusCreated {
		t.Fatalf("profile status = %d", profileResponse.Code)
	}
	roomsResponse := httptest.NewRecorder()
	handler.ServeHTTP(roomsResponse, httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/rooms", nil))
	if roomsResponse.Code != http.StatusAccepted {
		t.Fatalf("rooms status = %d", roomsResponse.Code)
	}
	websocketResponse := httptest.NewRecorder()
	handler.ServeHTTP(websocketResponse, httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/ws", nil))
	if websocketResponse.Code != http.StatusSwitchingProtocols {
		t.Fatalf("WebSocket status = %d", websocketResponse.Code)
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

	notFound := http.NotFoundHandler()
	if _, err := NewHandler(nil, notFound, notFound, notFound, t.TempDir()); err == nil {
		t.Fatal("NewHandler(nil auth) error = nil")
	}
	if _, err := NewHandler(notFound, nil, notFound, notFound, t.TempDir()); err == nil {
		t.Fatal("NewHandler(nil profile) error = nil")
	}
	if _, err := NewHandler(notFound, notFound, nil, notFound, t.TempDir()); err == nil {
		t.Fatal("NewHandler(nil rooms) error = nil")
	}
	if _, err := NewHandler(notFound, notFound, notFound, nil, t.TempDir()); err == nil {
		t.Fatal("NewHandler(nil WebSocket) error = nil")
	}
	if _, err := NewHandler(notFound, notFound, notFound, notFound, ""); err == nil {
		t.Fatal("NewHandler(empty web root) error = nil")
	}
	if _, err := NewHandler(notFound, notFound, notFound, notFound, filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("NewHandler(missing web root) error = nil")
	}
	if _, err := NewHandler(notFound, notFound, notFound, notFound, t.TempDir()); err == nil {
		t.Fatal("NewHandler(web root without index.html) error = nil")
	}
}
