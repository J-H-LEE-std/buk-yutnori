package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"buk-yutnori/internal/application"
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

func TestCommandSessionWritesAndReplaysCommandResultAcrossConnections(t *testing.T) {
	start, end := uint64(23), uint64(25)
	executor := &sessionExecutor{outcomes: []protocol.CommandOutcome{{
		Status: protocol.CommandAccepted, EventSequenceStart: &start, EventSequenceEnd: &end,
	}}}
	processor, err := application.NewProcessor(executor)
	if err != nil {
		t.Fatalf("application.NewProcessor() error = %v", err)
	}
	session, err := NewCommandSession(processor)
	if err != nil {
		t.Fatalf("NewCommandSession() error = %v", err)
	}
	handler := mustHandler(t, &recordingAuthenticator{user: auth.User{ID: testUserID}}, session, DefaultConfig(testCookieName))
	server := httptest.NewServer(handler)
	defer server.Close()

	first, _, err := dial(t, server.URL, server.URL, testRawToken)
	if err != nil {
		t.Fatalf("first Dial() error = %v", err)
	}
	defer first.CloseNow()
	second, _, err := dial(t, server.URL, server.URL, testRawToken)
	if err != nil {
		t.Fatalf("second Dial() error = %v", err)
	}
	defer second.CloseNow()

	message := []byte(`{"version":1,"direction":"client_command","type":"THROW_YUT","request_id":"req-1","command_id":"cmd-1","room_id":"room-1","match_id":"match-1","payload":{}}`)
	firstResponse := commandRoundTrip(t, first, message)
	reorderedDuplicate := []byte(`{"payload":{},"match_id":"match-1","room_id":"room-1","command_id":"cmd-1","request_id":"req-1","type":"THROW_YUT","direction":"client_command","version":1}`)
	secondResponse := commandRoundTrip(t, second, reorderedDuplicate)
	if string(firstResponse) != string(secondResponse) {
		t.Fatalf("duplicate response changed:\nfirst  = %s\nsecond = %s", firstResponse, secondResponse)
	}
	var result protocol.CommandResult
	if err := json.Unmarshal(secondResponse, &result); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if result.CommandID != "cmd-1" || result.Payload.Status != protocol.CommandAccepted || result.Payload.EventSequenceStart == nil || *result.Payload.EventSequenceStart != 23 || result.Payload.EventSequenceEnd == nil || *result.Payload.EventSequenceEnd != 25 {
		t.Fatalf("COMMAND_RESULT = %+v", result)
	}
	if executor.callCount() != 1 {
		t.Fatalf("Execute() calls = %d, want 1", executor.callCount())
	}
}

func TestCommandSessionSharesConcurrentDuplicateAcrossConnections(t *testing.T) {
	started := make(chan struct{})
	releaseExecutor := make(chan struct{})
	executor := &blockingSessionExecutor{started: started, release: releaseExecutor}
	processor, err := application.NewProcessor(executor)
	if err != nil {
		t.Fatalf("application.NewProcessor() error = %v", err)
	}
	arrived := make(chan struct{}, 2)
	releaseProcessor := make(chan struct{})
	barrier := &barrierCommandProcessor{inner: processor, arrived: arrived, release: releaseProcessor}
	session, err := NewCommandSession(barrier)
	if err != nil {
		t.Fatalf("NewCommandSession() error = %v", err)
	}
	handler := mustHandler(t, &recordingAuthenticator{user: auth.User{ID: testUserID}}, session, DefaultConfig(testCookieName))
	server := httptest.NewServer(handler)
	defer server.Close()
	first, _, err := dial(t, server.URL, server.URL, testRawToken)
	if err != nil {
		t.Fatalf("first Dial() error = %v", err)
	}
	defer first.CloseNow()
	second, _, err := dial(t, server.URL, server.URL, testRawToken)
	if err != nil {
		t.Fatalf("second Dial() error = %v", err)
	}
	defer second.CloseNow()

	message := []byte(`{"version":1,"direction":"client_command","type":"THROW_YUT","command_id":"cmd-concurrent","room_id":"room-1","match_id":"match-1","payload":{}}`)
	writeCommand(t, first, message)
	writeCommand(t, second, message)
	for range 2 {
		select {
		case <-arrived:
		case <-time.After(2 * time.Second):
			t.Fatal("both WebSocket sessions did not reach the processor")
		}
	}
	close(releaseProcessor)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("executor did not start")
	}
	close(releaseExecutor)
	firstResponse := readCommandResult(t, first)
	secondResponse := readCommandResult(t, second)
	if string(firstResponse) != string(secondResponse) {
		t.Fatalf("concurrent duplicate response changed:\nfirst  = %s\nsecond = %s", firstResponse, secondResponse)
	}
	if got := executor.calls.Load(); got != 1 {
		t.Fatalf("Execute() calls = %d, want 1", got)
	}
}

func TestCommandSessionClosesOnConflictingCommandIDReuse(t *testing.T) {
	executor := &sessionExecutor{outcomes: []protocol.CommandOutcome{{Status: protocol.CommandAccepted}}}
	processor, err := application.NewProcessor(executor)
	if err != nil {
		t.Fatalf("application.NewProcessor() error = %v", err)
	}
	session, err := NewCommandSession(processor)
	if err != nil {
		t.Fatalf("NewCommandSession() error = %v", err)
	}
	handler := mustHandler(t, &recordingAuthenticator{user: auth.User{ID: testUserID}}, session, DefaultConfig(testCookieName))
	server := httptest.NewServer(handler)
	defer server.Close()
	connection, _, err := dial(t, server.URL, server.URL, testRawToken)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer connection.CloseNow()

	first := []byte(`{"version":1,"direction":"client_command","type":"SEND_CHAT","command_id":"cmd-1","room_id":"room-1","payload":{"text":"first"}}`)
	commandRoundTrip(t, connection, first)
	conflict := []byte(`{"version":1,"direction":"client_command","type":"SEND_CHAT","command_id":"cmd-1","room_id":"room-1","payload":{"text":"different"}}`)
	if err := connection.Write(context.Background(), websocket.MessageText, conflict); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	_, _, err = connection.Read(context.Background())
	if got := websocket.CloseStatus(err); got != websocket.StatusPolicyViolation {
		t.Fatalf("close status = %v, want %v, error = %v", got, websocket.StatusPolicyViolation, err)
	}
	var closeError websocket.CloseError
	if !errors.As(err, &closeError) || closeError.Reason != "command_id_conflict" {
		t.Fatalf("close error = %#v, want reason command_id_conflict", err)
	}
	if executor.callCount() != 1 {
		t.Fatalf("Execute() calls = %d, want 1", executor.callCount())
	}
}

func TestNewCommandSessionRejectsMissingProcessor(t *testing.T) {
	t.Parallel()

	if session, err := NewCommandSession(nil); !errors.Is(err, ErrInvalidConfiguration) || session != nil {
		t.Fatalf("NewCommandSession(nil) = %v, %v", session, err)
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

func commandRoundTrip(t *testing.T, connection *websocket.Conn, message []byte) []byte {
	t.Helper()
	writeCommand(t, connection, message)
	return readCommandResult(t, connection)
}

func writeCommand(t *testing.T, connection *websocket.Conn, message []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := connection.Write(ctx, websocket.MessageText, message); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
}

func readCommandResult(t *testing.T, connection *websocket.Conn) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	messageType, response, err := connection.Read(ctx)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if messageType != websocket.MessageText {
		t.Fatalf("response type = %v, want text", messageType)
	}
	return response
}

type recordingAuthenticator struct {
	mu    sync.Mutex
	user  auth.User
	err   error
	token string
	calls int
}

type sessionExecutor struct {
	mu       sync.Mutex
	outcomes []protocol.CommandOutcome
	calls    int
}

type blockingSessionExecutor struct {
	started chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (executor *blockingSessionExecutor) Execute(ctx context.Context, _ auth.User, _ protocol.ClientCommand) (protocol.CommandOutcome, error) {
	if executor.calls.Add(1) == 1 {
		close(executor.started)
	}
	select {
	case <-executor.release:
		return protocol.CommandOutcome{Status: protocol.CommandAccepted}, nil
	case <-ctx.Done():
		return protocol.CommandOutcome{}, ctx.Err()
	}
}

type barrierCommandProcessor struct {
	inner   CommandProcessor
	arrived chan struct{}
	release chan struct{}
}

func (processor *barrierCommandProcessor) Process(ctx context.Context, user auth.User, command protocol.ClientCommand) (protocol.CommandResult, error) {
	select {
	case processor.arrived <- struct{}{}:
	case <-ctx.Done():
		return protocol.CommandResult{}, ctx.Err()
	}
	select {
	case <-processor.release:
		return processor.inner.Process(ctx, user, command)
	case <-ctx.Done():
		return protocol.CommandResult{}, ctx.Err()
	}
}

func (executor *sessionExecutor) Execute(_ context.Context, _ auth.User, _ protocol.ClientCommand) (protocol.CommandOutcome, error) {
	executor.mu.Lock()
	defer executor.mu.Unlock()
	index := executor.calls
	executor.calls++
	if index >= len(executor.outcomes) {
		return protocol.CommandOutcome{}, errors.New("unexpected executor call")
	}
	return executor.outcomes[index], nil
}

func (executor *sessionExecutor) callCount() int {
	executor.mu.Lock()
	defer executor.mu.Unlock()
	return executor.calls
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
