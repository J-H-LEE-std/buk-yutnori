package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"buk-yutnori/internal/application"
	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/room"
)

type stubRoomAuthenticator struct {
	user auth.User
	err  error
}

func (s *stubRoomAuthenticator) Authenticate(_ context.Context, _ string) (auth.User, error) {
	return s.user, s.err
}

type stubRoomsService struct {
	listResult   []application.RoomSummary
	detail       application.RoomDetailSnapshot
	detailErr    error
	createInput  application.CreateRoomInput
	createResult application.RoomSummary
	createErr    error
	joinInput    application.JoinRoomInput
	joinResult   application.RoomSummary
	joinErr      error
}

func (s *stubRoomsService) List() []application.RoomSummary {
	return s.listResult
}

func (s *stubRoomsService) Create(input application.CreateRoomInput) (application.RoomSummary, error) {
	s.createInput = input
	return s.createResult, s.createErr
}

func (s *stubRoomsService) Detail(_ auth.UserID, _ domain.RoomID) (application.RoomDetailSnapshot, error) {
	return s.detail, s.detailErr
}

func (s *stubRoomsService) Join(input application.JoinRoomInput) (application.RoomSummary, error) {
	s.joinInput = input
	return s.joinResult, s.joinErr
}

func newRoomsTestHandler(authenticate roomAuthenticator, rooms roomsService) http.Handler {
	handler, err := NewRoomsHandler(authenticate, rooms)
	if err != nil {
		panic(err)
	}
	return handler
}

// roomsRequest builds an authenticated guarded request, serves it through the
// handler, and returns the recorder.
func roomsRequest(
	t *testing.T,
	handler http.Handler,
	method,
	target string,
	body any,
	options ...func(*http.Request),
) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	request := httptest.NewRequest(method, target, nil)
	if body != nil {
		if raw, ok := body.(string); ok {
			reader = bytes.NewBufferString(raw)
		} else {
			buffer := bytes.NewBuffer(nil)
			if err := json.NewEncoder(buffer).Encode(body); err != nil {
				t.Fatalf("Encode(body) error = %v", err)
			}
			reader = buffer
		}
		request = httptest.NewRequest(method, target, reader)
		request.Header.Set("Content-Type", "application/json")
	}
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "raw-session-secret"})
	request.Header.Set(RequestGuardHeader, RequestGuardValue)
	for _, option := range options {
		option(request)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestRoomsRoutesRequireSession(t *testing.T) {
	service := &stubRoomsService{}
	handler := newRoomsTestHandler(&stubRoomAuthenticator{err: auth.ErrUnauthenticated}, service)

	tests := []struct {
		name   string
		method string
		target string
	}{
		{name: "list", method: http.MethodGet, target: "/api/v1/rooms"},
		{
			name:   "create",
			method: http.MethodPost,
			target: "/api/v1/rooms",
			// body omitted: authentication precedes decoding
		},
		{name: "join", method: http.MethodPost, target: "/api/v1/rooms/some-room/join"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(tt.method, tt.target, nil)
			request.Header.Set(RequestGuardHeader, RequestGuardValue)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", response.Code)
			}
		})
	}
}

func TestCreateRoomRequiresRequestGuard(t *testing.T) {
	service := &stubRoomsService{}
	handler := newRoomsTestHandler(
		&stubRoomAuthenticator{user: auth.User{ID: "user-1"}},
		service,
	)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/rooms", bytes.NewBufferString(`{"title":"방"}`))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "raw-session-secret"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden || service.createResult.Title != "" {
		t.Fatalf("status = %d, create called = %v", response.Code, service.createResult.Title != "")
	}
}
func TestCreateRoomAppliesDefaultsAndPassesCreation(t *testing.T) {
	authenticator := &stubRoomAuthenticator{user: auth.User{ID: "user-1"}}
	service := &stubRoomsService{
		createResult: application.RoomSummary{
			RoomID:      domain.RoomID("room-id"),
			Title:       "방",
			PlayerCount: 1,
			MaxPlayers:  8,
		},
	}
	handler := newRoomsTestHandler(authenticator, service)

	response := roomsRequest(t, handler, http.MethodPost, "/api/v1/rooms", map[string]any{
		"title":    "방",
		"password": "pass1234",
		"team":     "B",
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}

	input := service.createInput
	if input.Creator != "user-1" {
		t.Fatalf("Creator = %q, want authenticated user", input.Creator)
	}
	if input.Creation.Title != "방" || input.Creation.Password != "pass1234" {
		t.Fatalf("Creation = %+v, want canonical fields", input.Creation)
	}
	if input.Team != domain.TeamB {
		t.Fatalf("Team = %q, want requested B", input.Team)
	}
	if input.Settings != room.DefaultSettings() {
		t.Fatalf("Settings = %+v, want defaults when omitted", input.Settings)
	}
}

func TestCreateRoomRejectsUnknownFieldsAndInvalidJSON(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "unknown field", body: `{"title":"방","hacker":true}`},
		{name: "broken json", body: `{"title":`},
		{name: "multiple values", body: `{"title":"방"}{"title":"또"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := newRoomsTestHandler(
				&stubRoomAuthenticator{user: auth.User{ID: "user-1"}},
				&stubRoomsService{},
			)

			response := roomsRequest(t, handler, http.MethodPost, "/api/v1/rooms", tt.body)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", response.Code)
			}
		})
	}
}

func TestJoinRoomMapsRegistryErrors(t *testing.T) {
	tests := []struct {
		name       string
		joinErr    error
		wantStatus int
		wantCode   string
	}{
		{
			name:       "not found",
			joinErr:    application.ErrRoomNotFound,
			wantStatus: http.StatusNotFound,
			wantCode:   "room_not_found",
		},
		{
			name:       "already member",
			joinErr:    application.ErrAlreadyMember,
			wantStatus: http.StatusConflict,
			wantCode:   "already_member",
		},
		{
			name:       "player capacity",
			joinErr:    room.ErrLobbyFull,
			wantStatus: http.StatusConflict,
			wantCode:   "room_full",
		},
		{
			name:       "combined capacity",
			joinErr:    application.ErrCombinedCapacityFull,
			wantStatus: http.StatusConflict,
			wantCode:   "room_full",
		},
		{
			name:       "password required",
			joinErr:    application.ErrPasswordRequired,
			wantStatus: http.StatusForbidden,
			wantCode:   "password_required",
		},
		{
			name:       "invalid password",
			joinErr:    application.ErrInvalidRoomPassword,
			wantStatus: http.StatusForbidden,
			wantCode:   "invalid_password",
		},
		{
			name:       "invalid creation falls back to bad request",
			joinErr:    room.ErrInvalidCreation,
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &stubRoomsService{joinErr: tt.joinErr}
			handler := newRoomsTestHandler(
				&stubRoomAuthenticator{user: auth.User{ID: "user-1"}},
				service,
			)

			response := roomsRequest(t, handler, http.MethodPost, "/api/v1/rooms/abc/join", map[string]any{
				"role": "player",
				"team": "A",
			})
			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, tt.wantStatus)
			}
			var got struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
				t.Fatalf("Unmarshal(error body) error = %v", err)
			}
			if got.Error != tt.wantCode {
				t.Fatalf("error code = %q, want %q", got.Error, tt.wantCode)
			}
		})
	}
}

func TestJoinRoomPassesRoleTeamAndPassword(t *testing.T) {
	authenticator := &stubRoomAuthenticator{user: auth.User{ID: "user-9"}}
	summary := application.RoomSummary{RoomID: domain.RoomID("abc"), Title: "방", PlayerCount: 2, MaxPlayers: 8}
	service := &stubRoomsService{joinResult: summary}
	handler := newRoomsTestHandler(authenticator, service)

	response := roomsRequest(t, handler, http.MethodPost, "/api/v1/rooms/abc/join", map[string]any{
		"role":     "player",
		"team":     "A",
		"password": "secret01",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}

	input := service.joinInput
	if input.RoomID != domain.RoomID("abc") || input.Role != "player" ||
		input.Team != domain.TeamA || input.Password != "secret01" || input.User != "user-9" {
		t.Fatalf("Join input = %+v, want path and body passthrough", input)
	}

	var joined joinedRoomResponse
	if err := json.Unmarshal(response.Body.Bytes(), &joined); err != nil {
		t.Fatalf("Unmarshal(joined) error = %v", err)
	}
	if joined.Role != "player" || joined.Team != domain.TeamA || joined.Title != "방" {
		t.Fatalf("joined response = %+v, want summary with role and team", joined)
	}
}

func TestListReturnsRoomsJSON(t *testing.T) {
	service := &stubRoomsService{
		listResult: []application.RoomSummary{
			{RoomID: domain.RoomID("r1"), Title: "공개방", HasPassword: true, PlayerCount: 3, MaxPlayers: 4},
		},
	}
	handler := newRoomsTestHandler(&stubRoomAuthenticator{user: auth.User{ID: "user-1"}}, service)

	response := roomsRequest(t, handler, http.MethodGet, "/api/v1/rooms", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}

	var list roomListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &list); err != nil {
		t.Fatalf("Unmarshal(list) error = %v", err)
	}
	if len(list.Rooms) != 1 || !list.Rooms[0].HasPassword || list.Rooms[0].PlayerCount != 3 {
		t.Fatalf("rooms = %+v, want single protected summary", list.Rooms)
	}
}

func TestNewRoomsHandlerRejectsMissingDependencies(t *testing.T) {
	stub := &stubRoomsService{}
	stubAuth := &stubRoomAuthenticator{}

	if _, err := NewRoomsHandler(nil, stub); !errors.Is(err, auth.ErrInvalidConfiguration) {
		t.Fatalf("NewRoomsHandler(nil auth) error = %v", err)
	}
	if _, err := NewRoomsHandler(stubAuth, nil); !errors.Is(err, auth.ErrInvalidConfiguration) {
		t.Fatalf("NewRoomsHandler(nil rooms) error = %v", err)
	}
}

func TestRoomDetailMapsMembershipVisibility(t *testing.T) {
	authenticator := &stubRoomAuthenticator{user: auth.User{ID: "user-1"}}
	validDetail := application.RoomDetailSnapshot{
		Summary: application.RoomSummary{RoomID: domain.RoomID("r1"), Title: "방", PlayerCount: 1, MaxPlayers: 8},
	}

	service := &stubRoomsService{detailErr: application.ErrNotMember}
	handler := newRoomsTestHandler(authenticator, service)
	response := roomsRequest(t, handler, http.MethodGet, "/api/v1/rooms/r1", nil)
	if response.Code != http.StatusForbidden {
		t.Fatalf("non-member status = %d, want 403", response.Code)
	}

	service = &stubRoomsService{detailErr: application.ErrRoomNotFound}
	handler = newRoomsTestHandler(authenticator, service)
	response = roomsRequest(t, handler, http.MethodGet, "/api/v1/rooms/missing", nil)
	if response.Code != http.StatusNotFound {
		t.Fatalf("unknown room status = %d, want 404", response.Code)
	}

	service = &stubRoomsService{detail: validDetail}
	handler = newRoomsTestHandler(authenticator, service)
	response = roomsRequest(t, handler, http.MethodGet, "/api/v1/rooms/r1", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("member status = %d, want 200", response.Code)
	}
}
