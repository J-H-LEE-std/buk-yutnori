package turn

import (
	"errors"
	"fmt"
	"sync"

	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/room"
)

var (
	// ErrDuplicateResultTokenID identifies an ID already seen by this queue.
	ErrDuplicateResultTokenID = errors.New("duplicate result token ID")

	// ErrResultTokenNotAvailable identifies a queued token behind an ordering rule.
	ErrResultTokenNotAvailable = errors.New("result token is not available")

	// ErrResultTokenNotFound identifies an ID never seen by this queue.
	ErrResultTokenNotFound = errors.New("result token not found")

	// ErrResultTokenAlreadyConsumed identifies a previously consumed token ID.
	ErrResultTokenAlreadyConsumed = errors.New("result token already consumed")

	// ErrInvalidMovementOrder identifies a non-canonical queue ordering mode.
	ErrInvalidMovementOrder = errors.New("invalid movement order")
)

// ResultQueue stores unresolved result tokens in generation order.
//
// IDs remain reserved after consumption, preventing one token from being
// appended or consumed twice during the queue's lifetime. Queue methods are
// safe for concurrent callers.
type ResultQueue struct {
	mutex   sync.RWMutex
	tokens  []ResultToken
	seenIDs map[domain.ResultTokenID]struct{}
}

// NewResultQueue validates and appends initial tokens in order.
func NewResultQueue(tokens ...ResultToken) (*ResultQueue, error) {
	queue := &ResultQueue{}
	for _, token := range tokens {
		if err := queue.Append(token); err != nil {
			return nil, err
		}
	}
	return queue, nil
}

// Append validates token and adds it to the queue tail.
func (queue *ResultQueue) Append(token ResultToken) error {
	if err := token.Validate(); err != nil {
		return err
	}

	queue.mutex.Lock()
	defer queue.mutex.Unlock()
	if queue.seenIDs == nil {
		queue.seenIDs = make(map[domain.ResultTokenID]struct{})
	}
	if _, exists := queue.seenIDs[token.ID]; exists {
		return fmt.Errorf("%w: %q", ErrDuplicateResultTokenID, token.ID)
	}
	queue.tokens = append(queue.tokens, token)
	queue.seenIDs[token.ID] = struct{}{}
	return nil
}

// Len returns the number of unresolved tokens.
func (queue *ResultQueue) Len() int {
	queue.mutex.RLock()
	defer queue.mutex.RUnlock()
	return len(queue.tokens)
}

// Snapshot returns an independent copy of all unresolved tokens.
func (queue *ResultQueue) Snapshot() []ResultToken {
	queue.mutex.RLock()
	defer queue.mutex.RUnlock()
	return append([]ResultToken(nil), queue.tokens...)
}

// Available returns the tokens eligible for the next resolution step.
//
// FIFO exposes only the head. Free order exposes every non-Buk token before
// the first Buk. When Buk is at the head, only that Buk is exposed.
func (queue *ResultQueue) Available(order room.MovementOrder) ([]ResultToken, error) {
	if err := validateMovementOrder(order); err != nil {
		return nil, err
	}

	queue.mutex.RLock()
	defer queue.mutex.RUnlock()
	end := availableEnd(queue.tokens, order)
	return append([]ResultToken(nil), queue.tokens[:end]...), nil
}

// Consume removes and returns one currently available token.
func (queue *ResultQueue) Consume(id domain.ResultTokenID, order room.MovementOrder) (ResultToken, error) {
	if err := validateMovementOrder(order); err != nil {
		return ResultToken{}, err
	}

	queue.mutex.Lock()
	defer queue.mutex.Unlock()
	index := queue.indexOf(id)
	if index < 0 {
		if _, seen := queue.seenIDs[id]; seen {
			return ResultToken{}, fmt.Errorf("%w: %q", ErrResultTokenAlreadyConsumed, id)
		}
		return ResultToken{}, fmt.Errorf("%w: %q", ErrResultTokenNotFound, id)
	}
	if index >= availableEnd(queue.tokens, order) {
		return ResultToken{}, fmt.Errorf("%w: %q", ErrResultTokenNotAvailable, id)
	}

	token := queue.tokens[index]
	copy(queue.tokens[index:], queue.tokens[index+1:])
	queue.tokens = queue.tokens[:len(queue.tokens)-1]
	return token, nil
}

func (queue *ResultQueue) indexOf(id domain.ResultTokenID) int {
	for index, token := range queue.tokens {
		if token.ID == id {
			return index
		}
	}
	return -1
}

func availableEnd(tokens []ResultToken, order room.MovementOrder) int {
	if len(tokens) == 0 {
		return 0
	}
	if order == room.MovementFIFO {
		return 1
	}
	for index, token := range tokens {
		if token.Result == domain.YutBuk {
			if index == 0 {
				return 1
			}
			return index
		}
	}
	return len(tokens)
}

func validateMovementOrder(order room.MovementOrder) error {
	switch order {
	case room.MovementFIFO, room.MovementFree:
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrInvalidMovementOrder, order)
	}
}
