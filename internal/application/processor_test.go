package application

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/protocol"
)

func TestProcessorReplaysSequentialDuplicateWithoutReexecution(t *testing.T) {
	t.Parallel()

	start, end := uint64(7), uint64(9)
	executor := &recordingExecutor{outcomes: []protocol.CommandOutcome{{
		Status: protocol.CommandAccepted, EventSequenceStart: &start, EventSequenceEnd: &end,
	}}}
	processor := mustProcessor(t, executor)
	command := testCommand("cmd-1", "hello")

	first, err := processor.Process(context.Background(), testUserA, command)
	if err != nil {
		t.Fatalf("first Process() error = %v", err)
	}
	second, err := processor.Process(context.Background(), testUserA, command)
	if err != nil {
		t.Fatalf("duplicate Process() error = %v", err)
	}
	if executor.callCount() != 1 {
		t.Fatalf("Execute() calls = %d, want 1", executor.callCount())
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("duplicate result differs:\nfirst  = %+v\nsecond = %+v", first, second)
	}
	if second.Payload.EventSequenceStart == nil || *second.Payload.EventSequenceStart != 7 || second.Payload.EventSequenceEnd == nil || *second.Payload.EventSequenceEnd != 9 {
		t.Fatalf("replayed sequence range = %+v", second.Payload)
	}
	*second.Payload.EventSequenceStart = 100
	third, err := processor.Process(context.Background(), testUserA, command)
	if err != nil {
		t.Fatalf("third Process() error = %v", err)
	}
	if third.Payload.EventSequenceStart == nil || *third.Payload.EventSequenceStart != 7 {
		t.Fatalf("caller mutation changed retained result: %+v", third.Payload)
	}
}

func TestProcessorSharesOneExecutionAcrossConcurrentDuplicates(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	executor := &blockingExecutor{started: started, release: release}
	processor := mustProcessor(t, executor)
	command := testCommand("cmd-concurrent", "same")

	const callers = 16
	results := make(chan protocol.CommandResult, callers)
	errorsChannel := make(chan error, callers)
	var waitGroup sync.WaitGroup
	for range callers {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			result, err := processor.Process(context.Background(), testUserA, command)
			results <- result
			errorsChannel <- err
		}()
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("executor did not start")
	}
	close(release)
	waitGroup.Wait()
	close(results)
	close(errorsChannel)
	for err := range errorsChannel {
		if err != nil {
			t.Fatalf("Process() error = %v", err)
		}
	}
	for result := range results {
		if result.Payload.Status != protocol.CommandAccepted {
			t.Fatalf("result = %+v", result)
		}
	}
	if got := executor.calls.Load(); got != 1 {
		t.Fatalf("Execute() calls = %d, want 1", got)
	}
}

func TestProcessorRejectsCommandIDReuseWithDifferentContent(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{outcomes: []protocol.CommandOutcome{{Status: protocol.CommandAccepted}}}
	processor := mustProcessor(t, executor)
	if _, err := processor.Process(context.Background(), testUserA, testCommand("cmd-1", "first")); err != nil {
		t.Fatalf("first Process() error = %v", err)
	}
	if _, err := processor.Process(context.Background(), testUserA, testCommand("cmd-1", "different")); !errors.Is(err, ErrCommandIDConflict) {
		t.Fatalf("conflicting Process() error = %v, want ErrCommandIDConflict", err)
	}
	if executor.callCount() != 1 {
		t.Fatalf("Execute() calls = %d, want 1", executor.callCount())
	}
}

func TestProcessorComparesTheFullDecodedCommandEnvelope(t *testing.T) {
	t.Parallel()

	base := testCommand("cmd-envelope", "same")
	base.RequestID = stringPointer("request-1")
	base.MatchID = matchIDPointer("match-1")
	tests := []struct {
		name   string
		mutate func(*protocol.ClientCommand)
	}{
		{name: "request_id", mutate: func(command *protocol.ClientCommand) { command.RequestID = stringPointer("request-2") }},
		{name: "type", mutate: func(command *protocol.ClientCommand) {
			command.Type = protocol.CommandSetReady
			command.Payload = protocol.SetReadyPayload{Ready: true}
		}},
		{name: "room_id", mutate: func(command *protocol.ClientCommand) { command.RoomID = "room-2" }},
		{name: "match_id", mutate: func(command *protocol.ClientCommand) { command.MatchID = matchIDPointer("match-2") }},
		{name: "payload", mutate: func(command *protocol.ClientCommand) { command.Payload = protocol.SendChatPayload{Text: "different"} }},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			executor := &recordingExecutor{outcomes: []protocol.CommandOutcome{{Status: protocol.CommandAccepted}}}
			processor := mustProcessor(t, executor)
			if _, err := processor.Process(context.Background(), testUserA, base); err != nil {
				t.Fatalf("first Process() error = %v", err)
			}
			changed := cloneCommand(base)
			test.mutate(&changed)
			if _, err := processor.Process(context.Background(), testUserA, changed); !errors.Is(err, ErrCommandIDConflict) {
				t.Fatalf("conflicting Process() error = %v, want ErrCommandIDConflict", err)
			}
			if executor.callCount() != 1 {
				t.Fatalf("Execute() calls = %d, want 1", executor.callCount())
			}
		})
	}
}

func TestProcessorScopesCommandIDByAuthenticatedUser(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{outcomes: []protocol.CommandOutcome{
		{Status: protocol.CommandAccepted},
		{Status: protocol.CommandAccepted},
	}}
	processor := mustProcessor(t, executor)
	command := testCommand("shared-command", "hello")
	for _, user := range []auth.User{testUserA, testUserB} {
		if _, err := processor.Process(context.Background(), user, command); err != nil {
			t.Fatalf("Process(%s) error = %v", user.ID, err)
		}
	}
	if executor.callCount() != 2 {
		t.Fatalf("Execute() calls = %d, want 2", executor.callCount())
	}
}

func TestProcessorDoesNotRetainRetriableRejectionOrExecutorFailure(t *testing.T) {
	t.Parallel()

	t.Run("retriable rejection", func(t *testing.T) {
		executor := &recordingExecutor{outcomes: []protocol.CommandOutcome{
			{Status: protocol.CommandRejected, Error: &protocol.CommandError{Code: "TEMPORARY", Message: "retry", Retriable: true}},
			{Status: protocol.CommandAccepted},
		}}
		processor := mustProcessor(t, executor)
		command := testCommand("cmd-retry", "hello")
		first, err := processor.Process(context.Background(), testUserA, command)
		if err != nil || first.Payload.Status != protocol.CommandRejected {
			t.Fatalf("first Process() = %+v, %v", first, err)
		}
		second, err := processor.Process(context.Background(), testUserA, command)
		if err != nil || second.Payload.Status != protocol.CommandAccepted {
			t.Fatalf("second Process() = %+v, %v", second, err)
		}
		if executor.callCount() != 2 {
			t.Fatalf("Execute() calls = %d, want 2", executor.callCount())
		}
	})

	t.Run("executor failure", func(t *testing.T) {
		failure := errors.New("transient executor failure")
		executor := &recordingExecutor{
			outcomes: []protocol.CommandOutcome{{}, {Status: protocol.CommandAccepted}},
			errors:   []error{failure, nil},
		}
		processor := mustProcessor(t, executor)
		command := testCommand("cmd-error", "hello")
		if _, err := processor.Process(context.Background(), testUserA, command); !errors.Is(err, failure) {
			t.Fatalf("first Process() error = %v, want executor failure", err)
		}
		if _, err := processor.Process(context.Background(), testUserA, command); err != nil {
			t.Fatalf("second Process() error = %v", err)
		}
		if executor.callCount() != 2 {
			t.Fatalf("Execute() calls = %d, want 2", executor.callCount())
		}
	})
}

func TestProcessorRetainsDeterministicRejection(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{outcomes: []protocol.CommandOutcome{{
		Status: protocol.CommandRejected,
		Error:  &protocol.CommandError{Code: "NOT_ALLOWED", Message: "not allowed", Retriable: false},
	}}}
	processor := mustProcessor(t, executor)
	command := testCommand("cmd-rejected", "hello")
	first, err := processor.Process(context.Background(), testUserA, command)
	if err != nil {
		t.Fatalf("first Process() error = %v", err)
	}
	second, err := processor.Process(context.Background(), testUserA, command)
	if err != nil {
		t.Fatalf("duplicate Process() error = %v", err)
	}
	if !reflect.DeepEqual(first, second) || second.Payload.Error == nil || second.Payload.Error.Code != "NOT_ALLOWED" {
		t.Fatalf("replayed rejection = %+v, first = %+v", second, first)
	}
	if executor.callCount() != 1 {
		t.Fatalf("Execute() calls = %d, want 1", executor.callCount())
	}
}

func TestProcessorForgetClosedRoomEvictsOnlyThatRoomsCompletedResults(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{outcomes: []protocol.CommandOutcome{
		{Status: protocol.CommandAccepted},
		{Status: protocol.CommandAccepted},
		{Status: protocol.CommandAccepted},
	}}
	processor := mustProcessor(t, executor)
	roomOne := testCommand("cmd-room-1", "one")
	roomTwo := testCommand("cmd-room-2", "two")
	roomTwo.RoomID = "room-2"
	if _, err := processor.Process(context.Background(), testUserA, roomOne); err != nil {
		t.Fatalf("room one Process() error = %v", err)
	}
	if _, err := processor.Process(context.Background(), testUserA, roomTwo); err != nil {
		t.Fatalf("room two Process() error = %v", err)
	}

	processor.ForgetClosedRoom(roomOne.RoomID)
	processor.mu.Lock()
	_, roomOneRetained := processor.entries[commandKey{userID: testUserA.ID, commandID: roomOne.CommandID}]
	processor.mu.Unlock()
	if roomOneRetained {
		t.Fatal("closed room result remains retained")
	}
	if _, err := processor.Process(context.Background(), testUserA, roomTwo); err != nil {
		t.Fatalf("retained room Process() error = %v", err)
	}
	if executor.callCount() != 2 {
		t.Fatalf("Execute() calls = %d, want 2", executor.callCount())
	}
}

func TestProcessorCanceledDuplicateDoesNotCancelOrReplaceFirstExecution(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	executor := &blockingExecutor{started: started, release: release}
	processor := mustProcessor(t, executor)
	command := testCommand("cmd-cancel", "same")
	firstResult := make(chan error, 1)
	go func() {
		_, err := processor.Process(context.Background(), testUserA, command)
		firstResult <- err
	}()
	<-started

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := processor.Process(canceled, testUserA, command); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled duplicate error = %v, want context.Canceled", err)
	}
	close(release)
	if err := <-firstResult; err != nil {
		t.Fatalf("first Process() error = %v", err)
	}
	if _, err := processor.Process(context.Background(), testUserA, command); err != nil {
		t.Fatalf("replay Process() error = %v", err)
	}
	if got := executor.calls.Load(); got != 1 {
		t.Fatalf("Execute() calls = %d, want 1", got)
	}
}

func TestProcessorForgetClosedRoomDefersInflightEvictionUntilExecutionCompletes(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	executor := &blockingExecutor{started: started, release: release}
	processor := mustProcessor(t, executor)
	command := testCommand("cmd-forget-inflight", "same")
	firstResult := make(chan error, 1)
	go func() {
		_, err := processor.Process(context.Background(), testUserA, command)
		firstResult <- err
	}()
	<-started
	processor.ForgetClosedRoom(command.RoomID)
	processor.mu.Lock()
	entry, exists := processor.entries[commandKey{userID: testUserA.ID, commandID: command.CommandID}]
	if !exists || entry.completed || !entry.evict {
		processor.mu.Unlock()
		t.Fatalf("in-flight entry after ForgetClosedRoom() = %+v, exists = %v", entry, exists)
	}
	processor.mu.Unlock()
	close(release)
	if err := <-firstResult; err != nil {
		t.Fatalf("first Process() error = %v", err)
	}
	if got := executor.calls.Load(); got != 1 {
		t.Fatalf("Execute() calls before eviction retry = %d, want 1", got)
	}
	processor.mu.Lock()
	_, retained := processor.entries[commandKey{userID: testUserA.ID, commandID: command.CommandID}]
	processor.mu.Unlock()
	if retained {
		t.Fatal("completed in-flight result remains after closed-room eviction")
	}
	if got := executor.calls.Load(); got != 1 {
		t.Fatalf("Execute() calls = %d, want 1", got)
	}
}

func TestNewProcessorRejectsMissingExecutor(t *testing.T) {
	t.Parallel()

	if processor, err := NewProcessor(nil); !errors.Is(err, ErrInvalidConfiguration) || processor != nil {
		t.Fatalf("NewProcessor(nil) = %v, %v", processor, err)
	}
	var typedNil *recordingExecutor
	if processor, err := NewProcessor(typedNil); !errors.Is(err, ErrInvalidConfiguration) || processor != nil {
		t.Fatalf("NewProcessor(typed nil) = %v, %v", processor, err)
	}
}

func mustProcessor(t *testing.T, executor Executor) *Processor {
	t.Helper()
	processor, err := NewProcessor(executor)
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}
	return processor
}

func testCommand(commandID, text string) protocol.ClientCommand {
	return protocol.ClientCommand{
		Version:   protocol.Version1,
		Direction: protocol.DirectionClientCommand,
		Type:      protocol.CommandSendChat,
		CommandID: commandID,
		RoomID:    "room-1",
		Payload:   protocol.SendChatPayload{Text: text},
	}
}

func stringPointer(value string) *string {
	return &value
}

func matchIDPointer(value domain.MatchID) *domain.MatchID {
	return &value
}

type recordingExecutor struct {
	mu       sync.Mutex
	outcomes []protocol.CommandOutcome
	errors   []error
	calls    int
}

func (e *recordingExecutor) Execute(_ context.Context, _ auth.User, _ protocol.ClientCommand) (protocol.CommandOutcome, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	index := e.calls
	e.calls++
	var outcome protocol.CommandOutcome
	if index < len(e.outcomes) {
		outcome = e.outcomes[index]
	}
	var err error
	if index < len(e.errors) {
		err = e.errors[index]
	}
	return outcome, err
}

func (e *recordingExecutor) callCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

type blockingExecutor struct {
	started chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (e *blockingExecutor) Execute(ctx context.Context, _ auth.User, _ protocol.ClientCommand) (protocol.CommandOutcome, error) {
	if e.calls.Add(1) == 1 {
		close(e.started)
	}
	select {
	case <-e.release:
		return protocol.CommandOutcome{Status: protocol.CommandAccepted}, nil
	case <-ctx.Done():
		return protocol.CommandOutcome{}, ctx.Err()
	}
}

var (
	testUserA = auth.User{ID: "usr_EREREREREREREREREREREQ"}
	testUserB = auth.User{ID: "usr_IiIiIiIiIiIiIiIiIiIiIg"}
)
