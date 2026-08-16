// Package wsapi exposes the authenticated WebSocket transport boundary.
package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/protocol"

	"github.com/coder/websocket"
)

const DefaultMaxMessageBytes int64 = 16 * 1024

var (
	ErrInvalidConfiguration = errors.New("invalid WebSocket configuration")
	ErrUnsupportedData      = errors.New("unsupported WebSocket data")
	ErrInvalidCommand       = errors.New("invalid WebSocket command")
	ErrMessageTooBig        = errors.New("WebSocket message too big")
	ErrEventBackpressure    = errors.New("WebSocket event backpressure")
)

// Authenticator resolves a raw HttpOnly cookie to a server-owned user.
type Authenticator interface {
	Authenticate(ctx context.Context, rawToken string) (auth.User, error)
}

// Session owns application behavior for one authenticated connection.
type Session interface {
	Serve(ctx context.Context, user auth.User, connection *Connection) error
}

// SessionFunc adapts a function to Session.
type SessionFunc func(context.Context, auth.User, *Connection) error

func (function SessionFunc) Serve(ctx context.Context, user auth.User, connection *Connection) error {
	return function(ctx, user, connection)
}

// Config contains transport limits and the server session cookie name.
type Config struct {
	SessionCookieName string
	MaxMessageBytes   int64
}

// DefaultConfig returns the browser transport defaults.
func DefaultConfig(sessionCookieName string) Config {
	return Config{SessionCookieName: sessionCookieName, MaxMessageBytes: DefaultMaxMessageBytes}
}

type handler struct {
	authenticator Authenticator
	session       Session
	config        Config
}

// NewHandler constructs the authenticated WebSocket endpoint.
func NewHandler(authenticator Authenticator, session Session, config Config) (http.Handler, error) {
	if authenticator == nil || session == nil || config.SessionCookieName == "" || config.MaxMessageBytes <= 0 {
		return nil, ErrInvalidConfiguration
	}
	return &handler{authenticator: authenticator, session: session, config: config}, nil
}

func (h *handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet || request.URL.Path != "/api/v1/ws" {
		writeHandshakeError(response, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	if !hasSameHostOrigin(request) {
		writeHandshakeError(response, http.StatusForbidden, "request_not_allowed")
		return
	}
	cookie, err := request.Cookie(h.config.SessionCookieName)
	if err != nil || cookie.Value == "" {
		writeHandshakeError(response, http.StatusUnauthorized, "unauthenticated")
		return
	}
	user, err := h.authenticator.Authenticate(request.Context(), cookie.Value)
	if err != nil {
		if errors.Is(err, auth.ErrUnauthenticated) {
			writeHandshakeError(response, http.StatusUnauthorized, "unauthenticated")
			return
		}
		writeHandshakeError(response, http.StatusInternalServerError, "internal_error")
		return
	}
	if err := user.ID.Validate(); err != nil {
		writeHandshakeError(response, http.StatusInternalServerError, "internal_error")
		return
	}

	connection, err := websocket.Accept(response, request, &websocket.AcceptOptions{
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		return
	}
	defer connection.CloseNow()
	connection.SetReadLimit(h.config.MaxMessageBytes)

	sessionContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	wrapped := &Connection{connection: connection}
	if err := h.session.Serve(sessionContext, user, wrapped); err != nil {
		if websocket.CloseStatus(err) != -1 || errors.Is(err, ErrUnsupportedData) || errors.Is(err, ErrInvalidCommand) || errors.Is(err, ErrMessageTooBig) || errors.Is(err, ErrEventBackpressure) {
			return
		}
		_ = connection.Close(websocket.StatusInternalError, "session_failed")
	}
}

// Connection is the project-owned text JSON adapter around a WebSocket.
type Connection struct {
	connection *websocket.Conn
}

// ReadCommand receives and strictly decodes one v1 client command.
func (c *Connection) ReadCommand(ctx context.Context) (protocol.ClientCommand, error) {
	messageType, message, err := c.connection.Read(ctx)
	if err != nil {
		if errors.Is(err, websocket.ErrMessageTooBig) {
			return protocol.ClientCommand{}, fmt.Errorf("%w: %v", ErrMessageTooBig, err)
		}
		return protocol.ClientCommand{}, err
	}
	if messageType != websocket.MessageText {
		_ = c.connection.Close(websocket.StatusUnsupportedData, "text_json_required")
		return protocol.ClientCommand{}, ErrUnsupportedData
	}
	command, err := protocol.DecodeClientCommand(message)
	if err != nil {
		_ = c.connection.Close(websocket.StatusPolicyViolation, "invalid_command")
		return protocol.ClientCommand{}, fmt.Errorf("%w: %v", ErrInvalidCommand, err)
	}
	return command, nil
}

// WriteJSON sends one trusted application value as a text JSON message.
func (c *Connection) WriteJSON(ctx context.Context, value any) error {
	message, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal WebSocket JSON: %w", err)
	}
	if err := c.connection.Write(ctx, websocket.MessageText, message); err != nil {
		return fmt.Errorf("write WebSocket JSON: %w", err)
	}
	return nil
}

// CloseNormal completes a normal WebSocket close handshake.
func (c *Connection) CloseNormal(reason string) error {
	return c.connection.Close(websocket.StatusNormalClosure, reason)
}

// CloseCommandIDConflict rejects semantic reuse of an idempotency key as a
// protocol policy violation.
func (c *Connection) CloseCommandIDConflict() error {
	if err := c.connection.Close(websocket.StatusPolicyViolation, "command_id_conflict"); err != nil {
		return fmt.Errorf("%w: close command_id conflict: %v", ErrInvalidCommand, err)
	}
	return ErrInvalidCommand
}

// CloseEventBackpressure disconnects a subscriber before silently dropping a
// room event and creating an unrecoverable sequence gap.
func (c *Connection) CloseEventBackpressure() error {
	if err := c.connection.Close(websocket.StatusTryAgainLater, "event_backpressure"); err != nil {
		return fmt.Errorf("%w: close event backpressure: %v", ErrEventBackpressure, err)
	}
	return ErrEventBackpressure
}

func hasSameHostOrigin(request *http.Request) bool {
	origins := request.Header.Values("Origin")
	if len(origins) != 1 {
		return false
	}
	origin, err := url.Parse(origins[0])
	if err != nil || (origin.Scheme != "http" && origin.Scheme != "https") || origin.Host == "" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		return false
	}
	return strings.EqualFold(origin.Host, request.Host)
}

func writeHandshakeError(response http.ResponseWriter, status int, code string) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(struct {
		Error string `json:"error"`
	}{Error: code})
}
