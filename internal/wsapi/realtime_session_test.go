package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"buk-yutnori/internal/application"
	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/protocol"

	"github.com/coder/websocket"
)

func TestRealtimeSessionBroadcastsChatAndReplaysOnlyCommandResult(t *testing.T) {
	room, err := application.NewPrototypeChatRoom(application.NewRoomEventSequences(), time.Now)
	if err != nil {
		t.Fatalf("NewPrototypeChatRoom() error = %v", err)
	}
	processor, err := application.NewProcessor(room)
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}
	session, err := NewRealtimeSession(processor, room)
	if err != nil {
		t.Fatalf("NewRealtimeSession() error = %v", err)
	}
	handler := mustHandler(t, &recordingAuthenticator{user: auth.User{ID: testUserID}}, session, DefaultConfig(testCookieName))
	server := httptest.NewServer(handler)
	defer server.Close()

	sender, _, err := dial(t, server.URL, server.URL, testRawToken)
	if err != nil {
		t.Fatalf("sender Dial() error = %v", err)
	}
	defer sender.CloseNow()
	observer, _, err := dial(t, server.URL, server.URL, testRawToken)
	if err != nil {
		t.Fatalf("observer Dial() error = %v", err)
	}
	defer observer.CloseNow()

	// A response proves that the observer session subscribed before the chat.
	observerReady := []byte(`{"version":1,"direction":"client_command","type":"SET_READY","command_id":"observer-ready","room_id":"prototype-room","payload":{"ready":true}}`)
	var readyResult protocol.CommandResult
	readWebSocketJSON(t, observer, observerReady, &readyResult)
	if readyResult.Payload.Status != protocol.CommandRejected {
		t.Fatalf("observer ready result = %+v", readyResult)
	}

	message := []byte(`{"version":1,"direction":"client_command","type":"SEND_CHAT","command_id":"chat-1","room_id":"prototype-room","payload":{"text":"두 브라우저 한글 채팅"}}`)
	writeCommand(t, sender, message)
	var senderResult protocol.CommandResult
	var senderEvent protocol.ChatMessageEvent
	for range 2 {
		frame := readTextFrame(t, sender)
		var envelope struct {
			Direction protocol.Direction `json:"direction"`
		}
		if err := json.Unmarshal(frame, &envelope); err != nil {
			t.Fatalf("decode sender envelope: %v", err)
		}
		switch envelope.Direction {
		case protocol.DirectionServerResponse:
			if err := json.Unmarshal(frame, &senderResult); err != nil {
				t.Fatalf("decode sender result: %v", err)
			}
		case protocol.DirectionServerEvent:
			if err := json.Unmarshal(frame, &senderEvent); err != nil {
				t.Fatalf("decode sender event: %v", err)
			}
		default:
			t.Fatalf("unexpected sender direction %q", envelope.Direction)
		}
	}
	if senderResult.Payload.Status != protocol.CommandAccepted || senderEvent.Payload.Text != "두 브라우저 한글 채팅" {
		t.Fatalf("sender result/event = %+v / %+v", senderResult, senderEvent)
	}
	var observerEvent protocol.ChatMessageEvent
	readWebSocketJSON(t, observer, nil, &observerEvent)
	if observerEvent != senderEvent {
		t.Fatalf("observer event = %+v, want %+v", observerEvent, senderEvent)
	}

	writeCommand(t, sender, message)
	var replay protocol.CommandResult
	readWebSocketJSON(t, sender, nil, &replay)
	if replay.CommandID != senderResult.CommandID || replay.Payload.EventSequenceStart == nil || senderResult.Payload.EventSequenceStart == nil || *replay.Payload.EventSequenceStart != *senderResult.Payload.EventSequenceStart {
		t.Fatalf("replayed result = %+v, want %+v", replay, senderResult)
	}
	assertNoWebSocketFrame(t, observer)
}

func TestNewRealtimeSessionRejectsMissingDependencies(t *testing.T) {
	room, err := application.NewPrototypeChatRoom(application.NewRoomEventSequences(), time.Now)
	if err != nil {
		t.Fatalf("NewPrototypeChatRoom() error = %v", err)
	}
	processor, err := application.NewProcessor(room)
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}
	if session, err := NewRealtimeSession(nil, room); err == nil || session != nil {
		t.Fatalf("NewRealtimeSession(nil processor) = %v, %v", session, err)
	}
	if session, err := NewRealtimeSession(processor, nil); err == nil || session != nil {
		t.Fatalf("NewRealtimeSession(nil source) = %v, %v", session, err)
	}
}

func TestRealtimeSessionInitiatesBackpressureCloseBeforeEndingBlockedWrite(t *testing.T) {
	room, err := application.NewPrototypeChatRoom(application.NewRoomEventSequences(), time.Now)
	if err != nil {
		t.Fatalf("NewPrototypeChatRoom() error = %v", err)
	}
	processor, err := application.NewProcessor(room)
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}
	subscription := &controlledChatSubscription{
		events: make(chan protocol.ChatMessageEvent, 1),
		done:   make(chan struct{}),
	}
	session, err := NewRealtimeSession(processor, staticChatEventSource{subscription: subscription})
	if err != nil {
		t.Fatalf("NewRealtimeSession() error = %v", err)
	}
	connection := &blockingRealtimeConnection{
		writeStarted:    make(chan struct{}),
		closeCalled:     make(chan struct{}),
		contextCanceled: make(chan struct{}),
	}
	subscription.events <- protocol.ChatMessageEvent{}

	served := make(chan error, 1)
	go func() {
		served <- session.serve(context.Background(), auth.User{ID: testUserID}, connection)
	}()
	select {
	case <-connection.writeStarted:
	case <-time.After(time.Second):
		t.Fatal("session did not begin event write")
	}
	close(subscription.done)

	select {
	case err := <-served:
		if !errors.Is(err, ErrEventBackpressure) {
			t.Fatalf("serve() error = %v, want ErrEventBackpressure", err)
		}
	case <-time.After(time.Second):
		t.Fatal("dropped subscription did not end blocked write")
	}
	select {
	case <-connection.closeCalled:
	default:
		t.Fatal("session did not close connection for event backpressure")
	}
	select {
	case <-connection.contextCanceled:
		t.Fatal("session canceled the WebSocket operation before initiating the close handshake")
	default:
	}
}

func TestRealtimeSessionSendsTryAgainLaterWhenSubscriptionIsDropped(t *testing.T) {
	room, err := application.NewPrototypeChatRoom(application.NewRoomEventSequences(), time.Now)
	if err != nil {
		t.Fatalf("NewPrototypeChatRoom() error = %v", err)
	}
	processor, err := application.NewProcessor(room)
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}
	subscription := &controlledChatSubscription{
		events: make(chan protocol.ChatMessageEvent),
		done:   make(chan struct{}),
	}
	session, err := NewRealtimeSession(processor, staticChatEventSource{subscription: subscription})
	if err != nil {
		t.Fatalf("NewRealtimeSession() error = %v", err)
	}
	handler := mustHandler(t, &recordingAuthenticator{user: auth.User{ID: testUserID}}, session, DefaultConfig(testCookieName))
	server := httptest.NewServer(handler)
	defer server.Close()
	client, _, err := dial(t, server.URL, server.URL, testRawToken)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer client.CloseNow()

	close(subscription.done)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, err = client.Read(ctx)
	if status := websocket.CloseStatus(err); status != websocket.StatusTryAgainLater {
		t.Fatalf("close status = %v, error = %v, want %v", status, err, websocket.StatusTryAgainLater)
	}
	var closeError websocket.CloseError
	if !errors.As(err, &closeError) || closeError.Reason != "event_backpressure" {
		t.Fatalf("close error = %v, want event_backpressure", err)
	}
}

func TestRealtimeSessionClosesInvalidSubscription(t *testing.T) {
	room, err := application.NewPrototypeChatRoom(application.NewRoomEventSequences(), time.Now)
	if err != nil {
		t.Fatalf("NewPrototypeChatRoom() error = %v", err)
	}
	processor, err := application.NewProcessor(room)
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}
	subscription := &controlledChatSubscription{
		events: make(chan protocol.ChatMessageEvent),
		closed: make(chan struct{}),
	}
	session, err := NewRealtimeSession(processor, staticChatEventSource{subscription: subscription})
	if err != nil {
		t.Fatalf("NewRealtimeSession() error = %v", err)
	}
	connection := &blockingRealtimeConnection{}

	err = session.serve(context.Background(), auth.User{ID: testUserID}, connection)
	if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("serve() error = %v, want ErrInvalidConfiguration", err)
	}
	select {
	case <-subscription.closed:
	default:
		t.Fatal("invalid subscription was not closed")
	}
}

func readWebSocketJSON(t *testing.T, connection *websocket.Conn, command []byte, destination any) {
	t.Helper()
	if command != nil {
		writeCommand(t, connection, command)
	}
	frame := readTextFrame(t, connection)
	if err := json.Unmarshal(frame, destination); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, frame = %s", err, frame)
	}
}

func readTextFrame(t *testing.T, connection *websocket.Conn) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	messageType, frame, err := connection.Read(ctx)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if messageType != websocket.MessageText {
		t.Fatalf("message type = %v, want text", messageType)
	}
	return frame
}

func assertNoWebSocketFrame(t *testing.T, connection *websocket.Conn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, _, err := connection.Read(ctx)
	if err == nil {
		t.Fatal("unexpected WebSocket frame")
	}
}

type staticChatEventSource struct {
	subscription application.ChatSubscription
}

func (source staticChatEventSource) Subscribe(auth.User) (application.ChatSubscription, error) {
	return source.subscription, nil
}

type controlledChatSubscription struct {
	events    chan protocol.ChatMessageEvent
	done      chan struct{}
	closed    chan struct{}
	closeOnce sync.Once
}

func (subscription *controlledChatSubscription) Events() <-chan protocol.ChatMessageEvent {
	return subscription.events
}

func (subscription *controlledChatSubscription) Done() <-chan struct{} {
	return subscription.done
}

func (subscription *controlledChatSubscription) Close() {
	if subscription.closed != nil {
		subscription.closeOnce.Do(func() { close(subscription.closed) })
	}
}

type blockingRealtimeConnection struct {
	writeOnce       sync.Once
	closeOnce       sync.Once
	cancelOnce      sync.Once
	writeStarted    chan struct{}
	closeCalled     chan struct{}
	contextCanceled chan struct{}
}

func (connection *blockingRealtimeConnection) ReadCommand(ctx context.Context) (protocol.ClientCommand, error) {
	<-ctx.Done()
	return protocol.ClientCommand{}, ctx.Err()
}

func (connection *blockingRealtimeConnection) WriteJSON(ctx context.Context, _ any) error {
	if connection.writeStarted != nil {
		connection.writeOnce.Do(func() { close(connection.writeStarted) })
	}
	select {
	case <-connection.closeCalled:
		return errors.New("write interrupted by close")
	case <-ctx.Done():
		if connection.contextCanceled != nil {
			connection.cancelOnce.Do(func() { close(connection.contextCanceled) })
		}
		return ctx.Err()
	}
}

func (*blockingRealtimeConnection) CloseCommandIDConflict() error {
	return ErrInvalidCommand
}

func (connection *blockingRealtimeConnection) CloseEventBackpressure() error {
	if connection.closeCalled != nil {
		connection.closeOnce.Do(func() { close(connection.closeCalled) })
	}
	return ErrEventBackpressure
}
