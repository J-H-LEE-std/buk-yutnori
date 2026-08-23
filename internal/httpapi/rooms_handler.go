// Room lifecycle JSON HTTP boundary over the authoritative room registry.

package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	"net/http"

	"buk-yutnori/internal/application"
	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/room"
)

const maxRoomsBodyBytes = 16 * 1024

type roomAuthenticator interface {
	Authenticate(ctx context.Context, rawToken string) (auth.User, error)
}

type roomsService interface {
	List() []application.RoomSummary
	Create(input application.CreateRoomInput) (application.RoomSummary, error)
	Join(input application.JoinRoomInput) (application.RoomSummary, error)
	Detail(user auth.UserID, roomID domain.RoomID) (application.RoomDetailSnapshot, error)
}

type roomsHandler struct {
	authenticate roomAuthenticator
	rooms        roomsService
}

type createRoomRequest struct {
	Title    string         `json:"title"`
	Password string         `json:"password"`
	Team     domain.TeamID  `json:"team"`
	Settings *room.Settings `json:"settings"`
}

type joinRoomRequest struct {
	Role     string        `json:"role"`
	Team     domain.TeamID `json:"team"`
	Password string        `json:"password"`
}

type joinedRoomResponse struct {
	application.RoomSummary
	Role string        `json:"role"`
	Team domain.TeamID `json:"team,omitempty"`
}

type roomListResponse struct {
	Rooms []application.RoomSummary `json:"rooms"`
}

// NewRoomsHandler constructs the versioned lobby lifecycle routes. Every route
// requires a valid session cookie; mutations additionally require the same-origin
// request guard.
func NewRoomsHandler(authenticate roomAuthenticator, rooms roomsService) (http.Handler, error) {
	if authenticate == nil || rooms == nil {
		return nil, auth.ErrInvalidConfiguration
	}
	handler := &roomsHandler{authenticate: authenticate, rooms: rooms}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/rooms", handler.list)
	mux.HandleFunc("POST /api/v1/rooms", handler.create)
	mux.HandleFunc("POST /api/v1/rooms/{room_id}/join", handler.join)
	mux.HandleFunc("GET /api/v1/rooms/{room_id}", handler.detail)
	return mux, nil
}

func (h *roomsHandler) list(response http.ResponseWriter, request *http.Request) {
	setPrivateJSONHeaders(response)
	if _, ok := h.requireUser(response, request); !ok {
		return
	}
	writeJSON(response, http.StatusOK, roomListResponse{Rooms: h.rooms.List()})
}

func (h *roomsHandler) create(response http.ResponseWriter, request *http.Request) {
	setPrivateJSONHeaders(response)
	user, ok := h.requireUser(response, request)
	if !ok {
		return
	}
	if !hasRequestGuard(request) {
		writeError(response, http.StatusForbidden, "request_not_allowed")
		return
	}

	var input createRoomRequest
	if !decodeStrictJSON(response, request, &input) {
		return
	}
	settings := room.DefaultSettings()
	if input.Settings != nil {
		settings = *input.Settings
	}
	team := domain.TeamA
	if input.Team != "" {
		team = input.Team
	}

	summary, err := h.rooms.Create(application.CreateRoomInput{
		Creator:  user.ID,
		Creation: room.Creation{Title: input.Title, Password: input.Password},
		Settings: settings,
		Team:     team,
	})
	if err != nil {
		writeRegistryError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, summary)
}

func (h *roomsHandler) join(response http.ResponseWriter, request *http.Request) {
	setPrivateJSONHeaders(response)
	user, ok := h.requireUser(response, request)
	if !ok {
		return
	}
	if !hasRequestGuard(request) {
		writeError(response, http.StatusForbidden, "request_not_allowed")
		return
	}

	var input joinRoomRequest
	if !decodeStrictJSON(response, request, &input) {
		return
	}

	summary, err := h.rooms.Join(application.JoinRoomInput{
		User:     user.ID,
		RoomID:   domain.RoomID(request.PathValue("room_id")),
		Role:     input.Role,
		Team:     input.Team,
		Password: input.Password,
	})
	if err != nil {
		writeRegistryError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, joinedRoomResponse{
		RoomSummary: summary,
		Role:        input.Role,
		Team:        input.Team,
	})
}

func (h *roomsHandler) detail(response http.ResponseWriter, request *http.Request) {
	setPrivateJSONHeaders(response)
	user, ok := h.requireUser(response, request)
	if !ok {
		return
	}
	detail, err := h.rooms.Detail(user.ID, domain.RoomID(request.PathValue("room_id")))
	if err != nil {
		writeRegistryError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, detail)
}

func (h *roomsHandler) requireUser(response http.ResponseWriter, request *http.Request) (auth.User, bool) {
	cookie, err := request.Cookie(SessionCookieName)
	if err != nil {
		writeError(response, http.StatusUnauthorized, "unauthenticated")
		return auth.User{}, false
	}
	user, err := h.authenticate.Authenticate(request.Context(), cookie.Value)
	if err != nil {
		if errors.Is(err, auth.ErrUnauthenticated) {
			clearSessionCookie(response)
			writeError(response, http.StatusUnauthorized, "unauthenticated")
			return auth.User{}, false
		}
		writeError(response, http.StatusInternalServerError, "internal_error")
		return auth.User{}, false
	}
	return user, true
}

func decodeStrictJSON(response http.ResponseWriter, request *http.Request, target any) bool {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(response, http.StatusUnsupportedMediaType, "json_required")
		return false
	}
	request.Body = http.MaxBytesReader(response, request.Body, maxRoomsBodyBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request")
		return false
	}
	if err := requireJSONEnd(decoder); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request")
		return false
	}
	return true
}

func writeRegistryError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, application.ErrRoomNotFound):
		writeError(response, http.StatusNotFound, "room_not_found")
	case errors.Is(err, application.ErrAlreadyMember):
		writeError(response, http.StatusConflict, "already_member")
	case errors.Is(err, application.ErrNotMember):
		writeError(response, http.StatusForbidden, "not_member")
	case errors.Is(err, application.ErrCombinedCapacityFull), errors.Is(err, room.ErrLobbyFull):
		writeError(response, http.StatusConflict, "room_full")
	case errors.Is(err, application.ErrPasswordRequired):
		writeError(response, http.StatusForbidden, "password_required")
	case errors.Is(err, application.ErrInvalidRoomPassword):
		writeError(response, http.StatusForbidden, "invalid_password")
	default:
		writeError(response, http.StatusBadRequest, "invalid_request")
	}
}
