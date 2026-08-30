package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	"net/http"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/profile"
)

const maxProfileBodyBytes = 16 * 1024

type profileAuthService interface {
	Authenticate(ctx context.Context, rawToken string) (auth.User, error)
}

type profileHandler struct {
	auth  profileAuthService
	store profile.Store
}

type saveProfileRequest struct {
	Nickname string `json:"nickname"`
	Public   *bool  `json:"is_public"`
}

type privateProfileResponse struct {
	UserID   auth.UserID `json:"user_id"`
	Nickname string      `json:"nickname"`
	Public   bool        `json:"is_public"`
	Wins     uint64      `json:"wins"`
	Losses   uint64      `json:"losses"`
}

type publicProfileResponse struct {
	UserID   auth.UserID `json:"user_id"`
	Nickname string      `json:"nickname"`
}

// NewProfileHandler builds authenticated self-profile routes and the public
// profile read route. The public route deliberately returns only a nickname
// when its owner disabled public statistics.
func NewProfileHandler(authService profileAuthService, store profile.Store) (http.Handler, error) {
	if authService == nil || store == nil {
		return nil, auth.ErrInvalidConfiguration
	}
	handler := &profileHandler{auth: authService, store: store}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/profile/me", handler.getMe)
	mux.HandleFunc("PUT /api/v1/profile/me", handler.saveMe)
	mux.HandleFunc("GET /api/v1/profiles/{user_id}", handler.getPublic)
	return mux, nil
}

func (h *profileHandler) getMe(response http.ResponseWriter, request *http.Request) {
	setPrivateJSONHeaders(response)
	user, ok := h.authenticatedUser(response, request)
	if !ok {
		return
	}
	value, err := h.store.Lookup(request.Context(), user.ID)
	if h.writeLookupError(response, err) {
		return
	}
	writeJSON(response, http.StatusOK, privateResponse(value))
}

func (h *profileHandler) saveMe(response http.ResponseWriter, request *http.Request) {
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
	user, ok := h.authenticatedUser(response, request)
	if !ok {
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, maxProfileBodyBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var input saveProfileRequest
	if err := decoder.Decode(&input); err != nil || input.Public == nil || requireJSONEnd(decoder) != nil {
		writeError(response, http.StatusBadRequest, "invalid_request")
		return
	}
	nickname, err := profile.ParseNickname(input.Nickname)
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid_nickname")
		return
	}
	value := profile.Profile{UserID: user.ID, Nickname: nickname, Public: *input.Public}
	if err := h.store.Save(request.Context(), value); err != nil {
		switch {
		case errors.Is(err, profile.ErrNicknameTaken):
			writeError(response, http.StatusConflict, "nickname_taken")
		case errors.Is(err, profile.ErrInvalidNickname):
			writeError(response, http.StatusBadRequest, "invalid_nickname")
		case errors.Is(err, profile.ErrNotFound):
			writeError(response, http.StatusUnauthorized, "unauthenticated")
		default:
			writeError(response, http.StatusInternalServerError, "internal_error")
		}
		return
	}
	persisted, err := h.store.Lookup(request.Context(), user.ID)
	if err != nil {
		// The profile write already succeeded. Do not turn that success into an
		// error merely because the optional confirmation read failed.
		writeJSON(response, http.StatusOK, privateResponse(value))
		return
	}
	writeJSON(response, http.StatusOK, privateResponse(persisted))
}

func (h *profileHandler) getPublic(response http.ResponseWriter, request *http.Request) {
	setPrivateJSONHeaders(response)
	userID := auth.UserID(request.PathValue("user_id"))
	if err := userID.Validate(); err != nil {
		writeError(response, http.StatusNotFound, "profile_not_found")
		return
	}
	value, err := h.store.Lookup(request.Context(), userID)
	if h.writeLookupError(response, err) {
		return
	}
	if value.Public {
		writeJSON(response, http.StatusOK, privateResponse(value))
		return
	}
	writeJSON(response, http.StatusOK, publicProfileResponse{UserID: value.UserID, Nickname: string(value.Nickname)})
}

func (h *profileHandler) authenticatedUser(response http.ResponseWriter, request *http.Request) (auth.User, bool) {
	cookie, err := request.Cookie(SessionCookieName)
	if err != nil {
		writeError(response, http.StatusUnauthorized, "unauthenticated")
		return auth.User{}, false
	}
	user, err := h.auth.Authenticate(request.Context(), cookie.Value)
	if err != nil {
		if errors.Is(err, auth.ErrUnauthenticated) {
			clearSessionCookie(response)
			writeError(response, http.StatusUnauthorized, "unauthenticated")
			return auth.User{}, false
		}
		writeError(response, http.StatusInternalServerError, "internal_error")
		return auth.User{}, false
	}
	if err := user.ID.Validate(); err != nil {
		writeError(response, http.StatusUnauthorized, "unauthenticated")
		return auth.User{}, false
	}
	return user, true
}

func (h *profileHandler) writeLookupError(response http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, profile.ErrNotFound) {
		writeError(response, http.StatusNotFound, "profile_not_found")
	} else {
		writeError(response, http.StatusInternalServerError, "internal_error")
	}
	return true
}

func privateResponse(value profile.Profile) privateProfileResponse {
	return privateProfileResponse{
		UserID: value.UserID, Nickname: string(value.Nickname), Public: value.Public,
		Wins: value.Wins, Losses: value.Losses,
	}
}
