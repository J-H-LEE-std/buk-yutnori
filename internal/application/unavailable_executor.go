package application

import (
	"context"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/protocol"
)

const applicationUnavailableCode = "APPLICATION_UNAVAILABLE"

// UnavailableExecutor is the temporary Milestone 2 runtime boundary used until
// room and match executors are connected. It never applies authoritative
// state and returns a transient rejection that is safe to retry and does not
// accumulate room-lifetime idempotency records for nonexistent rooms.
type UnavailableExecutor struct{}

// Execute implements Executor.
func (UnavailableExecutor) Execute(ctx context.Context, _ auth.User, _ protocol.ClientCommand) (protocol.CommandOutcome, error) {
	if err := ctx.Err(); err != nil {
		return protocol.CommandOutcome{}, err
	}
	return protocol.CommandOutcome{
		Status: protocol.CommandRejected,
		Error: &protocol.CommandError{
			Code:      applicationUnavailableCode,
			Message:   "방 및 경기 서비스가 아직 준비되지 않았습니다.",
			Retriable: true,
		},
	}, nil
}
