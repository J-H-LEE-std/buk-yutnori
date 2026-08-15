package wsapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/protocol"

	"github.com/coder/websocket"
)

const (
	testCookieName = "__Host-buk_session"
	testRawToken   = "raw-session-token"
)

func TestHandlerAuthenticatesBeforeUpgradeAndPassesOnlyUser(t *testing.T) {
	authenticator := &recordingAuthenticator{user: auth.User{ID: testUserID}}
	receivedUser := make(chan auth.User, 1)
	handler := mustHandler(t, authenticator, SessionFunc(func(_ context.Context, user auth.User, connection *Connection) error {
		receivedUser <- user
		return connection.CloseNormal("test_complete")
	}), DefaultConfig(testCookieName))
	server := httptest.NewServer(handler)
	defer server.Close()

	connection, response, err := dial(t, server.URL, server.URL, testRawToken)
	if err != nil {
		t.Fatalf("Dial() error = %v, response = %v", err, response)
	}
	defer connection.CloseNow()

	select {
	case user := <-receivedUser:
		if user.ID != testUserID {
			t.Fatalf("session user = %q", user.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("session handler was not called")
	}
	if got := authenticator.lastToken(); got != testRawToken {
		t.Fatalf("Authenticate() token = %q", got)
	}
}

func TestHandlerRejectsHandshakeBeforeSession(t *testing.T) {
	tests := []struct {
		name          string
		origin        string
		token         string
		authErr       error
		wantStatus    int
		wantAuthCalls int
	}{
		{name: "missing origin", token: testRawToken, wantStatus: http.StatusForbidden},
		{name: "cross origin", origin: "https://evil.example", token: testRawToken, wantStatus: http.StatusForbidden},
		{name: "missing cookie", origin: "same", wantStatus: http.StatusUnauthorized},
		{name: "expired session", origin: "same", token: testRawToken, authErr: auth.ErrUnauthenticated, wantStatus: http.StatusUnauthorized, wantAuthCalls: 1},
		{name: "store failure", origin: "same", token: testRawToken, authErr: errors.New("database secret detail"), wantStatus: http.StatusInternalServerError, wantAuthCalls: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			authenticator := &recordingAuthenticator{user: auth.User{ID: testUserID}, err: test.authErr}
			called := make(chan struct{}, 1)
			handler := mustHandler(t, authenticator, SessionFunc(func(context.Context, auth.User, *Connection) error {
				called <- struct{}{}
				return nil
			}), DefaultConfig(testCookieName))
			server := httptest.NewServer(handler)
			defer server.Close()
			origin := test.origin
			if origin == "same" {
				origin = server.URL
			}

			connection, response, err := dial(t, server.URL, origin, test.token)
			if connection != nil {
				connection.CloseNow()
			}
			if err == nil {
				t.Fatal("Dial() error = nil")
			}
			if response == nil || response.StatusCode != test.wantStatus {
				t.Fatalf("response = %v, want status %d", response, test.wantStatus)
			}
			if got := authenticator.callCount(); got != test.wantAuthCalls {
				t.Fatalf("Authenticate() calls = %d, want %d", got, test.wantAuthCalls)
			}
			select {
			case <-called:
				t.Fatal("session handler called for rejected handshake")
			default:
			}
		})
	}
}

func TestConnectionReadsStrictTextClientCommandAndWritesJSON(t *testing.T) {
	commandResult := make(chan protocol.ClientCommand, 1)
	handler := mustHandler(t, &recordingAuthenticator{user: auth.User{ID: testUserID}}, SessionFunc(func(ctx context.Context, _ auth.User, connection *Connection) error {
		command, err := connection.ReadCommand(ctx)
		if err != nil {
			return err
		}
		commandResult <- command
		return connection.WriteJSON(ctx, map[string]bool{"received": true})
	}), DefaultConfig(testCookieName))
	server := httptest.NewServer(handler)
	defer server.Close()
	connection, _, err := dial(t, server.URL, server.URL, testRawToken)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer connection.CloseNow()

	message := `{"version":1,"direction":"client_command","type":"SEND_CHAT","command_id":"cmd-1","room_id":"room-1","payload":{"text":"한글 채팅"}}`
	if err := connection.Write(context.Background(), websocket.MessageText, []byte(message)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	messageType, response, err := connection.Read(context.Background())
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if messageType != websocket.MessageText || string(response) != `{"received":true}` {
		t.Fatalf("response = %v %s", messageType, response)
	}
	command := <-commandResult
	if command.Type != protocol.CommandSendChat || command.CommandID != "cmd-1" {
		t.Fatalf("command = %+v", command)
	}
}

func TestConnectionClosesOnInvalidData(t *testing.T) {
	tests := []struct {
		name        string
		config      Config
		messageType websocket.MessageType
		message     []byte
		wantStatus  websocket.StatusCode
	}{
		{name: "binary", config: DefaultConfig(testCookieName), messageType: websocket.MessageBinary, message: []byte("not-json"), wantStatus: websocket.StatusUnsupportedData},
		{name: "malformed command", config: DefaultConfig(testCookieName), messageType: websocket.MessageText, message: []byte(`{"version":1}`), wantStatus: websocket.StatusPolicyViolation},
		{name: "oversized", config: Config{SessionCookieName: testCookieName, MaxMessageBytes: 64}, messageType: websocket.MessageText, message: []byte(strings.Repeat("x", 65)), wantStatus: websocket.StatusMessageTooBig},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			readResult := make(chan error, 1)
			handler := mustHandler(t, &recordingAuthenticator{user: auth.User{ID: testUserID}}, SessionFunc(func(ctx context.Context, _ auth.User, connection *Connection) error {
				_, err := connection.ReadCommand(ctx)
				readResult <- err
				return err
			}), test.config)
			server := httptest.NewServer(handler)
			defer server.Close()
			connection, _, err := dial(t, server.URL, server.URL, testRawToken)
			if err != nil {
				t.Fatalf("Dial() error = %v", err)
			}
			defer connection.CloseNow()

			if err := connection.Write(context.Background(), test.messageType, test.message); err != nil {
				t.Fatalf("Write() error = %v", err)
			}
			_, _, err = connection.Read(context.Background())
			if got := websocket.CloseStatus(err); got != test.wantStatus {
				t.Fatalf("close status = %v, want %v, error = %v", got, test.wantStatus, err)
			}
			select {
			case readErr := <-readResult:
				if readErr == nil {
					t.Fatal("ReadCommand() error = nil")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("ReadCommand() did not return")
			}
		})
	}
}

func TestPendingSessionRejectsValidApplicationCommandWithoutApplyingIt(t *testing.T) {
	handler := mustHandler(t, &recordingAuthenticator{user: auth.User{ID: testUserID}}, PendingSession{}, DefaultConfig(testCookieName))
	server := httptest.NewServer(handler)
	defer server.Close()
	connection, _, err := dial(t, server.URL, server.URL, testRawToken)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer connection.CloseNow()

	message := `{"version":1,"direction":"client_command","type":"SEND_CHAT","command_id":"cmd-1","room_id":"room-1","payload":{"text":"not applied"}}`
	if err := connection.Write(context.Background(), websocket.MessageText, []byte(message)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	_, _, err = connection.Read(context.Background())
	if got := websocket.CloseStatus(err); got != websocket.StatusTryAgainLater {
		t.Fatalf("close status = %v, want %v, error = %v", got, websocket.StatusTryAgainLater, err)
	}
}

func TestNewHandlerRejectsInvalidDependencies(t *testing.T) {
	validAuth := &recordingAuthenticator{user: auth.User{ID: testUserID}}
	validSession := SessionFunc(func(context.Context, auth.User, *Connection) error { return nil })
	validConfig := DefaultConfig(testCookieName)

	if _, err := NewHandler(nil, validSession, validConfig); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("NewHandler(nil auth) error = %v", err)
	}
	if _, err := NewHandler(validAuth, nil, validConfig); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("NewHandler(nil session) error = %v", err)
	}
	if _, err := NewHandler(validAuth, validSession, Config{}); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("NewHandler(empty config) error = %v", err)
	}
}

func mustHandler(t *testing.T, authenticator Authenticator, session Session, config Config) http.Handler {
	t.Helper()
	handler, err := NewHandler(authenticator, session, config)
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	return handler
}

func dial(t *testing.T, serverURL, origin, token string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	header := make(http.Header)
	if origin != "" {
		header.Set("Origin", origin)
	}
	if token != "" {
		header.Set("Cookie", testCookieName+"="+token)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	return websocket.Dial(ctx, "ws"+strings.TrimPrefix(serverURL, "http")+"/api/v1/ws", &websocket.DialOptions{HTTPHeader: header})
}

type recordingAuthenticator struct {
	mu    sync.Mutex
	user  auth.User
	err   error
	token string
	calls int
}

func (a *recordingAuthenticator) Authenticate(_ context.Context, token string) (auth.User, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	a.token = token
	return a.user, a.err
}

func (a *recordingAuthenticator) lastToken() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.token
}

func (a *recordingAuthenticator) callCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls
}

const testUserID auth.UserID = "usr_EREREREREREREREREREREQ"
