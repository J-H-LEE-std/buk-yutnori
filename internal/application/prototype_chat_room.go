package application

import (
	"context"
	"fmt"
	"sync"
	"time"
	"unicode/utf8"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/protocol"
)

const (
	// PrototypeRoomID is the only room exposed by the Milestone 2 chat
	// prototype. The authoritative room lifecycle replaces it in Milestone 3.
	PrototypeRoomID domain.RoomID = "prototype-room"

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

// ChatEventSource registers authenticated connections for prototype chat.
type ChatEventSource interface {
	Subscribe(auth.User) (ChatSubscription, error)
}

// PrototypeChatRoom owns the temporary in-memory room chat state, limits and
// subscribers used by the Milestone 2 vertical prototype.
type PrototypeChatRoom struct {
	mutex          sync.Mutex
	sequences      *RoomEventSequences
	now            func() time.Time
	nextSubscriber uint64
	subscribers    map[uint64]*prototypeChatSubscription
	users          map[auth.UserID]*chatUserState
}

type chatUserState struct {
	attempts         []time.Time
	accepted         []time.Time
	blockedUntil     time.Time
	lastAcceptedText string
	lastAcceptedAt   time.Time
	lastObservedAt   time.Time
}

type prototypeChatSubscription struct {
	room   *PrototypeChatRoom
	id     uint64
	events chan protocol.ChatMessageEvent
	done   chan struct{}
}

// NewPrototypeChatRoom constructs the fixed in-memory chat room.
func NewPrototypeChatRoom(sequences *RoomEventSequences, now func() time.Time) (*PrototypeChatRoom, error) {
	if sequences == nil || now == nil {
		return nil, fmt.Errorf("%w: chat sequences and clock are required", ErrInvalidConfiguration)
	}
	return &PrototypeChatRoom{
		sequences:   sequences,
		now:         now,
		subscribers: make(map[uint64]*prototypeChatSubscription),
		users:       make(map[auth.UserID]*chatUserState),
	}, nil
}

// Subscribe adds one authenticated connection to the prototype room.
func (room *PrototypeChatRoom) Subscribe(user auth.User) (ChatSubscription, error) {
	if room == nil || room.sequences == nil || room.now == nil {
		return nil, fmt.Errorf("%w: prototype chat room is required", ErrInvalidConfiguration)
	}
	if err := user.ID.Validate(); err != nil {
		return nil, fmt.Errorf("%w: authenticated user_id", ErrInvalidCommand)
	}

	room.mutex.Lock()
	defer room.mutex.Unlock()
	room.nextSubscriber++
	subscription := &prototypeChatSubscription{
		room: room, id: room.nextSubscriber,
		events: make(chan protocol.ChatMessageEvent, chatSubscriberBuffer),
		done:   make(chan struct{}),
	}
	room.subscribers[subscription.id] = subscription
	return subscription, nil
}

// Execute implements Executor. SEND_CHAT is authoritative in the fixed room;
// other commands remain transiently unavailable until their application
// owners exist.
func (room *PrototypeChatRoom) Execute(ctx context.Context, user auth.User, command protocol.ClientCommand) (protocol.CommandOutcome, error) {
	if room == nil || room.sequences == nil || room.now == nil {
		return protocol.CommandOutcome{}, fmt.Errorf("%w: prototype chat room is required", ErrInvalidConfiguration)
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
	if command.RoomID != PrototypeRoomID {
		return rejectedPrototypeCommand("ROOM_NOT_FOUND", "prototype room not found", true), nil
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
		return rejectedPrototypeCommand("CHAT_BLOCKED", "chat is temporarily blocked", true), nil
	}

	state.attempts = timestampsAfter(state.attempts, now.Add(-chatAttemptWindow))
	state.attempts = append(state.attempts, now)
	if len(state.attempts) > chatAttemptLimit {
		state.blockedUntil = now.Add(chatBlockDuration)
		return rejectedPrototypeCommand("CHAT_BLOCKED", "chat is temporarily blocked", true), nil
	}
	if payload.Text == state.lastAcceptedText && !state.lastAcceptedAt.IsZero() && now.Sub(state.lastAcceptedAt) < chatDuplicateWindow {
		return rejectedPrototypeCommand("CHAT_DUPLICATE", "duplicate chat message", true), nil
	}
	state.accepted = timestampsAfter(state.accepted, now.Add(-chatSecondWindow))
	if len(state.accepted) >= chatPerSecondLimit {
		return rejectedPrototypeCommand("CHAT_RATE_LIMITED", "chat rate limit exceeded", true), nil
	}

	sequence, err := room.sequences.CommitNext(PrototypeRoomID)
	if err != nil {
		return protocol.CommandOutcome{}, fmt.Errorf("commit chat sequence: %w", err)
	}
	event, err := protocol.NewChatMessageEvent(PrototypeRoomID, sequence, user.ID, payload.Text, now)
	if err != nil {
		return protocol.CommandOutcome{}, fmt.Errorf("build chat event: %w", err)
	}
	state.accepted = append(state.accepted, now)
	state.lastAcceptedText = payload.Text
	state.lastAcceptedAt = now
	room.publishLocked(event)
	return protocol.CommandOutcome{
		Status:             protocol.CommandAccepted,
		EventSequenceStart: uint64Pointer(sequence),
		EventSequenceEnd:   uint64Pointer(sequence),
	}, nil
}

func (room *PrototypeChatRoom) publishLocked(event protocol.ChatMessageEvent) {
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

func (room *PrototypeChatRoom) unsubscribe(subscription *prototypeChatSubscription) {
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

func (subscription *prototypeChatSubscription) Events() <-chan protocol.ChatMessageEvent {
	return subscription.events
}

func (subscription *prototypeChatSubscription) Done() <-chan struct{} {
	return subscription.done
}

func (subscription *prototypeChatSubscription) Close() {
	if subscription != nil && subscription.room != nil {
		subscription.room.unsubscribe(subscription)
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
