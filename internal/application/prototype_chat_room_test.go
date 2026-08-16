package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/protocol"
)

const chatTestUserID auth.UserID = "usr_EREREREREREREREREREREQ"

func TestPrototypeChatRoomPublishesKoreanMessageToEverySubscriber(t *testing.T) {
	clock := &chatTestClock{current: time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)}
	room := mustPrototypeChatRoom(t, clock.Now)
	sender := mustChatSubscription(t, room, chatTestUserID)
	defer sender.Close()
	observer := mustChatSubscription(t, room, auth.UserID("usr_IiIiIiIiIiIiIiIiIiIiIg"))
	defer observer.Close()

	outcome, err := room.Execute(context.Background(), auth.User{ID: chatTestUserID}, chatCommand("cmd-1", "한글 채팅 👋"))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	assertAcceptedChatSequence(t, outcome, 1)
	senderEvent := readChatEvent(t, sender)
	observerEvent := readChatEvent(t, observer)
	if senderEvent != observerEvent {
		t.Fatalf("subscriber events differ:\nsender = %+v\nobserver = %+v", senderEvent, observerEvent)
	}
	if senderEvent.Payload.Text != "한글 채팅 👋" || senderEvent.Payload.SenderUserID != chatTestUserID || senderEvent.Payload.MessageID != "chat-1" {
		t.Fatalf("CHAT_MESSAGE = %+v", senderEvent)
	}
}

func TestPrototypeChatRoomProcessorReplaysWithoutRepublishing(t *testing.T) {
	clock := &chatTestClock{current: time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)}
	room := mustPrototypeChatRoom(t, clock.Now)
	observer := mustChatSubscription(t, room, chatTestUserID)
	defer observer.Close()
	processor, err := NewProcessor(room)
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}
	command := chatCommand("cmd-replay", "한 번만 전달")

	first, err := processor.Process(context.Background(), auth.User{ID: chatTestUserID}, command)
	if err != nil {
		t.Fatalf("first Process() error = %v", err)
	}
	readChatEvent(t, observer)
	second, err := processor.Process(context.Background(), auth.User{ID: chatTestUserID}, command)
	if err != nil {
		t.Fatalf("second Process() error = %v", err)
	}
	firstJSON, _ := json.Marshal(first)
	secondJSON, _ := json.Marshal(second)
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("replayed result changed:\nfirst = %s\nsecond = %s", firstJSON, secondJSON)
	}
	assertNoChatEvent(t, observer)
}

func TestPrototypeChatRoomRejectsWrongRoomAndUnsupportedCommand(t *testing.T) {
	clock := &chatTestClock{current: time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)}
	room := mustPrototypeChatRoom(t, clock.Now)
	subscription := mustChatSubscription(t, room, chatTestUserID)
	defer subscription.Close()

	wrongRoom := chatCommand("cmd-room", "hello")
	wrongRoom.RoomID = "other-room"
	outcome, err := room.Execute(context.Background(), auth.User{ID: chatTestUserID}, wrongRoom)
	if err != nil {
		t.Fatalf("wrong room Execute() error = %v", err)
	}
	assertChatRejection(t, outcome, "ROOM_NOT_FOUND", true)

	unsupported := protocol.ClientCommand{
		Version: protocol.Version1, Direction: protocol.DirectionClientCommand,
		Type: protocol.CommandSetReady, CommandID: "cmd-ready", RoomID: PrototypeRoomID,
		Payload: protocol.SetReadyPayload{Ready: true},
	}
	outcome, err = room.Execute(context.Background(), auth.User{ID: chatTestUserID}, unsupported)
	if err != nil {
		t.Fatalf("unsupported Execute() error = %v", err)
	}
	assertChatRejection(t, outcome, "APPLICATION_UNAVAILABLE", true)
	assertNoChatEvent(t, subscription)
}

func TestPrototypeChatRoomProcessorDoesNotRetainUnknownRoomRejection(t *testing.T) {
	room := mustPrototypeChatRoom(t, time.Now)
	processor, err := NewProcessor(room)
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}
	command := chatCommand("cmd-unknown-room", "hello")
	command.RoomID = "unknown-room"
	for attempt := 1; attempt <= 2; attempt++ {
		result, err := processor.Process(context.Background(), auth.User{ID: chatTestUserID}, command)
		if err != nil {
			t.Fatalf("Process(%d) error = %v", attempt, err)
		}
		if result.Payload.Error == nil || result.Payload.Error.Code != "ROOM_NOT_FOUND" || !result.Payload.Error.Retriable {
			t.Fatalf("Process(%d) result = %+v", attempt, result)
		}
	}
	processor.mu.Lock()
	entryCount := len(processor.entries)
	processor.mu.Unlock()
	if entryCount != 0 {
		t.Fatalf("idempotency entries = %d, want 0", entryCount)
	}
}

func TestPrototypeChatRoomAppliesRateDuplicateAndBlockPolicies(t *testing.T) {
	t.Run("sliding one second maximum", func(t *testing.T) {
		clock := &chatTestClock{current: time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)}
		room := mustPrototypeChatRoom(t, clock.Now)
		subscription := mustChatSubscription(t, room, chatTestUserID)
		defer subscription.Close()
		for index := 1; index <= 3; index++ {
			outcome, err := room.Execute(context.Background(), auth.User{ID: chatTestUserID}, chatCommand(fmt.Sprintf("cmd-%d", index), fmt.Sprintf("message-%d", index)))
			if err != nil {
				t.Fatalf("accepted Execute(%d) error = %v", index, err)
			}
			assertAcceptedChatSequence(t, outcome, uint64(index))
			readChatEvent(t, subscription)
		}
		outcome, err := room.Execute(context.Background(), auth.User{ID: chatTestUserID}, chatCommand("cmd-4", "message-4"))
		if err != nil {
			t.Fatalf("rate-limited Execute() error = %v", err)
		}
		assertChatRejection(t, outcome, "CHAT_RATE_LIMITED", true)
		clock.Advance(time.Second)
		outcome, err = room.Execute(context.Background(), auth.User{ID: chatTestUserID}, chatCommand("cmd-5", "message-5"))
		if err != nil {
			t.Fatalf("post-window Execute() error = %v", err)
		}
		assertAcceptedChatSequence(t, outcome, 4)
	})

	t.Run("exact duplicate within five seconds", func(t *testing.T) {
		clock := &chatTestClock{current: time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)}
		room := mustPrototypeChatRoom(t, clock.Now)
		subscription := mustChatSubscription(t, room, chatTestUserID)
		defer subscription.Close()
		first, err := room.Execute(context.Background(), auth.User{ID: chatTestUserID}, chatCommand("cmd-first", "repeat me"))
		if err != nil {
			t.Fatalf("first Execute() error = %v", err)
		}
		assertAcceptedChatSequence(t, first, 1)
		readChatEvent(t, subscription)
		clock.Advance(4 * time.Second)
		duplicate, err := room.Execute(context.Background(), auth.User{ID: chatTestUserID}, chatCommand("cmd-duplicate", "repeat me"))
		if err != nil {
			t.Fatalf("duplicate Execute() error = %v", err)
		}
		assertChatRejection(t, duplicate, "CHAT_DUPLICATE", true)
		clock.Advance(time.Second)
		afterWindow, err := room.Execute(context.Background(), auth.User{ID: chatTestUserID}, chatCommand("cmd-after", "repeat me"))
		if err != nil {
			t.Fatalf("after-window Execute() error = %v", err)
		}
		assertAcceptedChatSequence(t, afterWindow, 2)
	})

	t.Run("sixteenth attempt blocks for one minute", func(t *testing.T) {
		clock := &chatTestClock{current: time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)}
		room := mustPrototypeChatRoom(t, clock.Now)
		for index := 1; index <= 15; index++ {
			outcome, err := room.Execute(context.Background(), auth.User{ID: chatTestUserID}, chatCommand(fmt.Sprintf("cmd-%d", index), fmt.Sprintf("attempt-%d", index)))
			if err != nil {
				t.Fatalf("attempt %d error = %v", index, err)
			}
			if index <= 3 {
				assertAcceptedChatSequence(t, outcome, uint64(index))
			} else {
				assertChatRejection(t, outcome, "CHAT_RATE_LIMITED", true)
			}
		}
		blocked, err := room.Execute(context.Background(), auth.User{ID: chatTestUserID}, chatCommand("cmd-16", "attempt-16"))
		if err != nil {
			t.Fatalf("blocking attempt error = %v", err)
		}
		assertChatRejection(t, blocked, "CHAT_BLOCKED", true)
		clock.Advance(59 * time.Second)
		stillBlocked, err := room.Execute(context.Background(), auth.User{ID: chatTestUserID}, chatCommand("cmd-17", "attempt-17"))
		if err != nil {
			t.Fatalf("blocked Execute() error = %v", err)
		}
		assertChatRejection(t, stillBlocked, "CHAT_BLOCKED", true)
		clock.Advance(time.Second)
		restored, err := room.Execute(context.Background(), auth.User{ID: chatTestUserID}, chatCommand("cmd-18", "attempt-18"))
		if err != nil {
			t.Fatalf("restored Execute() error = %v", err)
		}
		assertAcceptedChatSequence(t, restored, 4)
	})
}

func TestPrototypeChatRoomDisconnectsOverflowedSubscriberWithoutAffectingOthers(t *testing.T) {
	clock := &chatTestClock{current: time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)}
	room := mustPrototypeChatRoom(t, clock.Now)
	slow := mustChatSubscription(t, room, chatTestUserID)
	defer slow.Close()
	fast := mustChatSubscription(t, room, auth.UserID("usr_IiIiIiIiIiIiIiIiIiIiIg"))
	defer fast.Close()

	for index := 1; index <= chatSubscriberBuffer+1; index++ {
		outcome, err := room.Execute(context.Background(), auth.User{ID: chatTestUserID}, chatCommand(fmt.Sprintf("cmd-%d", index), fmt.Sprintf("message-%d", index)))
		if err != nil {
			t.Fatalf("Execute(%d) error = %v", index, err)
		}
		assertAcceptedChatSequence(t, outcome, uint64(index))
		readChatEvent(t, fast)
		clock.Advance(6 * time.Second)
	}
	for range chatSubscriberBuffer {
		readChatEvent(t, slow)
	}
	if _, ok := <-slow.Events(); ok {
		t.Fatal("overflowed subscription remains open")
	}

	outcome, err := room.Execute(context.Background(), auth.User{ID: chatTestUserID}, chatCommand("cmd-final", "still delivered"))
	if err != nil {
		t.Fatalf("final Execute() error = %v", err)
	}
	assertAcceptedChatSequence(t, outcome, uint64(chatSubscriberBuffer+2))
	if event := readChatEvent(t, fast); event.Payload.Text != "still delivered" {
		t.Fatalf("fast subscriber event = %+v", event)
	}
}

func TestPrototypeChatRoomSerializesClockObservationWithCommands(t *testing.T) {
	base := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	firstClockEntered := make(chan struct{})
	releaseFirstClock := make(chan struct{})
	var calls atomic.Int32
	clock := func() time.Time {
		call := calls.Add(1)
		if call == 1 {
			close(firstClockEntered)
			<-releaseFirstClock
		}
		return base.Add(time.Duration(call) * time.Millisecond)
	}
	room := mustPrototypeChatRoom(t, clock)
	type execution struct {
		outcome protocol.CommandOutcome
		err     error
	}
	results := make(chan execution, 2)
	execute := func(commandID, text string) {
		outcome, err := room.Execute(context.Background(), auth.User{ID: chatTestUserID}, chatCommand(commandID, text))
		results <- execution{outcome: outcome, err: err}
	}
	go execute("cmd-first", "first")
	select {
	case <-firstClockEntered:
	case <-time.After(time.Second):
		t.Fatal("first command did not enter clock")
	}
	go execute("cmd-second", "second")
	time.Sleep(20 * time.Millisecond)
	close(releaseFirstClock)

	seenSequences := make(map[uint64]bool)
	for range 2 {
		select {
		case result := <-results:
			if result.err != nil {
				t.Fatalf("concurrent Execute() error = %v", result.err)
			}
			if result.outcome.EventSequenceStart == nil {
				t.Fatalf("concurrent Execute() outcome = %+v", result.outcome)
			}
			seenSequences[*result.outcome.EventSequenceStart] = true
		case <-time.After(time.Second):
			t.Fatal("concurrent commands did not complete")
		}
	}
	if !seenSequences[1] || !seenSequences[2] {
		t.Fatalf("concurrent sequences = %v, want 1 and 2", seenSequences)
	}
}

func TestNewPrototypeChatRoomRejectsInvalidDependencies(t *testing.T) {
	if room, err := NewPrototypeChatRoom(nil, time.Now); !errors.Is(err, ErrInvalidConfiguration) || room != nil {
		t.Fatalf("NewPrototypeChatRoom(nil sequences) = %v, %v", room, err)
	}
	if room, err := NewPrototypeChatRoom(NewRoomEventSequences(), nil); !errors.Is(err, ErrInvalidConfiguration) || room != nil {
		t.Fatalf("NewPrototypeChatRoom(nil clock) = %v, %v", room, err)
	}
}

func chatCommand(commandID, text string) protocol.ClientCommand {
	return protocol.ClientCommand{
		Version: protocol.Version1, Direction: protocol.DirectionClientCommand,
		Type: protocol.CommandSendChat, CommandID: commandID, RoomID: PrototypeRoomID,
		Payload: protocol.SendChatPayload{Text: text},
	}
}

func mustPrototypeChatRoom(t *testing.T, now func() time.Time) *PrototypeChatRoom {
	t.Helper()
	room, err := NewPrototypeChatRoom(NewRoomEventSequences(), now)
	if err != nil {
		t.Fatalf("NewPrototypeChatRoom() error = %v", err)
	}
	return room
}

func mustChatSubscription(t *testing.T, room *PrototypeChatRoom, userID auth.UserID) ChatSubscription {
	t.Helper()
	subscription, err := room.Subscribe(auth.User{ID: userID})
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	return subscription
}

func readChatEvent(t *testing.T, subscription ChatSubscription) protocol.ChatMessageEvent {
	t.Helper()
	select {
	case event, ok := <-subscription.Events():
		if !ok {
			t.Fatal("subscription closed before event")
		}
		return event
	case <-time.After(time.Second):
		t.Fatal("chat event not delivered")
		return protocol.ChatMessageEvent{}
	}
}

func assertNoChatEvent(t *testing.T, subscription ChatSubscription) {
	t.Helper()
	select {
	case event, ok := <-subscription.Events():
		t.Fatalf("unexpected chat event = %+v, open = %v", event, ok)
	case <-time.After(20 * time.Millisecond):
	}
}

func assertAcceptedChatSequence(t *testing.T, outcome protocol.CommandOutcome, want uint64) {
	t.Helper()
	if outcome.Status != protocol.CommandAccepted || outcome.EventSequenceStart == nil || outcome.EventSequenceEnd == nil || *outcome.EventSequenceStart != want || *outcome.EventSequenceEnd != want || outcome.Error != nil {
		t.Fatalf("accepted outcome = %+v, want sequence %d", outcome, want)
	}
}

func assertChatRejection(t *testing.T, outcome protocol.CommandOutcome, code string, retriable bool) {
	t.Helper()
	if outcome.Status != protocol.CommandRejected || outcome.EventSequenceStart != nil || outcome.EventSequenceEnd != nil || outcome.Error == nil || outcome.Error.Code != code || outcome.Error.Retriable != retriable {
		t.Fatalf("rejected outcome = %+v, want code=%s retriable=%v", outcome, code, retriable)
	}
}

type chatTestClock struct {
	current time.Time
}

func (clock *chatTestClock) Now() time.Time {
	return clock.current
}

func (clock *chatTestClock) Advance(duration time.Duration) {
	clock.current = clock.current.Add(duration)
}
