package application

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"
	"unicode/utf8"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/protocol"
	"buk-yutnori/internal/storage"
)

const (
	// LobbyChatRoomID is the permanent WebSocket scope for authenticated
	// lobby-wide chat. It is not a joinable game room and has its own sequence
	// space, so room membership and match replay can never authorize it.
	LobbyChatRoomID domain.RoomID = "lobby"

	chatSubscriberBuffer = 16
	chatPerSecondLimit   = 3
	chatAttemptLimit     = 15
	chatSecondWindow     = time.Second
	chatAttemptWindow    = 5 * time.Second
	chatDuplicateWindow  = 5 * time.Second
	chatBlockDuration    = time.Minute
)

// ChatSubscription is one authenticated connection's bounded event stream.
// Done closes when the room drops the subscription, including backpressure.
type ChatSubscription interface {
	Events() <-chan protocol.ChatMessageEvent
	Done() <-chan struct{}
	Close()
}

// ChatEventSource registers authenticated connections for lobby-wide chat.
type ChatEventSource interface {
	Subscribe(auth.User) (ChatSubscription, error)
}

// LobbyChatRoom owns authoritative lobby-wide chat admission, limits and
// active subscriptions. Accepted events are delivered immediately; durable
// logging is deliberately asynchronous and never affects that delivery.
type LobbyChatRoom struct {
	mutex          sync.Mutex
	sequences      *RoomEventSequences
	now            func() time.Time
	nextSubscriber uint64
	subscribers    map[uint64]*lobbyChatSubscription
	users          map[auth.UserID]*chatUserState
	logs           *lobbyChatLogWriter
	closedLogs     *lobbyChatLogWriter
}

type chatUserState struct {
	attempts         []time.Time
	accepted         []time.Time
	blockedUntil     time.Time
	lastAcceptedText string
	lastAcceptedAt   time.Time
	lastObservedAt   time.Time
}

type lobbyChatSubscription struct {
	room   *LobbyChatRoom
	id     uint64
	events chan protocol.ChatMessageEvent
	done   chan struct{}
}

// NewLobbyChatRoom constructs the authenticated lobby chat scope. logStore is
// optional for memory-only tests, but production supplies the SQLite event
// store so accepted messages are recorded without entering the match commit
// path or blocking WebSocket delivery.
func NewLobbyChatRoom(sequences *RoomEventSequences, now func() time.Time, logStore storage.EventStore) (*LobbyChatRoom, error) {
	if sequences == nil || now == nil {
		return nil, fmt.Errorf("%w: chat sequences and clock are required", ErrInvalidConfiguration)
	}
	room := &LobbyChatRoom{
		sequences:   sequences,
		now:         now,
		subscribers: make(map[uint64]*lobbyChatSubscription),
		users:       make(map[auth.UserID]*chatUserState),
	}
	if logStore != nil {
		if err := restoreLobbyChatBoundary(sequences, logStore); err != nil {
			return nil, err
		}
		room.logs = newLobbyChatLogWriter(logStore)
	}
	return room, nil
}

func restoreLobbyChatBoundary(sequences *RoomEventSequences, logStore storage.EventStore) error {
	ctx, cancel := eventStoreContext()
	defer cancel()
	var boundary uint64
	if reader, ok := logStore.(interface {
		LatestRoomEventSequence(context.Context, domain.RoomID) (uint64, error)
	}); ok {
		var err error
		boundary, err = reader.LatestRoomEventSequence(ctx, LobbyChatRoomID)
		if err != nil {
			return fmt.Errorf("restore latest lobby chat sequence: %w", err)
		}
	} else {
		rows, err := logStore.ReadRoomEventsAfter(ctx, LobbyChatRoomID, 0)
		if err != nil {
			return fmt.Errorf("restore lobby chat log boundary: %w", err)
		}
		for _, row := range rows {
			if row.Sequence > boundary {
				boundary = row.Sequence
			}
		}
	}
	if err := sequences.RestoreBoundary(LobbyChatRoomID, boundary); err != nil {
		return fmt.Errorf("restore lobby chat sequence: %w", err)
	}
	return nil
}

// Subscribe adds one authenticated connection to the lobby-wide chat stream.
func (room *LobbyChatRoom) Subscribe(user auth.User) (ChatSubscription, error) {
	if room == nil || room.sequences == nil || room.now == nil {
		return nil, fmt.Errorf("%w: lobby chat room is required", ErrInvalidConfiguration)
	}
	if err := user.ID.Validate(); err != nil {
		return nil, fmt.Errorf("%w: authenticated user_id", ErrInvalidCommand)
	}

	room.mutex.Lock()
	defer room.mutex.Unlock()
	room.nextSubscriber++
	subscription := &lobbyChatSubscription{
		room: room, id: room.nextSubscriber,
		events: make(chan protocol.ChatMessageEvent, chatSubscriberBuffer),
		done:   make(chan struct{}),
	}
	room.subscribers[subscription.id] = subscription
	return subscription, nil
}

// Execute implements Executor for the dedicated lobby scope only.
func (room *LobbyChatRoom) Execute(ctx context.Context, user auth.User, command protocol.ClientCommand) (protocol.CommandOutcome, error) {
	if room == nil || room.sequences == nil || room.now == nil {
		return protocol.CommandOutcome{}, fmt.Errorf("%w: lobby chat room is required", ErrInvalidConfiguration)
	}
	if err := ctx.Err(); err != nil {
		return protocol.CommandOutcome{}, err
	}
	if err := user.ID.Validate(); err != nil {
		return protocol.CommandOutcome{}, fmt.Errorf("%w: authenticated user_id", ErrInvalidCommand)
	}
	if command.Type != protocol.CommandSendChat {
		return (UnavailableExecutor{}).Execute(ctx, user, command)
	}
	if command.RoomID != LobbyChatRoomID {
		return rejectedLobbyChatCommand("ROOM_NOT_FOUND", "lobby chat scope not found", true), nil
	}
	payload, ok := command.Payload.(protocol.SendChatPayload)
	if !ok || payload.Text == "" || utf8.RuneCountInString(payload.Text) > protocol.MaxChatCodePoints {
		return protocol.CommandOutcome{}, fmt.Errorf("%w: invalid SEND_CHAT payload", ErrInvalidCommand)
	}

	room.mutex.Lock()
	defer room.mutex.Unlock()
	now := room.now()
	if now.IsZero() {
		return protocol.CommandOutcome{}, fmt.Errorf("%w: chat clock returned zero time", ErrInvalidConfiguration)
	}
	state := room.users[user.ID]
	if state == nil {
		state = &chatUserState{}
		room.users[user.ID] = state
	}
	if !state.lastObservedAt.IsZero() && now.Before(state.lastObservedAt) {
		return protocol.CommandOutcome{}, fmt.Errorf("%w: chat clock moved backwards", ErrInvalidConfiguration)
	}
	state.lastObservedAt = now
	if now.Before(state.blockedUntil) {
		return rejectedLobbyChatCommand("CHAT_BLOCKED", "chat is temporarily blocked", true), nil
	}

	state.attempts = timestampsAfter(state.attempts, now.Add(-chatAttemptWindow))
	state.attempts = append(state.attempts, now)
	if len(state.attempts) > chatAttemptLimit {
		state.blockedUntil = now.Add(chatBlockDuration)
		return rejectedLobbyChatCommand("CHAT_BLOCKED", "chat is temporarily blocked", true), nil
	}
	if payload.Text == state.lastAcceptedText && !state.lastAcceptedAt.IsZero() && now.Sub(state.lastAcceptedAt) < chatDuplicateWindow {
		return rejectedLobbyChatCommand("CHAT_DUPLICATE", "duplicate chat message", true), nil
	}
	state.accepted = timestampsAfter(state.accepted, now.Add(-chatSecondWindow))
	if len(state.accepted) >= chatPerSecondLimit {
		return rejectedLobbyChatCommand("CHAT_RATE_LIMITED", "chat rate limit exceeded", true), nil
	}

	sequence, err := room.sequences.CommitNext(LobbyChatRoomID)
	if err != nil {
		return protocol.CommandOutcome{}, fmt.Errorf("commit chat sequence: %w", err)
	}
	event, err := protocol.NewChatMessageEvent(LobbyChatRoomID, sequence, user.ID, payload.Text, now)
	if err != nil {
		return protocol.CommandOutcome{}, fmt.Errorf("build chat event: %w", err)
	}
	state.accepted = append(state.accepted, now)
	state.lastAcceptedText = payload.Text
	state.lastAcceptedAt = now
	room.publishLocked(event)
	room.persistBestEffort(event, now)
	return protocol.CommandOutcome{
		Status:             protocol.CommandAccepted,
		EventSequenceStart: uint64Pointer(sequence),
		EventSequenceEnd:   uint64Pointer(sequence),
	}, nil
}

func (room *LobbyChatRoom) publishLocked(event protocol.ChatMessageEvent) {
	for id, subscription := range room.subscribers {
		select {
		case subscription.events <- event:
		default:
			delete(room.subscribers, id)
			close(subscription.done)
			close(subscription.events)
		}
	}
}

func (room *LobbyChatRoom) unsubscribe(subscription *lobbyChatSubscription) {
	if room == nil || subscription == nil {
		return
	}
	room.mutex.Lock()
	defer room.mutex.Unlock()
	current, exists := room.subscribers[subscription.id]
	if !exists || current != subscription {
		return
	}
	delete(room.subscribers, subscription.id)
	close(subscription.done)
	close(subscription.events)
}

func (subscription *lobbyChatSubscription) Events() <-chan protocol.ChatMessageEvent {
	return subscription.events
}

func (subscription *lobbyChatSubscription) Done() <-chan struct{} {
	return subscription.done
}

func (subscription *lobbyChatSubscription) Close() {
	if subscription != nil && subscription.room != nil {
		subscription.room.unsubscribe(subscription)
	}
}

// Close stops the best-effort log worker after all accepted messages already
// queued by this process have been given a bounded chance to persist.
func (room *LobbyChatRoom) Close(ctx context.Context) error {
	if room == nil {
		return nil
	}
	room.mutex.Lock()
	writer := room.logs
	if writer != nil {
		room.logs = nil
		room.closedLogs = writer
	} else {
		writer = room.closedLogs
	}
	room.mutex.Unlock()
	if writer == nil {
		return nil
	}
	return writer.Close(ctx)
}

func (room *LobbyChatRoom) persistBestEffort(event protocol.ChatMessageEvent, sentAt time.Time) {
	if room.logs == nil {
		return
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		slog.Error("encode lobby chat log", "error", err, "sequence", event.Sequence)
		return
	}
	room.logs.Enqueue(storage.EventRow{
		RoomID: LobbyChatRoomID, Sequence: event.Sequence, EventType: protocol.EventChatMessage,
		PayloadJSON: encoded, CreatedAtMS: sentAt.UnixMilli(),
	})
}

const lobbyChatLogBuffer = 128

// lobbyChatLogWriter separates optional chat logging from authoritative chat
// delivery. A full queue or failed append is observable in structured logs;
// neither condition can reject a valid chat command or affect a match.
type lobbyChatLogWriter struct {
	store storage.EventStore
	queue chan storage.EventRow
	done  chan struct{}
	once  sync.Once
}

func newLobbyChatLogWriter(store storage.EventStore) *lobbyChatLogWriter {
	writer := &lobbyChatLogWriter{
		store: store, queue: make(chan storage.EventRow, lobbyChatLogBuffer), done: make(chan struct{}),
	}
	go writer.run()
	return writer
}

func (writer *lobbyChatLogWriter) Enqueue(row storage.EventRow) {
	select {
	case writer.queue <- row:
	default:
		slog.Error("drop lobby chat log after persistence queue saturation", "sequence", row.Sequence)
	}
}

func (writer *lobbyChatLogWriter) run() {
	defer close(writer.done)
	for row := range writer.queue {
		ctx, cancel := eventStoreContext()
		err := writer.store.AppendRoomEvents(ctx, []storage.EventRow{row})
		cancel()
		if err != nil {
			slog.Error("persist lobby chat log", "error", err, "sequence", row.Sequence)
		}
	}
}

func (writer *lobbyChatLogWriter) Close(ctx context.Context) error {
	if writer == nil {
		return nil
	}
	writer.once.Do(func() { close(writer.queue) })
	select {
	case <-writer.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func timestampsAfter(values []time.Time, cutoff time.Time) []time.Time {
	first := 0
	for first < len(values) && !values[first].After(cutoff) {
		first++
	}
	return append(values[:0], values[first:]...)
}

func uint64Pointer(value uint64) *uint64 {
	return &value
}
