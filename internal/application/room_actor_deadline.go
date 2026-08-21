package application

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// RoomActorDeadline is one cancellable, server-owned deadline submission.
// Cancel prevents an operation that has not reached actor admission. Once the
// actor accepts the operation, RoomActor owns it through completion.
type RoomActorDeadline struct {
	cancel context.CancelFunc
	done   chan struct{}

	mu  sync.RWMutex
	err error
}

type roomActorDeadlineClock interface {
	Now() time.Time
	NewTimer(time.Duration) roomActorDeadlineTimer
}

type roomActorDeadlineTimer interface {
	C() <-chan time.Time
	Stop() bool
}

type systemRoomActorDeadlineClock struct{}

type systemRoomActorDeadlineTimer struct {
	timer *time.Timer
}

// ScheduleRoomActorDeadline schedules operation for deadlineAt using the
// process monotonic clock embedded in time.Time values created by time.Now.
func ScheduleRoomActorDeadline(
	actor *RoomActor,
	deadlineAt time.Time,
	operation RoomActorOperation,
) (*RoomActorDeadline, error) {
	return scheduleRoomActorDeadline(actor, deadlineAt, operation, systemRoomActorDeadlineClock{})
}

func scheduleRoomActorDeadline(
	actor *RoomActor,
	deadlineAt time.Time,
	operation RoomActorOperation,
	clock roomActorDeadlineClock,
) (*RoomActorDeadline, error) {
	if err := actor.validateSubmission(context.Background()); err != nil {
		return nil, err
	}
	if operation == nil {
		return nil, fmt.Errorf("%w: room deadline operation is required", ErrInvalidConfiguration)
	}
	if isNilInterface(clock) {
		return nil, fmt.Errorf("%w: room deadline clock is required", ErrInvalidConfiguration)
	}

	duration := deadlineAt.Sub(clock.Now())
	if duration < 0 {
		duration = 0
	}
	timer := clock.NewTimer(duration)
	if isNilInterface(timer) {
		return nil, fmt.Errorf("%w: room deadline timer is required", ErrInvalidConfiguration)
	}
	if timer.C() == nil {
		timer.Stop()
		return nil, fmt.Errorf("%w: room deadline timer channel is required", ErrInvalidConfiguration)
	}

	submissionCtx, cancel := context.WithCancel(context.Background())
	deadline := &RoomActorDeadline{
		cancel: cancel,
		done:   make(chan struct{}),
	}
	go deadline.run(submissionCtx, actor, operation, timer)
	return deadline, nil
}

// Cancel stops this deadline before actor admission when possible. It is
// idempotent and does not interrupt an operation already accepted by the
// actor. A cancellation that wins before admission completes Wait with
// context.Canceled.
func (deadline *RoomActorDeadline) Cancel() {
	if deadline != nil && deadline.cancel != nil {
		deadline.cancel()
	}
}

// Wait observes the deadline's terminal result. Canceling the waiting context
// does not cancel the deadline; callers use Cancel explicitly for ownership.
func (deadline *RoomActorDeadline) Wait(ctx context.Context) error {
	if deadline == nil || deadline.done == nil || ctx == nil {
		return fmt.Errorf("%w: room actor deadline is required", ErrInvalidConfiguration)
	}

	select {
	case <-deadline.done:
		return deadline.result()
	default:
	}
	select {
	case <-deadline.done:
		return deadline.result()
	case <-ctx.Done():
		select {
		case <-deadline.done:
			return deadline.result()
		default:
			return ctx.Err()
		}
	}
}

func (deadline *RoomActorDeadline) run(
	ctx context.Context,
	actor *RoomActor,
	operation RoomActorOperation,
	timer roomActorDeadlineTimer,
) {
	var err error
	select {
	case <-timer.C():
		err = actor.ExecuteInternal(ctx, operation)
	case <-actor.stopping:
		err = ErrRoomActorClosed
	case <-ctx.Done():
		err = ctx.Err()
	}
	deadline.cancel()
	timer.Stop()
	deadline.finish(err)
}

func (deadline *RoomActorDeadline) finish(err error) {
	deadline.mu.Lock()
	deadline.err = err
	deadline.mu.Unlock()
	close(deadline.done)
}

func (deadline *RoomActorDeadline) result() error {
	deadline.mu.RLock()
	defer deadline.mu.RUnlock()
	return deadline.err
}

func (systemRoomActorDeadlineClock) Now() time.Time {
	return time.Now()
}

func (systemRoomActorDeadlineClock) NewTimer(duration time.Duration) roomActorDeadlineTimer {
	return systemRoomActorDeadlineTimer{timer: time.NewTimer(duration)}
}

func (timer systemRoomActorDeadlineTimer) C() <-chan time.Time {
	return timer.timer.C
}

func (timer systemRoomActorDeadlineTimer) Stop() bool {
	return timer.timer.Stop()
}
