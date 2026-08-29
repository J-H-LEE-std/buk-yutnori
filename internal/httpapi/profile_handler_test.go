package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/profile"
)

func TestProfileHandlerUpdatesAuthenticatedOwnersOnly(t *testing.T) {
	t.Parallel()

	userID := auth.UserID("usr_EREREREREREREREREREREQ")
	authService := &stubAuthService{authenticateUser: auth.User{ID: userID}}
	store := &stubProfileStore{}
	handler, err := NewProfileHandler(authService, store)
	if err != nil {
		t.Fatalf("NewProfileHandler() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodPut, "https://game.example/api/v1/profile/me", strings.NewReader(`{"nickname":"가나다","is_public":false}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(RequestGuardHeader, RequestGuardValue)
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "raw-session-secret"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", response.Code, response.Body.String())
	}
	if authService.authenticateToken != "raw-session-secret" {
		t.Fatalf("Authenticate token = %q", authService.authenticateToken)
	}
	if store.saved != (profile.Profile{UserID: userID, Nickname: "가나다", Public: false}) {
		t.Fatalf("saved profile = %+v", store.saved)
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode profile response: %v", err)
	}
	if body["user_id"] != string(userID) || body["nickname"] != "가나다" || body["is_public"] != false || body["wins"] != float64(0) || body["losses"] != float64(0) {
		t.Fatalf("profile response = %#v", body)
	}

	missingGuard := httptest.NewRequest(http.MethodPut, "https://game.example/api/v1/profile/me", strings.NewReader(`{"nickname":"가나다","is_public":true}`))
	missingGuard.Header.Set("Content-Type", "application/json")
	missingGuard.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "raw-session-secret"})
	missingGuardResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingGuardResponse, missingGuard)
	if missingGuardResponse.Code != http.StatusForbidden {
		t.Fatalf("missing guard status = %d", missingGuardResponse.Code)
	}

	unauthenticated := httptest.NewRequest(http.MethodPut, "https://game.example/api/v1/profile/me", strings.NewReader(`{"nickname":"가나다","is_public":true}`))
	unauthenticated.Header.Set("Content-Type", "application/json")
	unauthenticated.Header.Set(RequestGuardHeader, RequestGuardValue)
	unauthenticatedResponse := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticatedResponse, unauthenticated)
	if unauthenticatedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("missing cookie status = %d", unauthenticatedResponse.Code)
	}
}

func TestProfileHandlerValidatesInputAndMapsNicknameConflict(t *testing.T) {
	t.Parallel()

	userID := auth.UserID("usr_EREREREREREREREREREREQ")
	store := &stubProfileStore{saveErr: profile.ErrNicknameTaken}
	handler, err := NewProfileHandler(&stubAuthService{authenticateUser: auth.User{ID: userID}}, store)
	if err != nil {
		t.Fatalf("NewProfileHandler() error = %v", err)
	}
	for name, test := range map[string]struct {
		body      string
		wantCode  int
		wantError string
	}{
		"invalid nickname": {`{"nickname":"가","is_public":true}`, http.StatusBadRequest, "invalid_nickname"},
		"missing public":   {`{"nickname":"가나다"}`, http.StatusBadRequest, "invalid_request"},
		"nickname taken":   {`{"nickname":"가나다","is_public":true}`, http.StatusConflict, "nickname_taken"},
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPut, "https://game.example/api/v1/profile/me", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set(RequestGuardHeader, RequestGuardValue)
			request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "raw-session-secret"})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantCode || !strings.Contains(response.Body.String(), test.wantError) {
				t.Fatalf("status/body = %d/%s, want %d/%s", response.Code, response.Body.String(), test.wantCode, test.wantError)
			}
		})
	}
}

func TestProfileHandlerExposesPrivateStatisticsOnlyToOwners(t *testing.T) {
	t.Parallel()

	userID := auth.UserID("usr_EREREREREREREREREREREQ")
	store := &stubProfileStore{lookup: profile.Profile{UserID: userID, Nickname: "가나다", Public: false, Wins: 7, Losses: 3}}
	handler, err := NewProfileHandler(&stubAuthService{authenticateUser: auth.User{ID: userID}}, store)
	if err != nil {
		t.Fatalf("NewProfileHandler() error = %v", err)
	}

	publicRequest := httptest.NewRequest(http.MethodGet, "https://game.example/api/v1/profiles/"+string(userID), nil)
	publicResponse := httptest.NewRecorder()
	handler.ServeHTTP(publicResponse, publicRequest)
	if publicResponse.Code != http.StatusOK || strings.Contains(publicResponse.Body.String(), "wins") || strings.Contains(publicResponse.Body.String(), "losses") || strings.Contains(publicResponse.Body.String(), "is_public") {
		t.Fatalf("private public-view response = %d %s", publicResponse.Code, publicResponse.Body.String())
	}

	selfRequest := httptest.NewRequest(http.MethodGet, "https://game.example/api/v1/profile/me", nil)
	selfRequest.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "raw-session-secret"})
	selfResponse := httptest.NewRecorder()
	handler.ServeHTTP(selfResponse, selfRequest)
	if selfResponse.Code != http.StatusOK || !strings.Contains(selfResponse.Body.String(), `"wins":7`) || !strings.Contains(selfResponse.Body.String(), `"losses":3`) || !strings.Contains(selfResponse.Body.String(), `"is_public":false`) {
		t.Fatalf("self profile response = %d %s", selfResponse.Code, selfResponse.Body.String())
	}
}

func TestNewProfileHandlerRejectsMissingDependencies(t *testing.T) {
	t.Parallel()

	if _, err := NewProfileHandler(nil, &stubProfileStore{}); !errors.Is(err, auth.ErrInvalidConfiguration) {
		t.Fatalf("NewProfileHandler(nil auth) error = %v", err)
	}
	if _, err := NewProfileHandler(&stubAuthService{}, nil); !errors.Is(err, auth.ErrInvalidConfiguration) {
		t.Fatalf("NewProfileHandler(nil store) error = %v", err)
	}
}

type stubProfileStore struct {
	saved     profile.Profile
	saveErr   error
	lookup    profile.Profile
	lookupErr error
}

func (store *stubProfileStore) Save(_ context.Context, value profile.Profile) error {
	store.saved = value
	return store.saveErr
}

func (store *stubProfileStore) Lookup(_ context.Context, _ auth.UserID) (profile.Profile, error) {
	return store.lookup, store.lookupErr
}
