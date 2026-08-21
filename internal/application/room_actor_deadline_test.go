package application

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"buk-yutnori/internal/domain"
)

func TestRoomActorDeadlineWaitsForTimerAndSubmitsOperation(t *testing.T) {
	t.Parallel()

	actor := mustRoomActor(t, "room-1", &actorImmediateExecutor{}, func(domain.RoomID) {})
	defer closeRoomActor(t, actor)
	now := time.Date(2026, time.August, 22, 12, 0, 0, 0, time.UTC)
	clock := newDeadlineTestClock(now)
	var calls atomic.Int32

	deadline, err := scheduleRoomActorDeadline(actor, now.Add(10*time.Second), func(context.Context) error {
		calls.Add(1)
		return nil
	}, clock)
	if err != nil {
		t.Fatalf("scheduleRoomActorDeadline() error = %v", err)
	}
	if got := clock.duration(); got != 10*time.Second {
		t.Fatalf("timer duration = %v, want 10s", got)
	}
	waiting := waitRoomActorDeadline(deadline, context.Background())
	assertNoError(t, waiting)
	if got := calls.Load(); got != 0 {
		t.Fatalf("operation calls before timer = %d, want 0", got)
	}

	clock.fire()
	if err := receiveError(t, waiting); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("operation calls = %d, want 1", got)
	}
	if got := clock.stopCalls(); got != 1 {
		t.Fatalf("timer Stop() calls = %d, want 1", got)
	}
}

func TestRoomActorDeadlineClampsPastDeadlineToImmediateTimer(t *testing.T) {
	t.Parallel()

	actor := mustRoomActor(t, "room-1", &actorImmediateExecutor{}, func(domain.RoomID) {})
	defer closeRoomActor(t, actor)
	now := time.Date(2026, time.August, 22, 12, 0, 0, 0, time.UTC)
	clock := newDeadlineTestClock(now)

	deadline, err := scheduleRoomActorDeadline(actor, now.Add(-time.Second), func(context.Context) error {
		return nil
	}, clock)
	if err != nil {
		t.Fatalf("scheduleRoomActorDeadline() error = %v", err)
	}
	if got := clock.duration(); got != 0 {
		t.Fatalf("past-deadline timer duration = %v, want 0", got)
	}
	clock.fire()
	if err := deadline.Wait(context.Background()); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
}

func TestScheduleRoomActorDeadlineRunsPastDeadlineImmediately(t *testing.T) {
	t.Parallel()

	actor := mustRoomActor(t, "room-1", &actorImmediateExecutor{}, func(domain.RoomID) {})
	defer closeRoomActor(t, actor)
	var calls atomic.Int32
	deadline, err := ScheduleRoomActorDeadline(actor, time.Now().Add(-time.Second), func(context.Context) error {
		calls.Add(1)
		return nil
	})
	if err != nil {
		t.Fatalf("ScheduleRoomActorDeadline() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := deadline.Wait(ctx); err != nil {
		t.Fatalf("Wait() immediate deadline error = %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("immediate operation calls = %d, want 1", got)
	}
}

func TestRoomActorDeadlineCancellationBeforeTimerPreventsOperation(t *testing.T) {
	t.Parallel()

	actor := mustRoomActor(t, "room-1", &actorImmediateExecutor{}, func(domain.RoomID) {})
	defer closeRoomActor(t, actor)
	clock := newDeadlineTestClock(time.Now())
	var calls atomic.Int32
	deadline, err := scheduleRoomActorDeadline(actor, clock.now.Add(time.Minute), func(context.Context) error {
		calls.Add(1)
		return nil
	}, clock)
	if err != nil {
		t.Fatalf("scheduleRoomActorDeadline() error = %v", err)
	}

	deadline.Cancel()
	deadline.Cancel()
	if err := deadline.Wait(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait() after Cancel() error = %v, want context.Canceled", err)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("operation calls = %d, want 0", got)
	}
	if got := clock.stopCalls(); got != 1 {
		t.Fatalf("timer Stop() calls = %d, want 1", got)
	}
}

func TestRoomActorDeadlineWaitCancellationDoesNotCancelDeadline(t *testing.T) {
	t.Parallel()

	actor := mustRoomActor(t, "room-1", &actorImmediateExecutor{}, func(domain.RoomID) {})
	defer closeRoomActor(t, actor)
	clock := newDeadlineTestClock(time.Now())
	var calls atomic.Int32
	deadline, err := scheduleRoomActorDeadline(actor, clock.now, func(context.Context) error {
		calls.Add(1)
		return nil
	}, clock)
	if err != nil {
		t.Fatalf("scheduleRoomActorDeadline() error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := deadline.Wait(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait(canceled) error = %v, want context.Canceled", err)
	}

	clock.fire()
	if err := deadline.Wait(context.Background()); err != nil {
		t.Fatalf("Wait() after waiter cancellation error = %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("operation calls = %d, want 1", got)
	}
}

func TestRoomActorDeadlineWakesConcurrentWaitersWithSameResult(t *testing.T) {
	t.Parallel()

	actor := mustRoomActor(t, "room-1", &actorImmediateExecutor{}, func(domain.RoomID) {})
	defer closeRoomActor(t, actor)
	clock := newDeadlineTestClock(time.Now())
	want := errors.New("shared deadline result")
	deadline, err := scheduleRoomActorDeadline(actor, clock.now, func(context.Context) error {
		return want
	}, clock)
	if err != nil {
		t.Fatalf("scheduleRoomActorDeadline() error = %v", err)
	}

	const waiterCount = 16
	waiters := make([]<-chan error, 0, waiterCount)
	for range waiterCount {
		waiters = append(waiters, waitRoomActorDeadline(deadline, context.Background()))
	}
	clock.fire()
	for index, waiter := range waiters {
		if err := receiveError(t, waiter); !errors.Is(err, want) {
			t.Fatalf("waiter %d error = %v, want %v", index, err, want)
		}
	}
}

func TestRoomActorDeadlineCancellationWhileWaitingForAdmissionPreventsOperation(t *testing.T) {
	t.Parallel()

	executor := &actorBlockingExecutor{
		started: make(chan struct{}),
		release: make(chan struct{}),
		events:  make(chan string, 1),
	}
	actor := mustRoomActor(t, "room-1", executor, func(domain.RoomID) {})
	defer closeRoomActor(t, actor)
	command := executeRoomCommand(actor, actorCommand("cmd-blocking", "room-1"))
	receiveSignal(t, executor.started)
	clock := newDeadlineTestClock(time.Now())
	var calls atomic.Int32
	deadline, err := scheduleRoomActorDeadline(actor, clock.now, func(context.Context) error {
		calls.Add(1)
		return nil
	}, clock)
	if err != nil {
		t.Fatalf("scheduleRoomActorDeadline() error = %v", err)
	}
	clock.fire()
	assertNoDeadlineResult(t, deadline)

	deadline.Cancel()
	if err := deadline.Wait(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait() after admission cancellation error = %v, want context.Canceled", err)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("operation calls = %d, want 0", got)
	}
	close(executor.release)
	assertActorExecutionSucceeded(t, <-command)
}

func TestRoomActorDeadlineOwnsOperationAfterActorAdmission(t *testing.T) {
	t.Parallel()

	actor := mustRoomActor(t, "room-1", &actorImmediateExecutor{}, func(domain.RoomID) {})
	defer closeRoomActor(t, actor)
	clock := newDeadlineTestClock(time.Now())
	started := make(chan struct{})
	release := make(chan struct{})
	cancelled := make(chan struct{}, 1)
	deadline, err := scheduleRoomActorDeadline(actor, clock.now, func(ctx context.Context) error {
		close(started)
		select {
		case <-ctx.Done():
			cancelled <- struct{}{}
			return ctx.Err()
		case <-release:
			return nil
		}
	}, clock)
	if err != nil {
		t.Fatalf("scheduleRoomActorDeadline() error = %v", err)
	}
	clock.fire()
	receiveSignal(t, started)

	deadline.Cancel()
	assertNoSignal(t, cancelled)
	assertNoDeadlineResult(t, deadline)
	close(release)
	if err := deadline.Wait(context.Background()); err != nil {
		t.Fatalf("Wait() after accepted operation cancellation error = %v", err)
	}
}

func TestRoomActorDeadlineReportsClosedActorAndOperationErrors(t *testing.T) {
	t.Parallel()

	t.Run("closed actor", func(t *testing.T) {
		actor := mustRoomActor(t, "room-closed", &actorImmediateExecutor{}, func(domain.RoomID) {})
		clock := newDeadlineTestClock(time.Now())
		deadline, err := scheduleRoomActorDeadline(actor, clock.now.Add(time.Minute), func(context.Context) error { return nil }, clock)
		if err != nil {
			t.Fatalf("scheduleRoomActorDeadline() error = %v", err)
		}
		closeRoomActor(t, actor)
		if err := deadline.Wait(context.Background()); !errors.Is(err, ErrRoomActorClosed) {
			t.Fatalf("Wait() closed actor error = %v, want ErrRoomActorClosed", err)
		}
		if got := clock.stopCalls(); got != 1 {
			t.Fatalf("closed actor timer Stop() calls = %d, want 1", got)
		}
	})

	t.Run("ordinary operation error", func(t *testing.T) {
		actor := mustRoomActor(t, "room-error", &actorImmediateExecutor{}, func(domain.RoomID) {})
		defer closeRoomActor(t, actor)
		clock := newDeadlineTestClock(time.Now())
		want := errors.New("deadline transition rejected")
		deadline, err := scheduleRoomActorDeadline(actor, clock.now, func(context.Context) error { return want }, clock)
		if err != nil {
			t.Fatalf("scheduleRoomActorDeadline() error = %v", err)
		}
		clock.fire()
		if err := deadline.Wait(context.Background()); !errors.Is(err, want) {
			t.Fatalf("Wait() operation error = %v, want %v", err, want)
		}
		if err := deadline.Wait(context.Background()); !errors.Is(err, want) {
			t.Fatalf("second Wait() operation error = %v, want %v", err, want)
		}
		assertActorExecutionSucceeded(t, receiveActorExecutionResult(t, executeRoomCommand(actor, actorCommand("cmd-after-error", "room-error"))))
	})
}

func TestRoomActorDeadlineReportsOperationPanicAsActorTerminalFailure(t *testing.T) {
	t.Parallel()

	var cleanupCalls atomic.Int32
	actor := mustRoomActor(t, "room-1", &actorImmediateExecutor{}, func(domain.RoomID) {
		cleanupCalls.Add(1)
	})
	clock := newDeadlineTestClock(time.Now())
	deadline, err := scheduleRoomActorDeadline(actor, clock.now, func(context.Context) error {
		panic("deadline operation failed")
	}, clock)
	if err != nil {
		t.Fatalf("scheduleRoomActorDeadline() error = %v", err)
	}
	clock.fire()
	if err := deadline.Wait(context.Background()); !errors.Is(err, ErrRoomActorPanicked) {
		t.Fatalf("Wait() panic error = %v, want ErrRoomActorPanicked", err)
	}
	if err := actor.Close(context.Background()); !errors.Is(err, ErrRoomActorPanicked) {
		t.Fatalf("Close() panic error = %v, want ErrRoomActorPanicked", err)
	}
	if got := cleanupCalls.Load(); got != 1 {
		t.Fatalf("cleanup calls = %d, want 1", got)
	}
}

func TestRoomActorDeadlineRejectsInvalidConfigurationAndWaitContext(t *testing.T) {
	t.Parallel()

	clock := newDeadlineTestClock(time.Now())
	operation := func(context.Context) error { return nil }
	if deadline, err := scheduleRoomActorDeadline(nil, clock.now, operation, clock); !errors.Is(err, ErrInvalidConfiguration) || deadline != nil {
		t.Fatalf("scheduleRoomActorDeadline(nil actor) = %v, %v", deadline, err)
	}
	actor := mustRoomActor(t, "room-1", &actorImmediateExecutor{}, func(domain.RoomID) {})
	defer closeRoomActor(t, actor)
	if deadline, err := scheduleRoomActorDeadline(actor, clock.now, nil, clock); !errors.Is(err, ErrInvalidConfiguration) || deadline != nil {
		t.Fatalf("scheduleRoomActorDeadline(nil operation) = %v, %v", deadline, err)
	}
	var nilClock *deadlineTestClock
	if deadline, err := scheduleRoomActorDeadline(actor, clock.now, operation, nilClock); !errors.Is(err, ErrInvalidConfiguration) || deadline != nil {
		t.Fatalf("scheduleRoomActorDeadline(nil clock) = %v, %v", deadline, err)
	}
	nilTimerClock := &deadlineTestClock{now: clock.now}
	if deadline, err := scheduleRoomActorDeadline(actor, clock.now, operation, nilTimerClock); !errors.Is(err, ErrInvalidConfiguration) || deadline != nil {
		t.Fatalf("scheduleRoomActorDeadline(nil timer) = %v, %v", deadline, err)
	}
	nilChannelTimer := &deadlineTestTimer{}
	nilChannelClock := &deadlineTestClock{now: clock.now, timer: nilChannelTimer}
	if deadline, err := scheduleRoomActorDeadline(actor, clock.now, operation, nilChannelClock); !errors.Is(err, ErrInvalidConfiguration) || deadline != nil {
		t.Fatalf("scheduleRoomActorDeadline(nil timer channel) = %v, %v", deadline, err)
	}
	if got := nilChannelTimer.stops.Load(); got != 1 {
		t.Fatalf("nil-channel timer Stop() calls = %d, want 1", got)
	}
	var nilDeadline *RoomActorDeadline
	if err := nilDeadline.Wait(context.Background()); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("nil deadline Wait() error = %v, want ErrInvalidConfiguration", err)
	}
	deadline, err := scheduleRoomActorDeadline(actor, clock.now, operation, clock)
	if err != nil {
		t.Fatalf("scheduleRoomActorDeadline() error = %v", err)
	}
	if err := deadline.Wait(nil); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("Wait(nil) error = %v, want ErrInvalidConfiguration", err)
	}
	deadline.Cancel()
}

func waitRoomActorDeadline(deadline *RoomActorDeadline, ctx context.Context) <-chan error {
	result := make(chan error, 1)
	go func() {
		result <- deadline.Wait(ctx)
	}()
	return result
}

func assertNoDeadlineResult(t *testing.T, deadline *RoomActorDeadline) {
	t.Helper()
	assertNoError(t, waitRoomActorDeadline(deadline, context.Background()))
}

type deadlineTestClock struct {
	now           time.Time
	timer         *deadlineTestTimer
	durationNanos atomic.Int64
}

func newDeadlineTestClock(now time.Time) *deadlineTestClock {
	return &deadlineTestClock{
		now:   now,
		timer: &deadlineTestTimer{fired: make(chan time.Time, 1)},
	}
}

func (clock *deadlineTestClock) Now() time.Time {
	return clock.now
}

func (clock *deadlineTestClock) NewTimer(duration time.Duration) roomActorDeadlineTimer {
	clock.durationNanos.Store(int64(duration))
	return clock.timer
}

func (clock *deadlineTestClock) duration() time.Duration {
	return time.Duration(clock.durationNanos.Load())
}

func (clock *deadlineTestClock) fire() {
	clock.timer.fired <- clock.now.Add(clock.duration())
}

func (clock *deadlineTestClock) stopCalls() int32 {
	return clock.timer.stops.Load()
}

type deadlineTestTimer struct {
	fired chan time.Time
	stops atomic.Int32
}

func (timer *deadlineTestTimer) C() <-chan time.Time {
	return timer.fired
}

func (timer *deadlineTestTimer) Stop() bool {
	timer.stops.Add(1)
	return true
}
