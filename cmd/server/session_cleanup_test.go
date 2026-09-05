package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestDeleteAllExpiredSessionsDrainsBoundedBatches(t *testing.T) {
	t.Parallel()

	cleaner := &scriptedSessionCleaner{results: []cleanupResult{{deleted: 2}, {deleted: 2}, {deleted: 1}}}
	cutoff := time.Date(2026, time.September, 5, 12, 0, 0, 0, time.UTC)
	deleted, err := deleteAllExpiredSessions(context.Background(), cleaner, cutoff, 2)
	if err != nil || deleted != 5 {
		t.Fatalf("deleteAllExpiredSessions() = %d, %v; want 5, nil", deleted, err)
	}
	if calls := cleaner.snapshotCalls(); len(calls) != 3 {
		t.Fatalf("cleanup calls = %d, want 3", len(calls))
	} else {
		for _, call := range calls {
			if call.cutoff != cutoff || call.limit != 2 {
				t.Fatalf("cleanup call = %+v, want cutoff %v limit 2", call, cutoff)
			}
		}
	}
}

func TestRunExpiredSessionCleanupRunsImmediatelyRetriesAndStops(t *testing.T) {
	t.Parallel()

	transient := errors.New("temporary sqlite busy")
	cleaner := &scriptedSessionCleaner{
		results: []cleanupResult{{err: transient}, {deleted: 0}},
		called:  make(chan struct{}, 2),
	}
	ctx, cancel := context.WithCancel(context.Background())
	ticks := make(chan time.Time)
	errorsSeen := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		runExpiredSessionCleanup(ctx, cleaner, func() time.Time {
			return time.Date(2026, time.September, 5, 12, 0, 0, 0, time.UTC)
		}, ticks, 10, func(err error) { errorsSeen <- err })
		close(done)
	}()

	waitForCleanupCall(t, cleaner.called)
	select {
	case err := <-errorsSeen:
		if !errors.Is(err, transient) {
			t.Fatalf("reported error = %v, want transient error", err)
		}
	case <-time.After(time.Second):
		t.Fatal("initial cleanup error was not reported")
	}
	ticks <- time.Now()
	waitForCleanupCall(t, cleaner.called)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cleanup worker did not stop after cancellation")
	}
	if calls := cleaner.snapshotCalls(); len(calls) != 2 {
		t.Fatalf("cleanup calls = %d, want initial and retry", len(calls))
	}
}

func TestStopExpiredSessionCleanupCancelsStopsAndJoinsWorker(t *testing.T) {
	t.Parallel()

	cancelCalled := make(chan struct{})
	ticker := time.NewTicker(time.Hour)
	workerDone := make(chan struct{})
	stopReturned := make(chan struct{})
	go func() {
		stopExpiredSessionCleanup(func() { close(cancelCalled) }, ticker, workerDone)
		close(stopReturned)
	}()

	<-cancelCalled
	select {
	case <-stopReturned:
		t.Fatal("cleanup stop returned before worker joined")
	default:
	}
	close(workerDone)
	select {
	case <-stopReturned:
	case <-time.After(time.Second):
		t.Fatal("cleanup stop did not return after worker finished")
	}
}

type cleanupResult struct {
	deleted int
	err     error
}

type cleanupCall struct {
	cutoff time.Time
	limit  int
}

type scriptedSessionCleaner struct {
	mu      sync.Mutex
	results []cleanupResult
	calls   []cleanupCall
	called  chan struct{}
}

func (cleaner *scriptedSessionCleaner) DeleteExpiredSessions(_ context.Context, cutoff time.Time, limit int) (int, error) {
	cleaner.mu.Lock()
	defer cleaner.mu.Unlock()
	cleaner.calls = append(cleaner.calls, cleanupCall{cutoff: cutoff, limit: limit})
	result := cleanupResult{}
	if len(cleaner.results) > 0 {
		result = cleaner.results[0]
		cleaner.results = cleaner.results[1:]
	}
	if cleaner.called != nil {
		cleaner.called <- struct{}{}
	}
	return result.deleted, result.err
}

func (cleaner *scriptedSessionCleaner) snapshotCalls() []cleanupCall {
	cleaner.mu.Lock()
	defer cleaner.mu.Unlock()
	return append([]cleanupCall(nil), cleaner.calls...)
}

func waitForCleanupCall(t *testing.T, called <-chan struct{}) {
	t.Helper()
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("cleanup call timed out")
	}
}
