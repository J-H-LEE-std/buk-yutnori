package turn

import (
	"errors"
	"reflect"
	"sync"
	"testing"

	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/room"
)

func TestMachineRunsCanonicalThrowingChain(t *testing.T) {
	machine := mustMachine(t, room.MovementFree, true)
	assertMachineState(t, machine, domain.TurnStart, domain.InputNone, "", "")

	if err := machine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	assertMachineState(
		t,
		machine,
		domain.TurnWaitThrow,
		domain.InputThrow,
		domain.ResultOriginInitialThrow,
		"",
	)

	recordThrow(t, machine, resultToken("token-yut", domain.YutYut, domain.ResultOriginInitialThrow))
	assertMachineState(
		t,
		machine,
		domain.TurnWaitThrow,
		domain.InputThrow,
		domain.ResultOriginYutExtra,
		"",
	)

	recordThrow(t, machine, resultToken("token-mo", domain.YutMo, domain.ResultOriginYutExtra))
	assertMachineState(
		t,
		machine,
		domain.TurnWaitThrow,
		domain.InputThrow,
		domain.ResultOriginMoExtra,
		"",
	)

	recordThrow(t, machine, resultToken("token-do", domain.YutDo, domain.ResultOriginMoExtra))
	assertMachineState(t, machine, domain.TurnResolveQueue, domain.InputNone, "", "")
	assertTokenIDs(t, machine.Snapshot().ResultQueue, "token-yut", "token-mo", "token-do")

	if err := machine.ResolveQueue(); err != nil {
		t.Fatalf("ResolveQueue() error = %v", err)
	}
	assertMachineState(t, machine, domain.TurnWaitMoveSelection, domain.InputSelectMove, "", "")
}

func TestMachineCanDisableYutMoExtraThrow(t *testing.T) {
	machine := mustMachine(t, room.MovementFIFO, false)
	startMachine(t, machine)
	recordThrow(t, machine, resultToken("token-yut", domain.YutYut, domain.ResultOriginInitialThrow))

	assertMachineState(t, machine, domain.TurnResolveQueue, domain.InputNone, "", "")
	if err := machine.ResolveQueue(); err != nil {
		t.Fatalf("ResolveQueue() error = %v", err)
	}
	assertMachineState(
		t,
		machine,
		domain.TurnWaitMoveSelection,
		domain.InputSelectMove,
		"",
		"",
	)
}

func TestMachineReturnsFromCaptureExtraThrowToQueue(t *testing.T) {
	machine := mustMachine(t, room.MovementFIFO, true)
	startMachine(t, machine)
	recordThrow(t, machine, resultToken("token-yut", domain.YutYut, domain.ResultOriginInitialThrow))
	recordThrow(t, machine, resultToken("token-do", domain.YutDo, domain.ResultOriginYutExtra))
	resolveSingleOrdinary(t, machine, "token-yut")
	applySelectedMove(t, machine, "token-yut", false)

	if err := machine.CompleteMove("token-yut", MoveOutcome{CaptureExtraThrow: true}); err != nil {
		t.Fatalf("CompleteMove() error = %v", err)
	}
	assertMachineState(
		t,
		machine,
		domain.TurnWaitThrow,
		domain.InputThrow,
		domain.ResultOriginCaptureExtra,
		"",
	)
	assertTokenIDs(t, machine.Snapshot().ResultQueue, "token-do")

	recordThrow(t, machine, resultToken("token-capture", domain.YutYut, domain.ResultOriginCaptureExtra))
	assertMachineState(
		t,
		machine,
		domain.TurnWaitThrow,
		domain.InputThrow,
		domain.ResultOriginYutExtra,
		"",
	)
	assertTokenIDs(t, machine.Snapshot().ResultQueue, "token-do", "token-capture")

	recordThrow(t, machine, resultToken("token-extra", domain.YutGae, domain.ResultOriginYutExtra))
	assertMachineState(t, machine, domain.TurnResolveQueue, domain.InputNone, "", "")
	assertTokenIDs(t, machine.Snapshot().ResultQueue, "token-do", "token-capture", "token-extra")
}

func TestMovementOrderControlsResultSelection(t *testing.T) {
	t.Run("FIFO auto-selects head", func(t *testing.T) {
		machine := machineWithTwoOrdinaryTokens(t, room.MovementFIFO)
		if err := machine.ResolveQueue(); err != nil {
			t.Fatalf("ResolveQueue() error = %v", err)
		}
		assertMachineState(
			t,
			machine,
			domain.TurnWaitMoveSelection,
			domain.InputSelectMove,
			"",
			"",
		)
	})

	t.Run("free order asks when multiple tokens are available", func(t *testing.T) {
		machine := machineWithTwoOrdinaryTokens(t, room.MovementFree)
		if err := machine.ResolveQueue(); err != nil {
			t.Fatalf("ResolveQueue() error = %v", err)
		}
		assertMachineState(
			t,
			machine,
			domain.TurnWaitMoveSelection,
			domain.InputSelectMove,
			"",
			"",
		)
		if err := machine.SelectMove("token-do", false); err != nil {
			t.Fatalf("SelectMove() error = %v", err)
		}
		assertMachineState(
			t,
			machine,
			domain.TurnApplyMove,
			domain.InputNone,
			"",
			"token-do",
		)
	})
}

func TestMachineRunsPieceRouteAndMovePhases(t *testing.T) {
	machine := mustMachine(t, room.MovementFIFO, false)
	startMachine(t, machine)
	recordThrow(t, machine, resultToken("token-1", domain.YutGeol, domain.ResultOriginInitialThrow))
	resolveSingleOrdinary(t, machine, "token-1")

	if err := machine.SelectMove("token-1", true); err != nil {
		t.Fatalf("SelectMove() error = %v", err)
	}
	assertMachineState(
		t,
		machine,
		domain.TurnWaitRouteSelection,
		domain.InputSelectRoute,
		"",
		"token-1",
	)
	if err := machine.RouteSelected("token-1"); err != nil {
		t.Fatalf("RouteSelected() error = %v", err)
	}
	assertMachineState(t, machine, domain.TurnApplyMove, domain.InputNone, "", "token-1")
	if err := machine.MoveApplied("token-1"); err != nil {
		t.Fatalf("MoveApplied() error = %v", err)
	}
	assertMachineState(
		t,
		machine,
		domain.TurnResolveStackCaptureFinish,
		domain.InputNone,
		"",
		"token-1",
	)
	if err := machine.CompleteMove("token-1", MoveOutcome{}); err != nil {
		t.Fatalf("CompleteMove() error = %v", err)
	}
	assertMachineState(t, machine, domain.TurnResolveQueue, domain.InputNone, "", "")
	if err := machine.ResolveQueue(); err != nil {
		t.Fatalf("ResolveQueue() error = %v", err)
	}
	assertMachineState(t, machine, domain.TurnEnd, domain.InputNone, "", "")
}

func TestBukCannotPassEarlierTokenAndResolvesAutomatically(t *testing.T) {
	machine := mustMachine(t, room.MovementFree, true)
	startMachine(t, machine)
	recordThrow(t, machine, resultToken("token-yut", domain.YutYut, domain.ResultOriginInitialThrow))
	recordThrow(t, machine, resultToken("token-buk", domain.YutBuk, domain.ResultOriginYutExtra))

	if err := machine.ResolveQueue(); err != nil {
		t.Fatalf("ResolveQueue() error = %v", err)
	}
	assertMachineState(
		t,
		machine,
		domain.TurnWaitMoveSelection,
		domain.InputSelectMove,
		"",
		"",
	)
	applySelectedMove(t, machine, "token-yut", false)
	if err := machine.CompleteMove("token-yut", MoveOutcome{}); err != nil {
		t.Fatalf("CompleteMove() error = %v", err)
	}

	if err := machine.ResolveQueue(); err != nil {
		t.Fatalf("ResolveQueue() error = %v", err)
	}
	assertMachineState(t, machine, domain.TurnResolveBuk, domain.InputNone, "", "token-buk")
	if err := machine.CompleteBuk("token-buk", BukOutcome{NoCandidate: true}); err != nil {
		t.Fatalf("CompleteBuk() error = %v", err)
	}
	assertMachineState(t, machine, domain.TurnEnd, domain.InputNone, "", "")
	if got := machine.Snapshot().ResultQueue; len(got) != 0 {
		t.Fatalf("queue after Buk resolution = %v, want empty", got)
	}
}

func TestMachineDiscardsOnlySelectedUnusableResult(t *testing.T) {
	machine := machineWithTwoOrdinaryTokens(t, room.MovementFIFO)
	if err := machine.ResolveQueue(); err != nil {
		t.Fatalf("ResolveQueue() error = %v", err)
	}
	if err := machine.DiscardUnusableResult("token-yut"); err != nil {
		t.Fatalf("DiscardUnusableResult() error = %v", err)
	}
	assertMachineState(t, machine, domain.TurnResolveQueue, domain.InputNone, "", "")
	assertTokenIDs(t, machine.Snapshot().ResultQueue, "token-do")

	if err := machine.ResolveQueue(); err != nil {
		t.Fatalf("ResolveQueue() error = %v", err)
	}
	assertMachineState(
		t,
		machine,
		domain.TurnWaitMoveSelection,
		domain.InputSelectMove,
		"",
		"",
	)
}

func TestMachineEndsMatchImmediatelyAfterResolvedMove(t *testing.T) {
	machine := mustMachine(t, room.MovementFIFO, false)
	startMachine(t, machine)
	recordThrow(t, machine, resultToken("token-1", domain.YutDo, domain.ResultOriginInitialThrow))
	resolveSingleOrdinary(t, machine, "token-1")
	applySelectedMove(t, machine, "token-1", false)
	if err := machine.CompleteMove("token-1", MoveOutcome{MatchEnded: true}); err != nil {
		t.Fatalf("CompleteMove() error = %v", err)
	}
	assertMachineState(t, machine, domain.TurnMatchEnd, domain.InputNone, "", "")
}

func TestMachineRejectsInvalidActionsWithoutMutation(t *testing.T) {
	machine := mustMachine(t, room.MovementFIFO, true)

	assertUnchangedAfterError(t, machine, ErrInvalidTransition, func() error {
		return machine.RecordThrow(resultToken("token-1", domain.YutDo, domain.ResultOriginInitialThrow))
	})
	startMachine(t, machine)
	assertUnchangedAfterError(t, machine, ErrInvalidTransition, func() error {
		return machine.SelectMove("token-1", false)
	})

	if _, err := machine.BeginThrow(); err != nil {
		t.Fatalf("BeginThrow() error = %v", err)
	}
	assertUnchangedAfterError(t, machine, ErrUnexpectedThrowOrigin, func() error {
		return machine.RecordThrow(resultToken("token-1", domain.YutDo, domain.ResultOriginCaptureExtra))
	})
	assertUnchangedAfterError(t, machine, ErrWrongTurnPlayer, func() error {
		token := resultToken("token-1", domain.YutDo, domain.ResultOriginInitialThrow)
		token.GeneratedByPlayerID = "player-b"
		return machine.RecordThrow(token)
	})
	if err := machine.RecordThrow(resultToken("token-1", domain.YutDo, domain.ResultOriginInitialThrow)); err != nil {
		t.Fatalf("RecordThrow() error = %v", err)
	}
	if err := machine.ResolveQueue(); err != nil {
		t.Fatalf("ResolveQueue() error = %v", err)
	}
	assertUnchangedAfterError(t, machine, ErrResultTokenNotAvailable, func() error {
		return machine.SelectMove("other-token", false)
	})
	assertUnchangedAfterError(t, machine, ErrInvalidTransition, func() error {
		return machine.RouteSelected("token-1")
	})
	if err := machine.SelectMove("token-1", false); err != nil {
		t.Fatalf("SelectMove() error = %v", err)
	}
	if err := machine.MoveApplied("token-1"); err != nil {
		t.Fatalf("MoveApplied() error = %v", err)
	}
	assertUnchangedAfterError(t, machine, ErrInvalidOutcome, func() error {
		return machine.CompleteMove("token-1", MoveOutcome{CaptureExtraThrow: true, MatchEnded: true})
	})
}

func TestNewMachineRejectsInvalidConfiguration(t *testing.T) {
	settings := room.DefaultSettings()
	settings.MovementOrder = "random"
	if _, err := NewMachine("player-a", settings); !errors.Is(err, ErrInvalidMachineConfig) {
		t.Fatalf("NewMachine(invalid settings) error = %v, want ErrInvalidMachineConfig", err)
	}
	if _, err := NewMachine("", room.DefaultSettings()); !errors.Is(err, ErrInvalidMachineConfig) {
		t.Fatalf("NewMachine(invalid player) error = %v, want ErrInvalidMachineConfig", err)
	}
}

func TestMachineSerializesConcurrentStart(t *testing.T) {
	machine := mustMachine(t, room.MovementFIFO, false)
	const attempts = 100
	errorsFound := make(chan error, attempts)
	var wait sync.WaitGroup

	for range attempts {
		wait.Add(1)
		go func() {
			defer wait.Done()
			errorsFound <- machine.Start()
		}()
	}
	wait.Wait()
	close(errorsFound)

	accepted := 0
	for err := range errorsFound {
		switch {
		case err == nil:
			accepted++
		case errors.Is(err, ErrInvalidTransition):
		default:
			t.Fatalf("Start() error = %v", err)
		}
	}
	if accepted != 1 {
		t.Fatalf("accepted Start() calls = %d, want 1", accepted)
	}
	assertMachineState(
		t,
		machine,
		domain.TurnWaitThrow,
		domain.InputThrow,
		domain.ResultOriginInitialThrow,
		"",
	)
}

func mustMachine(t *testing.T, order room.MovementOrder, yutMoExtra bool) *Machine {
	t.Helper()
	settings := room.DefaultSettings()
	settings.MovementOrder = order
	settings.YutMoExtraThrow = yutMoExtra
	machine, err := NewMachine("player-a", settings)
	if err != nil {
		t.Fatalf("NewMachine() error = %v", err)
	}
	return machine
}

func startMachine(t *testing.T, machine *Machine) {
	t.Helper()
	if err := machine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
}

func recordThrow(t *testing.T, machine *Machine, token ResultToken) {
	t.Helper()
	origin, err := machine.BeginThrow()
	if err != nil {
		t.Fatalf("BeginThrow() error = %v", err)
	}
	if origin != token.Origin {
		t.Fatalf("BeginThrow() origin = %q, token origin = %q", origin, token.Origin)
	}
	assertMachineState(t, machine, domain.TurnThrowingChain, domain.InputNone, origin, "")
	if err := machine.RecordThrow(token); err != nil {
		t.Fatalf("RecordThrow() error = %v", err)
	}
}

func machineWithTwoOrdinaryTokens(t *testing.T, order room.MovementOrder) *Machine {
	t.Helper()
	machine := mustMachine(t, order, true)
	startMachine(t, machine)
	recordThrow(t, machine, resultToken("token-yut", domain.YutYut, domain.ResultOriginInitialThrow))
	recordThrow(t, machine, resultToken("token-do", domain.YutDo, domain.ResultOriginYutExtra))
	return machine
}

func resolveSingleOrdinary(t *testing.T, machine *Machine, tokenID domain.ResultTokenID) {
	t.Helper()
	if err := machine.ResolveQueue(); err != nil {
		t.Fatalf("ResolveQueue() error = %v", err)
	}
	assertMachineState(
		t,
		machine,
		domain.TurnWaitMoveSelection,
		domain.InputSelectMove,
		"",
		"",
	)
}

func applySelectedMove(t *testing.T, machine *Machine, tokenID domain.ResultTokenID, routeRequired bool) {
	t.Helper()
	if err := machine.SelectMove(tokenID, routeRequired); err != nil {
		t.Fatalf("SelectMove() error = %v", err)
	}
	if routeRequired {
		if err := machine.RouteSelected(tokenID); err != nil {
			t.Fatalf("RouteSelected() error = %v", err)
		}
	}
	if err := machine.MoveApplied(tokenID); err != nil {
		t.Fatalf("MoveApplied() error = %v", err)
	}
}

func assertMachineState(
	t *testing.T,
	machine *Machine,
	phase domain.TurnPhase,
	input domain.RequiredInput,
	origin domain.ResultOrigin,
	selected domain.ResultTokenID,
) {
	t.Helper()
	snapshot := machine.Snapshot()
	if snapshot.Phase != phase ||
		snapshot.RequiredInput != input ||
		snapshot.ExpectedThrowOrigin != origin ||
		snapshot.SelectedTokenID != selected {
		t.Fatalf(
			"state = phase %q, input %q, origin %q, selected %q; want %q, %q, %q, %q",
			snapshot.Phase,
			snapshot.RequiredInput,
			snapshot.ExpectedThrowOrigin,
			snapshot.SelectedTokenID,
			phase,
			input,
			origin,
			selected,
		)
	}
}

func assertUnchangedAfterError(
	t *testing.T,
	machine *Machine,
	want error,
	action func() error,
) {
	t.Helper()
	before := machine.Snapshot()
	err := action()
	if !errors.Is(err, want) {
		t.Fatalf("action error = %v, want %v", err, want)
	}
	after := machine.Snapshot()
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("failed action changed state\nbefore: %#v\nafter:  %#v", before, after)
	}
}
