package application

import (
	"context"
	"errors"
	"testing"

	"buk-yutnori/internal/protocol"
)

func TestUnavailableExecutorReturnsTransientRetriableRejection(t *testing.T) {
	t.Parallel()

	outcome, err := (UnavailableExecutor{}).Execute(context.Background(), testUserA, testCommand("cmd-1", "hello"))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if outcome.Status != protocol.CommandRejected || outcome.Error == nil || outcome.Error.Code != applicationUnavailableCode || !outcome.Error.Retriable {
		t.Fatalf("outcome = %+v", outcome)
	}
}

func TestUnavailableExecutorHonorsCanceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (UnavailableExecutor{}).Execute(ctx, testUserA, testCommand("cmd-1", "hello")); !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute() error = %v, want context.Canceled", err)
	}
}

func TestUnavailableExecutorDoesNotAccumulateIdempotencyRecords(t *testing.T) {
	t.Parallel()

	processor := mustProcessor(t, UnavailableExecutor{})
	command := testCommand("cmd-1", "hello")
	for range 2 {
		result, err := processor.Process(context.Background(), testUserA, command)
		if err != nil {
			t.Fatalf("Process() error = %v", err)
		}
		if result.Payload.Error == nil || !result.Payload.Error.Retriable {
			t.Fatalf("result = %+v", result)
		}
	}
	processor.mu.Lock()
	retained := len(processor.entries)
	processor.mu.Unlock()
	if retained != 0 {
		t.Fatalf("retained unavailable results = %d, want 0", retained)
	}
}
