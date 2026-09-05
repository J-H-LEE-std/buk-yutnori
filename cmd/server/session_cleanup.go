package main

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	sessionCleanupBatchSize = 256
	sessionCleanupInterval  = time.Hour
)

type expiredSessionCleaner interface {
	DeleteExpiredSessions(ctx context.Context, cutoff time.Time, limit int) (int, error)
}

func deleteAllExpiredSessions(ctx context.Context, cleaner expiredSessionCleaner, cutoff time.Time, batchSize int) (int, error) {
	if cleaner == nil || cutoff.IsZero() || batchSize <= 0 {
		return 0, errors.New("invalid expired session cleanup configuration")
	}
	total := 0
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		deleted, err := cleaner.DeleteExpiredSessions(ctx, cutoff, batchSize)
		if err != nil {
			return total, err
		}
		if deleted < 0 || deleted > batchSize {
			return total, fmt.Errorf("expired session cleaner returned invalid batch count %d", deleted)
		}
		total += deleted
		if deleted < batchSize {
			return total, nil
		}
	}
}

func runExpiredSessionCleanup(
	ctx context.Context,
	cleaner expiredSessionCleaner,
	now func() time.Time,
	ticks <-chan time.Time,
	batchSize int,
	reportError func(error),
) {
	cleanup := func() {
		if _, err := deleteAllExpiredSessions(ctx, cleaner, now().UTC(), batchSize); err != nil && ctx.Err() == nil && reportError != nil {
			reportError(err)
		}
	}
	cleanup()
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-ticks:
			if !ok {
				return
			}
			cleanup()
		}
	}
}
