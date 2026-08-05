package turn

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/room"
)

func TestResultTokenValidatesCanonicalFields(t *testing.T) {
	valid := resultToken("token-1", domain.YutGae, domain.ResultOriginInitialThrow)
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid token rejected: %v", err)
	}

	data, err := json.Marshal(valid)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	wantJSON := `{"token_id":"token-1","result":"gae","origin":"initial_throw","generated_by_player_id":"player-a"}`
	if string(data) != wantJSON {
		t.Fatalf("Marshal() = %s, want %s", data, wantJSON)
	}

	tests := []struct {
		name   string
		field  string
		mutate func(*ResultToken)
	}{
		{name: "token ID", field: "token_id", mutate: func(token *ResultToken) { token.ID = "" }},
		{name: "result", field: "result", mutate: func(token *ResultToken) { token.Result = "invalid" }},
		{name: "origin", field: "origin", mutate: func(token *ResultToken) { token.Origin = "invalid" }},
		{name: "player ID", field: "generated_by_player_id", mutate: func(token *ResultToken) { token.GeneratedByPlayerID = "" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			token := valid
			test.mutate(&token)
			err := token.Validate()
			if !errors.Is(err, ErrInvalidResultToken) {
				t.Fatalf("Validate() error = %v, want ErrInvalidResultToken", err)
			}
			if !strings.Contains(err.Error(), test.field) {
				t.Fatalf("Validate() error = %q, want field %q", err, test.field)
			}
		})
	}
}

func TestResultOriginCanonicalJSON(t *testing.T) {
	origins := []domain.ResultOrigin{
		domain.ResultOriginInitialThrow,
		domain.ResultOriginYutExtra,
		domain.ResultOriginMoExtra,
		domain.ResultOriginCaptureExtra,
	}
	want := []string{"initial_throw", "yut_extra", "mo_extra", "capture_extra"}

	for index, origin := range origins {
		if err := origin.Validate(); err != nil {
			t.Fatalf("Validate(%q) error = %v", origin, err)
		}
		data, err := json.Marshal(origin)
		if err != nil {
			t.Fatalf("Marshal(%q) error = %v", origin, err)
		}
		if string(data) != `"`+want[index]+`"` {
			t.Fatalf("Marshal(%q) = %s", origin, data)
		}
	}

	invalid := domain.ResultOrigin("bonus")
	if err := invalid.Validate(); !errors.Is(err, domain.ErrInvalidEnumValue) {
		t.Fatalf("Validate(invalid) error = %v, want ErrInvalidEnumValue", err)
	}
	if _, err := json.Marshal(invalid); !errors.Is(err, domain.ErrInvalidEnumValue) {
		t.Fatalf("Marshal(invalid) error = %v, want ErrInvalidEnumValue", err)
	}
}

func TestResultTokenScalarTypesRejectInvalidJSONWithoutMutation(t *testing.T) {
	for _, input := range []string{`""`, `42`, `null`} {
		id := domain.ResultTokenID("token-1")
		if err := json.Unmarshal([]byte(input), &id); err == nil {
			t.Errorf("ResultTokenID Unmarshal(%s) error = nil", input)
		}
		if id != "token-1" {
			t.Errorf("ResultTokenID after Unmarshal(%s) = %q", input, id)
		}
	}

	for _, input := range []string{`"bonus"`, `42`, `null`} {
		origin := domain.ResultOriginInitialThrow
		if err := json.Unmarshal([]byte(input), &origin); err == nil {
			t.Errorf("ResultOrigin Unmarshal(%s) error = nil", input)
		}
		if origin != domain.ResultOriginInitialThrow {
			t.Errorf("ResultOrigin after Unmarshal(%s) = %q", input, origin)
		}
	}
}

func TestResultQueueAppendsAtTailAndReturnsCopies(t *testing.T) {
	queue := mustQueue(t,
		resultToken("token-1", domain.YutDo, domain.ResultOriginInitialThrow),
		resultToken("token-2", domain.YutYut, domain.ResultOriginYutExtra),
	)
	extra := resultToken("token-3", domain.YutMo, domain.ResultOriginCaptureExtra)
	if err := queue.Append(extra); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	if got, want := queue.Len(), 3; got != want {
		t.Fatalf("Len() = %d, want %d", got, want)
	}
	snapshot := queue.Snapshot()
	assertTokenIDs(t, snapshot, "token-1", "token-2", "token-3")

	snapshot[0].ID = "mutated"
	snapshot = append(snapshot, resultToken("token-4", domain.YutGae, domain.ResultOriginInitialThrow))
	assertTokenIDs(t, queue.Snapshot(), "token-1", "token-2", "token-3")
}

func TestResultQueueRejectsDuplicateAndReusedIDs(t *testing.T) {
	first := resultToken("token-1", domain.YutDo, domain.ResultOriginInitialThrow)
	queue := mustQueue(t, first)

	if err := queue.Append(first); !errors.Is(err, ErrDuplicateResultTokenID) {
		t.Fatalf("Append(duplicate) error = %v, want ErrDuplicateResultTokenID", err)
	}
	if _, err := queue.Consume(first.ID, room.MovementFIFO); err != nil {
		t.Fatalf("Consume() error = %v", err)
	}
	if err := queue.Append(first); !errors.Is(err, ErrDuplicateResultTokenID) {
		t.Fatalf("Append(reused) error = %v, want ErrDuplicateResultTokenID", err)
	}
	if _, err := queue.Consume(first.ID, room.MovementFIFO); !errors.Is(err, ErrResultTokenAlreadyConsumed) {
		t.Fatalf("Consume(twice) error = %v, want ErrResultTokenAlreadyConsumed", err)
	}
}

func TestFIFOOnlyExposesAndConsumesHead(t *testing.T) {
	queue := mustQueue(t,
		resultToken("token-1", domain.YutDo, domain.ResultOriginInitialThrow),
		resultToken("token-2", domain.YutGae, domain.ResultOriginInitialThrow),
	)

	available, err := queue.Available(room.MovementFIFO)
	if err != nil {
		t.Fatalf("Available() error = %v", err)
	}
	assertTokenIDs(t, available, "token-1")

	before := queue.Snapshot()
	if _, err := queue.Consume("token-2", room.MovementFIFO); !errors.Is(err, ErrResultTokenNotAvailable) {
		t.Fatalf("Consume(non-head) error = %v, want ErrResultTokenNotAvailable", err)
	}
	if !reflect.DeepEqual(queue.Snapshot(), before) {
		t.Fatal("failed FIFO consume changed queue")
	}

	consumed, err := queue.Consume("token-1", room.MovementFIFO)
	if err != nil {
		t.Fatalf("Consume(head) error = %v", err)
	}
	if consumed.ID != "token-1" {
		t.Fatalf("Consume(head) ID = %q", consumed.ID)
	}
	assertTokenIDs(t, queue.Snapshot(), "token-2")
}

func TestFreeOrderStopsAtFirstBukBarrier(t *testing.T) {
	queue := mustQueue(t,
		resultToken("token-do", domain.YutDo, domain.ResultOriginInitialThrow),
		resultToken("token-geol", domain.YutGeol, domain.ResultOriginInitialThrow),
		resultToken("token-buk", domain.YutBuk, domain.ResultOriginCaptureExtra),
		resultToken("token-mo", domain.YutMo, domain.ResultOriginMoExtra),
	)

	available, err := queue.Available(room.MovementFree)
	if err != nil {
		t.Fatalf("Available() error = %v", err)
	}
	assertTokenIDs(t, available, "token-do", "token-geol")
	available[0].ID = "mutated"
	assertTokenIDs(t, queue.Snapshot(), "token-do", "token-geol", "token-buk", "token-mo")

	if _, err := queue.Consume("token-mo", room.MovementFree); !errors.Is(err, ErrResultTokenNotAvailable) {
		t.Fatalf("Consume(after Buk) error = %v, want ErrResultTokenNotAvailable", err)
	}
	if _, err := queue.Consume("token-buk", room.MovementFree); !errors.Is(err, ErrResultTokenNotAvailable) {
		t.Fatalf("Consume(Buk before prefix) error = %v, want ErrResultTokenNotAvailable", err)
	}
	if _, err := queue.Consume("token-geol", room.MovementFree); err != nil {
		t.Fatalf("Consume(free prefix) error = %v", err)
	}
	assertAvailableIDs(t, queue, room.MovementFree, "token-do")
	if _, err := queue.Consume("token-do", room.MovementFree); err != nil {
		t.Fatalf("Consume(last prefix) error = %v", err)
	}
	assertAvailableIDs(t, queue, room.MovementFree, "token-buk")
	if _, err := queue.Consume("token-mo", room.MovementFree); !errors.Is(err, ErrResultTokenNotAvailable) {
		t.Fatalf("Consume(after head Buk) error = %v, want ErrResultTokenNotAvailable", err)
	}
	if _, err := queue.Consume("token-buk", room.MovementFree); err != nil {
		t.Fatalf("Consume(head Buk) error = %v", err)
	}
	assertAvailableIDs(t, queue, room.MovementFree, "token-mo")
}

func TestYutBeforeBukCannotBeSkipped(t *testing.T) {
	queue := mustQueue(t,
		resultToken("token-yut", domain.YutYut, domain.ResultOriginInitialThrow),
		resultToken("token-buk", domain.YutBuk, domain.ResultOriginYutExtra),
	)

	assertAvailableIDs(t, queue, room.MovementFree, "token-yut")
	if _, err := queue.Consume("token-buk", room.MovementFree); !errors.Is(err, ErrResultTokenNotAvailable) {
		t.Fatalf("Consume(Buk) error = %v, want ErrResultTokenNotAvailable", err)
	}
	if _, err := queue.Consume("token-yut", room.MovementFree); err != nil {
		t.Fatalf("Consume(Yut) error = %v", err)
	}
	assertAvailableIDs(t, queue, room.MovementFree, "token-buk")
}

func TestQueueFailuresLeaveStateUnchanged(t *testing.T) {
	queue := mustQueue(t, resultToken("token-1", domain.YutDo, domain.ResultOriginInitialThrow))
	before := queue.Snapshot()

	invalid := resultToken("token-2", domain.YutGae, domain.ResultOriginInitialThrow)
	invalid.Origin = "invalid"
	if err := queue.Append(invalid); !errors.Is(err, ErrInvalidResultToken) {
		t.Fatalf("Append(invalid) error = %v, want ErrInvalidResultToken", err)
	}
	if _, err := queue.Available(room.MovementOrder("random")); !errors.Is(err, ErrInvalidMovementOrder) {
		t.Fatalf("Available(invalid order) error = %v, want ErrInvalidMovementOrder", err)
	}
	if _, err := queue.Consume("unknown", room.MovementFIFO); !errors.Is(err, ErrResultTokenNotFound) {
		t.Fatalf("Consume(unknown) error = %v, want ErrResultTokenNotFound", err)
	}
	if !reflect.DeepEqual(queue.Snapshot(), before) {
		t.Fatal("failed operations changed queue")
	}
}

func TestResultQueueSerializesConcurrentAppends(t *testing.T) {
	const appends = 1000
	queue := mustQueue(t)
	appendErrors := make(chan error, appends)
	var wait sync.WaitGroup

	for index := range appends {
		wait.Add(1)
		go func() {
			defer wait.Done()
			id := domain.ResultTokenID(fmt.Sprintf("token-%d", index))
			appendErrors <- queue.Append(resultToken(id, domain.YutGae, domain.ResultOriginInitialThrow))
		}()
	}
	wait.Wait()
	close(appendErrors)

	for err := range appendErrors {
		if err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}
	if got := queue.Len(); got != appends {
		t.Fatalf("Len() = %d, want %d", got, appends)
	}
	seen := make(map[domain.ResultTokenID]struct{}, appends)
	for _, token := range queue.Snapshot() {
		if _, duplicate := seen[token.ID]; duplicate {
			t.Fatalf("duplicate token ID %q", token.ID)
		}
		seen[token.ID] = struct{}{}
	}
}

func resultToken(id domain.ResultTokenID, result domain.YutResult, origin domain.ResultOrigin) ResultToken {
	return ResultToken{
		ID:                  id,
		Result:              result,
		Origin:              origin,
		GeneratedByPlayerID: "player-a",
	}
}

func mustQueue(t *testing.T, tokens ...ResultToken) *ResultQueue {
	t.Helper()
	queue, err := NewResultQueue(tokens...)
	if err != nil {
		t.Fatalf("NewResultQueue() error = %v", err)
	}
	return queue
}

func assertAvailableIDs(t *testing.T, queue *ResultQueue, order room.MovementOrder, want ...domain.ResultTokenID) {
	t.Helper()
	available, err := queue.Available(order)
	if err != nil {
		t.Fatalf("Available() error = %v", err)
	}
	assertTokenIDs(t, available, want...)
}

func assertTokenIDs(t *testing.T, tokens []ResultToken, want ...domain.ResultTokenID) {
	t.Helper()
	got := make([]domain.ResultTokenID, len(tokens))
	for index, token := range tokens {
		got[index] = token.ID
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("token IDs = %v, want %v", got, want)
	}
}
