// Package httpapi exposes JSON HTTP boundaries without owning domain state.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"time"

	"buk-yutnori/internal/auth"
)

const (
	// SessionCookieName uses the __Host- prefix so browsers require Secure,
	// Path=/, and no Domain attribute.
	SessionCookieName  = "__Host-buk_session"
	RequestGuardHeader = "X-Buk-Request"
	RequestGuardValue  = "1"
	maxLoginBodyBytes  = 16 * 1024
)

type authService interface {
	Login(ctx context.Context, credential string) (auth.LoginResult, error)
	Authenticate(ctx context.Context, rawToken string) (auth.User, error)
	Logout(ctx context.Context, rawToken string) error
}

type authHandler struct {
	service authService
	config  Config
}

// Config contains public browser authentication configuration. A Google web
// client ID identifies the audience and is not a client secret.
type Config struct {
	GoogleClientID string
}

type googleLoginRequest struct {
	Credential string `json:"credential"`
}

type loginResponse struct {
	UserID    auth.UserID `json:"user_id"`
	ExpiresAt string      `json:"expires_at"`
}

type sessionResponse struct {
	Authenticated bool        `json:"authenticated"`
	UserID        auth.UserID `json:"user_id"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type authConfigResponse struct {
	GoogleClientID string `json:"google_client_id"`
}

// NewAuthHandler constructs the versioned authentication routes.
func NewAuthHandler(service authService, config Config) (http.Handler, error) {
	if service == nil || config.GoogleClientID == "" {
		return nil, auth.ErrInvalidConfiguration
	}
	handler := &authHandler{service: service, config: config}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/auth/config", handler.authConfig)
	mux.HandleFunc("POST /api/v1/auth/google", handler.googleLogin)
	mux.HandleFunc("GET /api/v1/auth/session", handler.sessionStatus)
	mux.HandleFunc("DELETE /api/v1/auth/session", handler.logout)
	return mux, nil
}

func (h *authHandler) authConfig(response http.ResponseWriter, _ *http.Request) {
	setPrivateJSONHeaders(response)
	writeJSON(response, http.StatusOK, authConfigResponse{GoogleClientID: h.config.GoogleClientID})
}

func (h *authHandler) googleLogin(response http.ResponseWriter, request *http.Request) {
	setPrivateJSONHeaders(response)
	if !hasRequestGuard(request) {
		writeError(response, http.StatusForbidden, "request_not_allowed")
		return
	}
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(response, http.StatusUnsupportedMediaType, "json_required")
		return
	}

	request.Body = http.MaxBytesReader(response, request.Body, maxLoginBodyBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var input googleLoginRequest
	if err := decoder.Decode(&input); err != nil || input.Credential == "" {
		writeError(response, http.StatusBadRequest, "invalid_request")
		return
	}
	if err := requireJSONEnd(decoder); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request")
		return
	}

	result, err := h.service.Login(request.Context(), input.Credential)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredential) {
			writeError(response, http.StatusUnauthorized, "invalid_credential")
			return
		}
		writeError(response, http.StatusInternalServerError, "internal_error")
		return
	}
	setSessionCookie(response, result.Token, result.ExpiresAt)
	writeJSON(response, http.StatusOK, loginResponse{
		UserID:    result.User.ID,
		ExpiresAt: result.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

func (h *authHandler) sessionStatus(response http.ResponseWriter, request *http.Request) {
	setPrivateJSONHeaders(response)
	cookie, err := request.Cookie(SessionCookieName)
	if err != nil {
		writeError(response, http.StatusUnauthorized, "unauthenticated")
		return
	}
	user, err := h.service.Authenticate(request.Context(), cookie.Value)
	if err != nil {
		if errors.Is(err, auth.ErrUnauthenticated) {
			clearSessionCookie(response)
			writeError(response, http.StatusUnauthorized, "unauthenticated")
			return
		}
		writeError(response, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(response, http.StatusOK, sessionResponse{Authenticated: true, UserID: user.ID})
}

func (h *authHandler) logout(response http.ResponseWriter, request *http.Request) {
	setPrivateJSONHeaders(response)
	if !hasRequestGuard(request) {
		writeError(response, http.StatusForbidden, "request_not_allowed")
		return
	}
	cookie, err := request.Cookie(SessionCookieName)
	if err == nil {
		if err := h.service.Logout(request.Context(), cookie.Value); err != nil && !errors.Is(err, auth.ErrUnauthenticated) {
			writeError(response, http.StatusInternalServerError, "internal_error")
			return
		}
	}
	clearSessionCookie(response)
	response.WriteHeader(http.StatusNoContent)
}

func hasRequestGuard(request *http.Request) bool {
	return request.Header.Get(RequestGuardHeader) == RequestGuardValue
}

func requireJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func setSessionCookie(response http.ResponseWriter, token string, expiresAt time.Time) {
	http.SetCookie(response, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt.UTC(),
		MaxAge:   int(auth.SessionLifetime / time.Second),
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

func clearSessionCookie(response http.ResponseWriter) {
	http.SetCookie(response, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

func setPrivateJSONHeaders(response http.ResponseWriter) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
}

func writeError(response http.ResponseWriter, status int, code string) {
	writeJSON(response, status, errorResponse{Error: code})
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.WriteHeader(status)
	if err := json.NewEncoder(response).Encode(value); err != nil {
		panic(fmt.Errorf("encode HTTP JSON response: %w", err))
	}
}
