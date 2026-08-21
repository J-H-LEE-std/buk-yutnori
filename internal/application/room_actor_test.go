package application

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/protocol"
)

func TestRoomActorSerializesAcceptedCommands(t *testing.T) {
	t.Parallel()

	executor := &actorSerialExecutor{
		started: make(chan string, 2),
		release: make(chan struct{}),
	}
	actor := mustRoomActor(t, "room-1", executor, func(domain.RoomID) {})
	defer closeRoomActor(t, actor)

	first := executeRoomCommand(actor, actorCommand("cmd-1", "room-1"))
	if commandID := receiveString(t, executor.started); commandID != "cmd-1" {
		t.Fatalf("first started command = %q", commandID)
	}
	second := executeRoomCommand(actor, actorCommand("cmd-2", "room-1"))
	assertNoString(t, executor.started)

	close(executor.release)
	assertActorExecutionSucceeded(t, <-first)
	if commandID := receiveString(t, executor.started); commandID != "cmd-2" {
		t.Fatalf("second started command = %q", commandID)
	}
	assertActorExecutionSucceeded(t, <-second)

	if got := executor.maxActive.Load(); got != 1 {
		t.Fatalf("maximum concurrent executions = %d, want 1", got)
	}
	executor.mu.Lock()
	completed := append([]string(nil), executor.completed...)
	executor.mu.Unlock()
	if len(completed) != 2 || completed[0] != "cmd-1" || completed[1] != "cmd-2" {
		t.Fatalf("completed command order = %v", completed)
	}
}

func TestRoomActorSerializesCommandAndInternalOperation(t *testing.T) {
	t.Parallel()

	executor := &actorBlockingExecutor{
		started: make(chan struct{}),
		release: make(chan struct{}),
		events:  make(chan string, 1),
	}
	actor := mustRoomActor(t, "room-1", executor, func(domain.RoomID) {})
	defer closeRoomActor(t, actor)

	command := executeRoomCommand(actor, actorCommand("cmd-first", "room-1"))
	receiveSignal(t, executor.started)
	operationStarted := make(chan struct{})
	operation := executeRoomOperation(actor, context.Background(), func(context.Context) error {
		close(operationStarted)
		return nil
	})
	assertNoSignal(t, operationStarted)

	close(executor.release)
	assertActorExecutionSucceeded(t, <-command)
	receiveSignal(t, operationStarted)
	if err := receiveError(t, operation); err != nil {
		t.Fatalf("ExecuteInternal() error = %v", err)
	}
}

func TestRoomActorOwnsAcceptedInternalOperationAfterCallerCancellation(t *testing.T) {
	t.Parallel()

	actor := mustRoomActor(t, "room-1", &actorImmediateExecutor{}, func(domain.RoomID) {})
	defer closeRoomActor(t, actor)

	started := make(chan struct{})
	release := make(chan struct{})
	cancelled := make(chan struct{}, 1)
	values := make(chan string, 1)
	ctxWithValue := context.WithValue(context.Background(), actorContextKey("trace_id"), "trace-internal")
	ctx, cancel := context.WithCancel(ctxWithValue)
	result := executeRoomOperation(actor, ctx, func(operationCtx context.Context) error {
		close(started)
		value, _ := operationCtx.Value(actorContextKey("trace_id")).(string)
		values <- value
		select {
		case <-operationCtx.Done():
			cancelled <- struct{}{}
			return operationCtx.Err()
		case <-release:
			return nil
		}
	})
	receiveSignal(t, started)
	if value := receiveString(t, values); value != "trace-internal" {
		t.Fatalf("accepted internal context value = %q, want trace-internal", value)
	}
	cancel()
	assertNoSignal(t, cancelled)
	assertNoError(t, result)

	close(release)
	if err := receiveError(t, result); err != nil {
		t.Fatalf("ExecuteInternal() error = %v", err)
	}
}

func TestRoomActorCloseRejectsInternalOperationsAndCleansUpAfterAcceptedOperation(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	events := make(chan string, 2)
	var calls atomic.Int32
	var cleanupCalls atomic.Int32
	actor := mustRoomActor(t, "room-1", &actorImmediateExecutor{}, func(domain.RoomID) {
		cleanupCalls.Add(1)
		events <- "cleanup"
	})

	accepted := executeRoomOperation(actor, context.Background(), func(context.Context) error {
		calls.Add(1)
		close(started)
		<-release
		events <- "operation-complete"
		return nil
	})
	receiveSignal(t, started)
	waiting := executeRoomOperation(actor, context.Background(), func(context.Context) error {
		calls.Add(1)
		return nil
	})
	assertNoError(t, waiting)
	closed := make(chan error, 1)
	go func() { closed <- actor.Close(context.Background()) }()
	receiveSignal(t, actor.stopping)

	if err := receiveError(t, waiting); !errors.Is(err, ErrRoomActorClosed) {
		t.Fatalf("waiting ExecuteInternal() error = %v, want ErrRoomActorClosed", err)
	}
	if err := actor.ExecuteInternal(context.Background(), func(context.Context) error { return nil }); !errors.Is(err, ErrRoomActorClosed) {
		t.Fatalf("ExecuteInternal() after Close() error = %v, want ErrRoomActorClosed", err)
	}
	assertNoError(t, closed)
	close(release)
	if err := receiveError(t, accepted); err != nil {
		t.Fatalf("accepted ExecuteInternal() error = %v", err)
	}
	if event := receiveString(t, events); event != "operation-complete" {
		t.Fatalf("first lifecycle event = %q, want operation-complete", event)
	}
	if event := receiveString(t, events); event != "cleanup" {
		t.Fatalf("second lifecycle event = %q, want cleanup", event)
	}
	if err := receiveError(t, closed); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("internal operation calls = %d, want 1", got)
	}
	if got := cleanupCalls.Load(); got != 1 {
		t.Fatalf("cleanup calls = %d, want 1", got)
	}
}

func TestRoomActorContainsInternalOperationPanicAndClosesFailedRoom(t *testing.T) {
	t.Parallel()

	var cleanupCalls atomic.Int32
	actor := mustRoomActor(t, "room-1", &actorImmediateExecutor{}, func(domain.RoomID) {
		cleanupCalls.Add(1)
	})

	err := actor.ExecuteInternal(context.Background(), func(context.Context) error {
		panic("internal operation failed")
	})
	if !errors.Is(err, ErrRoomActorPanicked) {
		t.Fatalf("panicking ExecuteInternal() error = %v, want ErrRoomActorPanicked", err)
	}
	if _, err := actor.Execute(context.Background(), actorTestUser, actorCommand("cmd-late", "room-1")); !errors.Is(err, ErrRoomActorClosed) {
		t.Fatalf("Execute() after internal panic error = %v, want ErrRoomActorClosed", err)
	}
	if err := actor.Close(context.Background()); !errors.Is(err, ErrRoomActorPanicked) {
		t.Fatalf("Close() after internal panic error = %v, want ErrRoomActorPanicked", err)
	}
	if got := cleanupCalls.Load(); got != 1 {
		t.Fatalf("cleanup calls after internal panic = %d, want 1", got)
	}
}

func TestRoomActorReturnsInternalOperationErrorWithoutClosing(t *testing.T) {
	t.Parallel()

	actor := mustRoomActor(t, "room-1", &actorImmediateExecutor{}, func(domain.RoomID) {})
	defer closeRoomActor(t, actor)
	want := errors.New("transition rejected")

	if err := actor.ExecuteInternal(context.Background(), func(context.Context) error { return want }); !errors.Is(err, want) {
		t.Fatalf("ExecuteInternal() error = %v, want %v", err, want)
	}
	assertActorExecutionSucceeded(t, receiveActorExecutionResult(t, executeRoomCommand(actor, actorCommand("cmd-after-error", "room-1"))))
}

func TestRoomActorRejectsCanceledOrInvalidInternalOperation(t *testing.T) {
	t.Parallel()

	executor := &actorBlockingExecutor{
		started: make(chan struct{}),
		release: make(chan struct{}),
		events:  make(chan string, 1),
	}
	actor := mustRoomActor(t, "room-1", executor, func(domain.RoomID) {})
	defer closeRoomActor(t, actor)

	command := executeRoomCommand(actor, actorCommand("cmd-first", "room-1"))
	receiveSignal(t, executor.started)
	var calls atomic.Int32
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := actor.ExecuteInternal(canceled, func(context.Context) error {
		calls.Add(1)
		return nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled ExecuteInternal() error = %v, want context.Canceled", err)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("canceled internal operation calls = %d, want 0", got)
	}
	if err := actor.ExecuteInternal(context.Background(), nil); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("nil ExecuteInternal() error = %v, want ErrInvalidConfiguration", err)
	}
	if err := actor.ExecuteInternal(nil, func(context.Context) error { return nil }); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("nil-context ExecuteInternal() error = %v, want ErrInvalidConfiguration", err)
	}
	var nilActor *RoomActor
	if err := nilActor.ExecuteInternal(context.Background(), func(context.Context) error { return nil }); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("nil-actor ExecuteInternal() error = %v, want ErrInvalidConfiguration", err)
	}

	close(executor.release)
	assertActorExecutionSucceeded(t, <-command)
}

func TestRoomActorCloseRejectsNewCommandsAndCleansUpAfterAcceptedExecution(t *testing.T) {
	t.Parallel()

	executor := &actorBlockingExecutor{
		started: make(chan struct{}),
		release: make(chan struct{}),
		events:  make(chan string, 2),
	}
	var cleanupCalls atomic.Int32
	actor := mustRoomActor(t, "room-1", executor, func(roomID domain.RoomID) {
		if roomID != "room-1" {
			t.Errorf("cleanup room_id = %q", roomID)
		}
		cleanupCalls.Add(1)
		executor.events <- "cleanup"
	})

	accepted := executeRoomCommand(actor, actorCommand("cmd-accepted", "room-1"))
	receiveSignal(t, executor.started)
	waiting := executeRoomCommand(actor, actorCommand("cmd-waiting", "room-1"))
	assertNoActorExecutionResult(t, waiting)
	closed := make(chan error, 1)
	go func() { closed <- actor.Close(context.Background()) }()
	receiveSignal(t, actor.stopping)

	assertActorExecutionError(t, receiveActorExecutionResult(t, waiting), ErrRoomActorClosed)
	if _, err := actor.Execute(context.Background(), actorTestUser, actorCommand("cmd-late", "room-1")); !errors.Is(err, ErrRoomActorClosed) {
		t.Fatalf("Execute() after Close() error = %v, want ErrRoomActorClosed", err)
	}
	assertNoError(t, closed)
	close(executor.release)
	assertActorExecutionSucceeded(t, <-accepted)
	if event := receiveString(t, executor.events); event != "execution-complete" {
		t.Fatalf("first lifecycle event = %q, want execution-complete", event)
	}
	if event := receiveString(t, executor.events); event != "cleanup" {
		t.Fatalf("second lifecycle event = %q, want cleanup", event)
	}
	if err := receiveError(t, closed); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := actor.Close(context.Background()); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if got := cleanupCalls.Load(); got != 1 {
		t.Fatalf("cleanup calls = %d, want 1", got)
	}
	if got := executor.calls.Load(); got != 1 {
		t.Fatalf("executor calls = %d, want 1", got)
	}
}

func TestRoomActorOwnsAcceptedExecutionAfterCallerCancellation(t *testing.T) {
	t.Parallel()

	executor := &actorCancellationExecutor{
		started:   make(chan struct{}),
		release:   make(chan struct{}),
		cancelled: make(chan struct{}, 1),
		values:    make(chan string, 1),
	}
	actor := mustRoomActor(t, "room-1", executor, func(domain.RoomID) {})
	defer closeRoomActor(t, actor)

	ctxWithValue := context.WithValue(context.Background(), actorContextKey("trace_id"), "trace-1")
	ctx, cancel := context.WithCancel(ctxWithValue)
	result := make(chan actorExecutionResult, 1)
	go func() {
		outcome, err := actor.Execute(ctx, actorTestUser, actorCommand("cmd-owned", "room-1"))
		result <- actorExecutionResult{outcome: outcome, err: err}
	}()
	receiveSignal(t, executor.started)
	if value := receiveString(t, executor.values); value != "trace-1" {
		t.Fatalf("accepted context value = %q, want trace-1", value)
	}
	cancel()
	assertNoSignal(t, executor.cancelled)
	assertNoActorExecutionResult(t, result)

	close(executor.release)
	assertActorExecutionSucceeded(t, <-result)
}

func TestRoomActorContainsExecutorPanicAndClosesFailedRoom(t *testing.T) {
	t.Parallel()

	var cleanupCalls atomic.Int32
	actor := mustRoomActor(t, "room-1", actorPanicExecutor{}, func(domain.RoomID) {
		cleanupCalls.Add(1)
	})

	result := receiveActorExecutionResult(t, executeRoomCommand(actor, actorCommand("cmd-panic", "room-1")))
	if !errors.Is(result.err, ErrRoomActorPanicked) {
		t.Fatalf("panicking Execute() error = %v, want ErrRoomActorPanicked", result.err)
	}
	if _, err := actor.Execute(context.Background(), actorTestUser, actorCommand("cmd-late", "room-1")); !errors.Is(err, ErrRoomActorClosed) {
		t.Fatalf("Execute() after executor panic error = %v, want ErrRoomActorClosed", err)
	}
	if err := actor.Close(context.Background()); !errors.Is(err, ErrRoomActorPanicked) {
		t.Fatalf("Close() after executor panic error = %v, want ErrRoomActorPanicked", err)
	}
	if got := cleanupCalls.Load(); got != 1 {
		t.Fatalf("cleanup calls after executor panic = %d, want 1", got)
	}

	healthy := mustRoomActor(t, "room-2", &actorImmediateExecutor{}, func(domain.RoomID) {})
	defer closeRoomActor(t, healthy)
	assertActorExecutionSucceeded(t, receiveActorExecutionResult(t, executeRoomCommand(healthy, actorCommand("cmd-healthy", "room-2"))))
}

func TestRoomActorContainsCleanupPanicAndReportsTerminalError(t *testing.T) {
	t.Parallel()

	actor := mustRoomActor(t, "room-1", &actorImmediateExecutor{}, func(domain.RoomID) {
		panic("cleanup failed")
	})
	assertActorExecutionSucceeded(t, receiveActorExecutionResult(t, executeRoomCommand(actor, actorCommand("cmd-1", "room-1"))))

	if err := actor.Close(context.Background()); !errors.Is(err, ErrRoomActorPanicked) {
		t.Fatalf("Close() after cleanup panic error = %v, want ErrRoomActorPanicked", err)
	}
	if err := actor.Close(context.Background()); !errors.Is(err, ErrRoomActorPanicked) {
		t.Fatalf("second Close() after cleanup panic error = %v, want ErrRoomActorPanicked", err)
	}
}

func TestRoomActorCloseTimeoutDoesNotCancelClosure(t *testing.T) {
	t.Parallel()

	executor := &actorBlockingExecutor{
		started: make(chan struct{}),
		release: make(chan struct{}),
		events:  make(chan string, 2),
	}
	var cleanupCalls atomic.Int32
	actor := mustRoomActor(t, "room-1", executor, func(domain.RoomID) {
		cleanupCalls.Add(1)
	})

	accepted := executeRoomCommand(actor, actorCommand("cmd-accepted", "room-1"))
	receiveSignal(t, executor.started)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := actor.Close(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Close() error = %v, want context.Canceled", err)
	}
	assertNoSignal(t, actor.done)

	close(executor.release)
	assertActorExecutionSucceeded(t, <-accepted)
	closeRoomActor(t, actor)
	if got := cleanupCalls.Load(); got != 1 {
		t.Fatalf("cleanup calls = %d, want 1", got)
	}
}

func TestRoomActorDoesNotAdmitCanceledCommandWhileBusy(t *testing.T) {
	t.Parallel()

	executor := &actorBlockingExecutor{
		started: make(chan struct{}),
		release: make(chan struct{}),
		events:  make(chan string, 2),
	}
	actor := mustRoomActor(t, "room-1", executor, func(domain.RoomID) {})
	defer closeRoomActor(t, actor)

	first := executeRoomCommand(actor, actorCommand("cmd-first", "room-1"))
	receiveSignal(t, executor.started)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := actor.Execute(canceled, actorTestUser, actorCommand("cmd-canceled", "room-1")); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Execute() error = %v, want context.Canceled", err)
	}
	if got := executor.calls.Load(); got != 1 {
		t.Fatalf("executor calls before release = %d, want 1", got)
	}
	close(executor.release)
	assertActorExecutionSucceeded(t, <-first)
}

func TestRoomActorsExecuteDifferentRoomsIndependently(t *testing.T) {
	t.Parallel()

	blockedExecutor := &actorBlockingExecutor{
		started: make(chan struct{}),
		release: make(chan struct{}),
		events:  make(chan string, 2),
	}
	fastExecutor := &actorImmediateExecutor{}
	blocked := mustRoomActor(t, "room-blocked", blockedExecutor, func(domain.RoomID) {})
	fast := mustRoomActor(t, "room-fast", fastExecutor, func(domain.RoomID) {})
	defer closeRoomActor(t, blocked)
	defer closeRoomActor(t, fast)

	blockedResult := executeRoomCommand(blocked, actorCommand("cmd-blocked", "room-blocked"))
	receiveSignal(t, blockedExecutor.started)
	fastResult := executeRoomCommand(fast, actorCommand("cmd-fast", "room-fast"))
	select {
	case result := <-fastResult:
		assertActorExecutionSucceeded(t, result)
	case <-time.After(time.Second):
		t.Fatal("different room actor was blocked")
	}
	close(blockedExecutor.release)
	assertActorExecutionSucceeded(t, <-blockedResult)
}

func TestRoomActorRejectsWrongRoomAndInvalidConfiguration(t *testing.T) {
	t.Parallel()

	executor := &actorImmediateExecutor{}
	actor := mustRoomActor(t, "room-1", executor, func(domain.RoomID) {})
	defer closeRoomActor(t, actor)
	if _, err := actor.Execute(context.Background(), actorTestUser, actorCommand("cmd-wrong", "room-2")); !errors.Is(err, ErrRoomActorMismatch) {
		t.Fatalf("wrong-room Execute() error = %v, want ErrRoomActorMismatch", err)
	}
	if got := executor.calls.Load(); got != 0 {
		t.Fatalf("executor calls = %d, want 0", got)
	}

	if actor, err := NewRoomActor("", executor, func(domain.RoomID) {}); !errors.Is(err, ErrInvalidConfiguration) || actor != nil {
		t.Fatalf("NewRoomActor(empty room) = %v, %v", actor, err)
	}
	if actor, err := NewRoomActor("room-1", nil, func(domain.RoomID) {}); !errors.Is(err, ErrInvalidConfiguration) || actor != nil {
		t.Fatalf("NewRoomActor(nil executor) = %v, %v", actor, err)
	}
	var typedNilExecutor *actorImmediateExecutor
	if actor, err := NewRoomActor("room-1", typedNilExecutor, func(domain.RoomID) {}); !errors.Is(err, ErrInvalidConfiguration) || actor != nil {
		t.Fatalf("NewRoomActor(typed nil executor) = %v, %v", actor, err)
	}
	if actor, err := NewRoomActor("room-1", executor, nil); !errors.Is(err, ErrInvalidConfiguration) || actor != nil {
		t.Fatalf("NewRoomActor(nil cleanup) = %v, %v", actor, err)
	}
}

func mustRoomActor(t *testing.T, roomID domain.RoomID, executor Executor, cleanup RoomActorCleanup) *RoomActor {
	t.Helper()
	actor, err := NewRoomActor(roomID, executor, cleanup)
	if err != nil {
		t.Fatalf("NewRoomActor() error = %v", err)
	}
	return actor
}

func closeRoomActor(t *testing.T, actor *RoomActor) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := actor.Close(ctx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func actorCommand(commandID string, roomID domain.RoomID) protocol.ClientCommand {
	return protocol.ClientCommand{
		Version: protocol.Version1, Direction: protocol.DirectionClientCommand,
		Type: protocol.CommandSendChat, CommandID: commandID, RoomID: roomID,
		Payload: protocol.SendChatPayload{Text: commandID},
	}
}

func executeRoomCommand(actor *RoomActor, command protocol.ClientCommand) <-chan actorExecutionResult {
	result := make(chan actorExecutionResult, 1)
	go func() {
		outcome, err := actor.Execute(context.Background(), actorTestUser, command)
		result <- actorExecutionResult{outcome: outcome, err: err}
	}()
	return result
}

func executeRoomOperation(actor *RoomActor, ctx context.Context, operation RoomActorOperation) <-chan error {
	result := make(chan error, 1)
	go func() {
		result <- actor.ExecuteInternal(ctx, operation)
	}()
	return result
}

func assertActorExecutionSucceeded(t *testing.T, result actorExecutionResult) {
	t.Helper()
	if result.err != nil || result.outcome.Status != protocol.CommandAccepted {
		t.Fatalf("actor execution = %+v, error = %v", result.outcome, result.err)
	}
}

func assertActorExecutionError(t *testing.T, result actorExecutionResult, want error) {
	t.Helper()
	if !errors.Is(result.err, want) {
		t.Fatalf("actor execution error = %v, want %v", result.err, want)
	}
}

func receiveActorExecutionResult(t *testing.T, results <-chan actorExecutionResult) actorExecutionResult {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for actor execution result")
		return actorExecutionResult{}
	}
}

func receiveSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for signal")
	}
}

func assertNoSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
		t.Fatal("unexpected signal")
	case <-time.After(20 * time.Millisecond):
	}
}

func receiveString(t *testing.T, values <-chan string) string {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for value")
		return ""
	}
}

func assertNoString(t *testing.T, values <-chan string) {
	t.Helper()
	select {
	case value := <-values:
		t.Fatalf("unexpected value %q", value)
	case <-time.After(20 * time.Millisecond):
	}
}

func receiveError(t *testing.T, errors <-chan error) error {
	t.Helper()
	select {
	case err := <-errors:
		return err
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for error result")
		return nil
	}
}

func assertNoError(t *testing.T, errors <-chan error) {
	t.Helper()
	select {
	case err := <-errors:
		t.Fatalf("unexpected early result: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
}

func assertNoActorExecutionResult(t *testing.T, results <-chan actorExecutionResult) {
	t.Helper()
	select {
	case result := <-results:
		t.Fatalf("unexpected early execution result: %+v", result)
	case <-time.After(20 * time.Millisecond):
	}
}

type actorExecutionResult struct {
	outcome protocol.CommandOutcome
	err     error
}

type actorSerialExecutor struct {
	started   chan string
	release   chan struct{}
	calls     atomic.Int32
	active    atomic.Int32
	maxActive atomic.Int32
	mu        sync.Mutex
	completed []string
}

func (executor *actorSerialExecutor) Execute(_ context.Context, _ auth.User, command protocol.ClientCommand) (protocol.CommandOutcome, error) {
	call := executor.calls.Add(1)
	active := executor.active.Add(1)
	for {
		maximum := executor.maxActive.Load()
		if active <= maximum || executor.maxActive.CompareAndSwap(maximum, active) {
			break
		}
	}
	executor.started <- command.CommandID
	if call == 1 {
		<-executor.release
	}
	executor.mu.Lock()
	executor.completed = append(executor.completed, command.CommandID)
	executor.mu.Unlock()
	executor.active.Add(-1)
	return protocol.CommandOutcome{Status: protocol.CommandAccepted}, nil
}

type actorBlockingExecutor struct {
	started chan struct{}
	release chan struct{}
	events  chan string
	calls   atomic.Int32
}

func (executor *actorBlockingExecutor) Execute(_ context.Context, _ auth.User, _ protocol.ClientCommand) (protocol.CommandOutcome, error) {
	executor.calls.Add(1)
	close(executor.started)
	<-executor.release
	executor.events <- "execution-complete"
	return protocol.CommandOutcome{Status: protocol.CommandAccepted}, nil
}

type actorCancellationExecutor struct {
	started   chan struct{}
	release   chan struct{}
	cancelled chan struct{}
	values    chan string
}

func (executor *actorCancellationExecutor) Execute(ctx context.Context, _ auth.User, _ protocol.ClientCommand) (protocol.CommandOutcome, error) {
	close(executor.started)
	if executor.values != nil {
		value, _ := ctx.Value(actorContextKey("trace_id")).(string)
		executor.values <- value
	}
	select {
	case <-ctx.Done():
		executor.cancelled <- struct{}{}
		return protocol.CommandOutcome{}, ctx.Err()
	case <-executor.release:
		return protocol.CommandOutcome{Status: protocol.CommandAccepted}, nil
	}
}

type actorContextKey string

type actorPanicExecutor struct{}

func (actorPanicExecutor) Execute(context.Context, auth.User, protocol.ClientCommand) (protocol.CommandOutcome, error) {
	panic("executor failed")
}

type actorImmediateExecutor struct {
	calls atomic.Int32
}

func (executor *actorImmediateExecutor) Execute(_ context.Context, _ auth.User, _ protocol.ClientCommand) (protocol.CommandOutcome, error) {
	executor.calls.Add(1)
	return protocol.CommandOutcome{Status: protocol.CommandAccepted}, nil
}

var actorTestUser = auth.User{ID: "usr_EREREREREREREREREREREQ"}
