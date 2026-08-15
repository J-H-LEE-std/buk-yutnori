package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"buk-yutnori/internal/auth"
)

func TestGoogleLoginSetsHardenedThirtyDayCookieWithoutReturningSecrets(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	service := &stubAuthService{loginResult: auth.LoginResult{
		User:      auth.User{ID: "usr_EREREREREREREREREREREQ"},
		Token:     "raw-session-secret",
		ExpiresAt: now.Add(auth.SessionLifetime),
	}}
	handler, err := NewAuthHandler(service, validAuthConfig())
	if err != nil {
		t.Fatalf("NewAuthHandler() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "https://game.example/api/v1/auth/google", strings.NewReader(`{"credential":"google-id-token"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(RequestGuardHeader, RequestGuardValue)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if service.loginCredential != "google-id-token" {
		t.Fatalf("login credential = %q", service.loginCredential)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %+v", cookies)
	}
	cookie := cookies[0]
	if cookie.Name != SessionCookieName || cookie.Value != "raw-session-secret" {
		t.Fatalf("cookie = %+v", cookie)
	}
	if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie security attributes = %+v", cookie)
	}
	if cookie.Path != "/" || cookie.Domain != "" || cookie.MaxAge != int(auth.SessionLifetime/time.Second) {
		t.Fatalf("cookie scope/lifetime = %+v", cookie)
	}
	if !cookie.Expires.Equal(now.Add(auth.SessionLifetime)) {
		t.Fatalf("cookie expiry = %v", cookie.Expires)
	}

	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("response JSON error = %v", err)
	}
	if body["user_id"] != "usr_EREREREREREREREREREREQ" || body["expires_at"] != now.Add(auth.SessionLifetime).Format(time.RFC3339) {
		t.Fatalf("response body = %#v", body)
	}
	if strings.Contains(response.Body.String(), "google-id-token") || strings.Contains(response.Body.String(), "raw-session-secret") {
		t.Fatalf("response leaks credential: %s", response.Body.String())
	}
}

func TestGoogleLoginRequiresSameOriginJSONRequestShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		contentType string
		guard       string
		body        string
		wantStatus  int
	}{
		{name: "missing request guard", contentType: "application/json", body: `{"credential":"token"}`, wantStatus: http.StatusForbidden},
		{name: "form content", contentType: "application/x-www-form-urlencoded", guard: RequestGuardValue, body: "credential=token", wantStatus: http.StatusUnsupportedMediaType},
		{name: "unknown field", contentType: "application/json", guard: RequestGuardValue, body: `{"credential":"token","session":"attacker"}`, wantStatus: http.StatusBadRequest},
		{name: "empty credential", contentType: "application/json", guard: RequestGuardValue, body: `{"credential":""}`, wantStatus: http.StatusBadRequest},
		{name: "trailing JSON", contentType: "application/json", guard: RequestGuardValue, body: `{"credential":"token"}{}`, wantStatus: http.StatusBadRequest},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			service := &stubAuthService{}
			handler, err := NewAuthHandler(service, validAuthConfig())
			if err != nil {
				t.Fatalf("NewAuthHandler() error = %v", err)
			}
			request := httptest.NewRequest(http.MethodPost, "https://game.example/api/v1/auth/google", strings.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)
			if test.guard != "" {
				request.Header.Set(RequestGuardHeader, test.guard)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d, body = %s", response.Code, test.wantStatus, response.Body.String())
			}
			if service.loginCalls != 0 {
				t.Fatalf("Login() calls = %d, want 0", service.loginCalls)
			}
		})
	}
}

func TestGoogleLoginMapsCredentialAndInternalFailuresWithoutDetails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		serviceErr error
		wantStatus int
	}{
		{name: "invalid credential", serviceErr: auth.ErrInvalidCredential, wantStatus: http.StatusUnauthorized},
		{name: "store failure", serviceErr: errors.New("database secret detail"), wantStatus: http.StatusInternalServerError},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			service := &stubAuthService{loginErr: test.serviceErr}
			handler, err := NewAuthHandler(service, validAuthConfig())
			if err != nil {
				t.Fatalf("NewAuthHandler() error = %v", err)
			}
			request := httptest.NewRequest(http.MethodPost, "https://game.example/api/v1/auth/google", strings.NewReader(`{"credential":"token"}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set(RequestGuardHeader, RequestGuardValue)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
			if strings.Contains(response.Body.String(), "database secret detail") {
				t.Fatalf("response leaks internal error: %s", response.Body.String())
			}
		})
	}
}

func TestSessionStatusAuthenticatesOnlyWithSessionCookie(t *testing.T) {
	t.Parallel()

	service := &stubAuthService{authenticateUser: auth.User{ID: "usr_EREREREREREREREREREREQ"}}
	handler, err := NewAuthHandler(service, validAuthConfig())
	if err != nil {
		t.Fatalf("NewAuthHandler() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "https://game.example/api/v1/auth/session", nil)
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "raw-session-secret"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || service.authenticateToken != "raw-session-secret" {
		t.Fatalf("status = %d, token = %q, body = %s", response.Code, service.authenticateToken, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("response JSON error = %v", err)
	}
	if body["authenticated"] != true || body["user_id"] != "usr_EREREREREREREREREREREQ" {
		t.Fatalf("response body = %#v", body)
	}

	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "https://game.example/api/v1/auth/session", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("missing cookie status = %d", unauthenticated.Code)
	}
}

func TestLogoutIsIdempotentAndClearsHardenedCookie(t *testing.T) {
	t.Parallel()

	service := &stubAuthService{}
	handler, err := NewAuthHandler(service, validAuthConfig())
	if err != nil {
		t.Fatalf("NewAuthHandler() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodDelete, "https://game.example/api/v1/auth/session", nil)
	request.Header.Set(RequestGuardHeader, RequestGuardValue)
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "raw-session-secret"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent || service.logoutToken != "raw-session-secret" {
		t.Fatalf("status = %d, logout token = %q", response.Code, service.logoutToken)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != SessionCookieName || cookies[0].Value != "" || cookies[0].MaxAge >= 0 {
		t.Fatalf("cleared cookies = %+v", cookies)
	}
	if !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Path != "/" {
		t.Fatalf("cleared cookie attributes = %+v", cookies[0])
	}

	withoutCookie := httptest.NewRequest(http.MethodDelete, "https://game.example/api/v1/auth/session", nil)
	withoutCookie.Header.Set(RequestGuardHeader, RequestGuardValue)
	withoutCookieResponse := httptest.NewRecorder()
	handler.ServeHTTP(withoutCookieResponse, withoutCookie)
	if withoutCookieResponse.Code != http.StatusNoContent {
		t.Fatalf("logout without cookie status = %d", withoutCookieResponse.Code)
	}
}

func TestAuthConfigPublishesOnlyGoogleClientID(t *testing.T) {
	t.Parallel()

	handler, err := NewAuthHandler(&stubAuthService{}, validAuthConfig())
	if err != nil {
		t.Fatalf("NewAuthHandler() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "https://game.example/api/v1/auth/config", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if got, want := strings.TrimSpace(response.Body.String()), `{"google_client_id":"web-client-id.apps.googleusercontent.com"}`; got != want {
		t.Fatalf("config body = %s, want %s", got, want)
	}
}

func TestNewAuthHandlerRejectsInvalidDependencies(t *testing.T) {
	t.Parallel()

	if _, err := NewAuthHandler(nil, validAuthConfig()); !errors.Is(err, auth.ErrInvalidConfiguration) {
		t.Fatalf("NewAuthHandler(nil) error = %v", err)
	}
	if _, err := NewAuthHandler(&stubAuthService{}, Config{}); !errors.Is(err, auth.ErrInvalidConfiguration) {
		t.Fatalf("NewAuthHandler(empty config) error = %v", err)
	}
}

func validAuthConfig() Config {
	return Config{GoogleClientID: "web-client-id.apps.googleusercontent.com"}
}

type stubAuthService struct {
	loginCalls        int
	loginCredential   string
	loginResult       auth.LoginResult
	loginErr          error
	authenticateToken string
	authenticateUser  auth.User
	authenticateErr   error
	logoutToken       string
	logoutErr         error
}

func (s *stubAuthService) Login(_ context.Context, credential string) (auth.LoginResult, error) {
	s.loginCalls++
	s.loginCredential = credential
	return s.loginResult, s.loginErr
}

func (s *stubAuthService) Authenticate(_ context.Context, rawToken string) (auth.User, error) {
	s.authenticateToken = rawToken
	return s.authenticateUser, s.authenticateErr
}

func (s *stubAuthService) Logout(_ context.Context, rawToken string) error {
	s.logoutToken = rawToken
	return s.logoutErr
}
