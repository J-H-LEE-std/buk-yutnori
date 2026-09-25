package server

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNewHandlerRoutesAPIBeforeStaticClientAndSetsSecurityHeaders(t *testing.T) {
	t.Parallel()

	webRoot := t.TempDir()
	inlineScript := "window.clientBooted=true"
	index := "<script>" + inlineScript + "</script>client shell"
	if err := os.WriteFile(filepath.Join(webRoot, "index.html"), []byte(index), 0o600); err != nil {
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
	if staticResponse.Code != http.StatusOK || staticResponse.Body.String() != index {
		t.Fatalf("static response = %d %q", staticResponse.Code, staticResponse.Body.String())
	}
	if got := staticResponse.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
	if got := staticResponse.Header().Get("Cross-Origin-Opener-Policy"); got != "same-origin-allow-popups" {
		t.Fatalf("Cross-Origin-Opener-Policy = %q", got)
	}
	if got := staticResponse.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options = %q", got)
	}
	if got := staticResponse.Header().Get("Content-Security-Policy"); got == "" || strings.Contains(strings.Split(got, "; style-src")[0], "'unsafe-inline'") {
		t.Fatalf("unsafe or empty Content-Security-Policy = %q", got)
	} else {
		digest := sha256.Sum256([]byte(inlineScript))
		wantHash := "'sha256-" + base64.StdEncoding.EncodeToString(digest[:]) + "'"
		if !strings.Contains(got, wantHash) {
			t.Fatalf("Content-Security-Policy = %q, missing %s", got, wantHash)
		}
		if !strings.Contains(got, "style-src 'self' 'unsafe-inline' https://accounts.google.com/gsi/style") {
			t.Fatalf("Content-Security-Policy blocks Google Identity Services styles: %q", got)
		}
	}
	if got := staticResponse.Header().Get("Permissions-Policy"); got == "" {
		t.Fatal("Permissions-Policy is empty")
	}
	if got := staticResponse.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
}

func TestInlineScriptHashesUseBrowserHTMLPreprocessing(t *testing.T) {
	t.Parallel()

	// Browsers replace NUL with U+FFFD and normalize CR line endings while
	// tokenizing script text before applying CSP. Hashing the file bytes instead
	// leaves generated Emscripten shells permanently blocked at startup.
	document := []byte("<script>const key = `left\x00right`;\r\nboot();\r</script>")
	hashes, err := inlineScriptHashes(document)
	if err != nil {
		t.Fatal(err)
	}
	normalized := "const key = `left\uFFFDright`;\nboot();\n"
	digest := sha256.Sum256([]byte(normalized))
	want := "'sha256-" + base64.StdEncoding.EncodeToString(digest[:]) + "'"
	if len(hashes) != 1 || hashes[0] != want {
		t.Fatalf("inlineScriptHashes() = %q, want %q", hashes, want)
	}
}

func TestInlineScriptHashesTokenizeScriptElements(t *testing.T) {
	tests := []struct {
		name     string
		document string
		want     string
	}{
		{
			name:     "quoted greater-than in attribute and case-insensitive tag",
			document: `<SCRIPT data-value=">">boot()</SCRIPT >`,
			want:     "boot()",
		},
		{
			name:     "script-like text in comment is ignored",
			document: `<!-- <script>not code</script> --><script>real()</script>`,
			want:     "real()",
		},
		{
			name:     "script prefix is not an element",
			document: `<scripting>not code</scripting><script>real()</script>`,
			want:     "real()",
		},
		{
			name:     "external script body is not hash authorized",
			document: `<script src="/external.js">fallback()</script><script>real()</script>`,
			want:     "real()",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			hashes, err := inlineScriptHashes([]byte(test.document))
			if err != nil {
				t.Fatalf("inlineScriptHashes() error = %v", err)
			}
			digest := sha256.Sum256([]byte(test.want))
			want := "'sha256-" + base64.StdEncoding.EncodeToString(digest[:]) + "'"
			if len(hashes) != 1 || hashes[0] != want {
				t.Fatalf("inlineScriptHashes() = %q, want [%q]", hashes, want)
			}
		})
	}
}

func TestInlineScriptHashesRejectUnterminatedScript(t *testing.T) {
	if _, err := inlineScriptHashes([]byte(`<script>boot()`)); err == nil {
		t.Fatal("inlineScriptHashes() accepted an unterminated script element")
	}
}

func TestBoundaryProtectionRateLimitsSensitiveRoutesAndIgnoresSpoofedForwarding(t *testing.T) {
	t.Parallel()
	webRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(webRoot, "index.html"), []byte("client"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	next := http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		calls++
		response.WriteHeader(http.StatusNoContent)
	})
	config := DefaultSecurityConfig()
	config.LoginLimit = 2
	handler, err := NewHandlerWithSecurity(next, next, next, next, webRoot, config)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 3; index++ {
		request := httptest.NewRequest(http.MethodPost, "https://game.example/api/v1/auth/google", nil)
		request.RemoteAddr = "198.51.100.20:1234"
		request.Header.Set("X-Forwarded-For", "203.0.113."+string(rune('1'+index)))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if index < 2 && response.Code != http.StatusNoContent {
			t.Fatalf("attempt %d status = %d", index+1, response.Code)
		}
		if index == 2 {
			if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" {
				t.Fatalf("limited response = %d headers=%v", response.Code, response.Header())
			}
		}
	}
	if calls != 2 {
		t.Fatalf("downstream calls = %d, want 2", calls)
	}
}

func TestBoundaryProtectionUsesForwardedClientOnlyFromTrustedProxy(t *testing.T) {
	t.Parallel()
	config := DefaultSecurityConfig()
	config.LoginLimit = 1
	config.TrustedProxyCIDRs = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	protection := newBoundaryProtection(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}), config)
	for _, client := range []string{"198.51.100.1", "198.51.100.2"} {
		request := httptest.NewRequest(http.MethodPost, "https://game.example/api/v1/auth/google", nil)
		request.RemoteAddr = "10.0.0.2:8080"
		request.Header.Set("X-Forwarded-For", client+", 10.0.0.3")
		response := httptest.NewRecorder()
		protection.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("client %s status = %d", client, response.Code)
		}
	}
}

func TestBoundaryProtectionSafelyCombinesMultipleForwardedHeaderLines(t *testing.T) {
	t.Parallel()
	config := DefaultSecurityConfig()
	config.LoginLimit = 1
	config.TrustedProxyCIDRs = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	protection := newBoundaryProtection(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}), config)
	for attempt := 0; attempt < 2; attempt++ {
		request := httptest.NewRequest(http.MethodPost, "https://game.example/api/v1/auth/google", nil)
		request.RemoteAddr = "10.0.0.2:8080"
		request.Header.Add("X-Forwarded-For", "203.0.113.99")
		request.Header.Add("X-Forwarded-For", "198.51.100.20, 10.0.0.3")
		response := httptest.NewRecorder()
		protection.ServeHTTP(response, request)
		want := http.StatusNoContent
		if attempt == 1 {
			want = http.StatusTooManyRequests
		}
		if response.Code != want {
			t.Fatalf("attempt %d status = %d, want %d", attempt+1, response.Code, want)
		}
	}
}

func TestBoundaryProtectionLimitsRoomJoinPerClientAcrossRoomIDs(t *testing.T) {
	t.Parallel()
	config := DefaultSecurityConfig()
	config.JoinLimit = 1
	protection := newBoundaryProtection(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}), config)

	request := func(client, room string) int {
		req := httptest.NewRequest(http.MethodPost, "https://game.example/api/v1/rooms/"+room+"/join", nil)
		req.RemoteAddr = client + ":1234"
		response := httptest.NewRecorder()
		protection.ServeHTTP(response, req)
		return response.Code
	}
	if got := request("198.51.100.1", "room-a"); got != http.StatusNoContent {
		t.Fatalf("first join status = %d", got)
	}
	if got := request("198.51.100.1", "room-a"); got != http.StatusTooManyRequests {
		t.Fatalf("repeated join status = %d", got)
	}
	if got := request("198.51.100.1", "room-b"); got != http.StatusTooManyRequests {
		t.Fatalf("different room status = %d", got)
	}
	if got := request("198.51.100.2", "room-a"); got != http.StatusNoContent {
		t.Fatalf("different client status = %d", got)
	}
}

func TestParseTrustedProxyCIDRsRejectsMalformedInput(t *testing.T) {
	t.Parallel()
	for _, input := range []string{"10.0.0.0/8,not-a-network", "0.0.0.0/0", "::/0"} {
		if _, err := ParseTrustedProxyCIDRs(input); err == nil {
			t.Fatalf("ParseTrustedProxyCIDRs(%q) error = nil", input)
		}
	}
}

func TestLimiterRejectsNewKeysUntilTheOldestWindowExpires(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.September, 23, 0, 0, 0, 0, time.UTC)
	limiter := newFixedWindowLimiter(func() time.Time { return now }, 1, time.Minute)
	for index := 0; index < maxLimiterEntries; index++ {
		if allowed, _ := limiter.allow(strconv.Itoa(index)); !allowed {
			t.Fatalf("initial key %d was rejected", index)
		}
	}
	if allowed, retryAfter := limiter.allow("new-a"); allowed || retryAfter != time.Minute {
		t.Fatalf("new key at saturation = (%v, %v), want (false, 1m)", allowed, retryAfter)
	}
	if got := len(limiter.entries); got != maxLimiterEntries {
		t.Fatalf("limiter entries = %d", got)
	}
	if _, exists := limiter.entries["0"]; !exists {
		t.Fatal("unexpired oldest entry was evicted")
	}

	now = now.Add(time.Minute)
	if allowed, _ := limiter.allow("new-a"); !allowed {
		t.Fatal("new key was rejected after stored windows expired")
	}
	if _, exists := limiter.entries["new-a"]; !exists {
		t.Fatal("new entry is missing")
	}
	if got := len(limiter.entries); got != 1 {
		t.Fatalf("expired entries retained = %d", got)
	}
}

func TestBoundaryProtectionGroupsIPv6ClientsBy64Prefix(t *testing.T) {
	t.Parallel()
	config := DefaultSecurityConfig()
	config.LoginLimit = 1
	protection := newBoundaryProtection(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}), config)
	for index, remote := range []string{"[2001:db8:1234:5678::1]:1234", "[2001:db8:1234:5678::ffff]:5678"} {
		request := httptest.NewRequest(http.MethodPost, "https://game.example/api/v1/auth/google", nil)
		request.RemoteAddr = remote
		response := httptest.NewRecorder()
		protection.ServeHTTP(response, request)
		want := http.StatusNoContent
		if index == 1 {
			want = http.StatusTooManyRequests
		}
		if response.Code != want {
			t.Fatalf("IPv6 attempt %d status = %d, want %d", index+1, response.Code, want)
		}
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
