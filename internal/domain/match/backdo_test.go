package match

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/board"
	"buk-yutnori/internal/domain/room"
	"buk-yutnori/internal/domain/turn"
)

func TestBackdoMovePlanAndApplyUseActualPreviousSpace(t *testing.T) {
	settings := room.DefaultSettings()
	settings.PieceCount = 2
	game := newCanonicalGame(t, settings)
	applyMove(t, game, domain.TeamA, "A-1", domain.YutDo, "")

	before := game.Snapshot()
	plan, err := game.BackdoMovePlan(domain.TeamA, "A-1")
	if err != nil {
		t.Fatalf("BackdoMovePlan() error = %v", err)
	}
	if plan.DestinationState != domain.PieceHomeCheckpoint ||
		plan.DestinationSpaceID != "chammeogi" || plan.ActualPreviousSpace != "do" ||
		!reflect.DeepEqual(plan.MovedPieceIDs, []domain.PieceID{"A-1"}) {
		t.Fatalf("BackdoMovePlan() = %#v", plan)
	}
	plan.MovedPieceIDs[0] = "mutated"
	if after := game.Snapshot(); !reflect.DeepEqual(after, before) {
		t.Fatal("planning or mutating the returned plan changed game state")
	}

	first := applyBackdo(t, game, domain.TeamA, "A-1")
	if first.MovementKind != domain.MovementBackdo || first.FromSpaceID != "do" ||
		first.ToSpaceID != "chammeogi" ||
		!reflect.DeepEqual(first.MovedPieceIDs, []domain.PieceID{"A-1"}) {
		t.Fatalf("first Backdo outcome = %#v", first)
	}
	piece := requirePiece(t, game.Snapshot(), "A-1")
	if piece.State != domain.PieceHomeCheckpoint || piece.CurrentSpaceID != "chammeogi" ||
		piece.ActualPreviousSpace != "do" {
		t.Fatalf("piece after first Backdo = %#v", piece)
	}

	second := applyBackdo(t, game, domain.TeamA, "A-1")
	if second.FromSpaceID != "chammeogi" || second.ToSpaceID != "do" {
		t.Fatalf("second Backdo outcome = %#v", second)
	}
	piece = requirePiece(t, game.Snapshot(), "A-1")
	if piece.State != domain.PieceOnBoard || piece.CurrentSpaceID != "do" ||
		piece.ActualPreviousSpace != "chammeogi" {
		t.Fatalf("piece after second Backdo = %#v", piece)
	}
}

func TestBackdoFromHomeCheckpointReturnsToEntrySpace(t *testing.T) {
	settings := room.DefaultSettings()
	settings.PieceCount = 2
	game := newCanonicalGame(t, settings)
	moveToNalYut(t, game, "A-1")
	applyMove(t, game, domain.TeamA, "A-1", domain.YutDo, "")

	outcome := applyBackdo(t, game, domain.TeamA, "A-1")
	if outcome.FromSpaceID != "chammeogi" || outcome.ToSpaceID != "nal_yut" {
		t.Fatalf("Backdo outcome = %#v", outcome)
	}
	piece := requirePiece(t, game.Snapshot(), "A-1")
	if piece.State != domain.PieceOnBoard || piece.CurrentSpaceID != "nal_yut" ||
		piece.ActualPreviousSpace != "chammeogi" {
		t.Fatalf("piece after checkpoint Backdo = %#v", piece)
	}
}

func TestBackdoReturnsToCenterOnlyThroughRecordedEntry(t *testing.T) {
	settings := room.DefaultSettings()
	settings.PieceCount = 2
	game := newCanonicalGame(t, settings)
	applyMove(t, game, domain.TeamA, "A-1", domain.YutMo, "")
	applyMove(t, game, domain.TeamA, "A-1", domain.YutGeol, domain.RouteShortcut)
	applyMove(t, game, domain.TeamA, "A-1", domain.YutDo, domain.RouteNormal)

	before := requirePiece(t, game.Snapshot(), "A-1")
	if before.CurrentSpaceID != "sok_yut" || before.ActualPreviousSpace != "bang" {
		t.Fatalf("piece before Backdo = %#v", before)
	}
	applyBackdo(t, game, domain.TeamA, "A-1")
	after := requirePiece(t, game.Snapshot(), "A-1")
	if after.CurrentSpaceID != "bang" || after.ActualPreviousSpace != "sok_yut" {
		t.Fatalf("piece after Backdo = %#v", after)
	}
}

func TestBackdoMovesStacksAndArrivingHistoryWins(t *testing.T) {
	settings := room.DefaultSettings()
	settings.PieceCount = 2
	game := newCanonicalGame(t, settings)
	applyMove(t, game, domain.TeamA, "A-1", domain.YutGae, "")
	applyMove(t, game, domain.TeamA, "A-2", domain.YutGeol, "")

	outcome := applyBackdo(t, game, domain.TeamA, "A-2")
	if !reflect.DeepEqual(outcome.MovedPieceIDs, []domain.PieceID{"A-2"}) ||
		!reflect.DeepEqual(outcome.StackedPieceIDs, []domain.PieceID{"A-1", "A-2"}) {
		t.Fatalf("Backdo stacking outcome = %#v", outcome)
	}
	for _, id := range []domain.PieceID{"A-1", "A-2"} {
		piece := requirePiece(t, game.Snapshot(), id)
		if piece.CurrentSpaceID != "gae" || piece.ActualPreviousSpace != "geol" {
			t.Fatalf("piece %q after stacking Backdo = %#v", id, piece)
		}
	}

	wholeStack := applyBackdo(t, game, domain.TeamA, "A-1")
	if !reflect.DeepEqual(wholeStack.MovedPieceIDs, []domain.PieceID{"A-1", "A-2"}) {
		t.Fatalf("whole-stack Backdo outcome = %#v", wholeStack)
	}
	for _, id := range wholeStack.MovedPieceIDs {
		piece := requirePiece(t, game.Snapshot(), id)
		if piece.CurrentSpaceID != "geol" || piece.ActualPreviousSpace != "gae" {
			t.Fatalf("piece %q after whole-stack Backdo = %#v", id, piece)
		}
	}
}

func TestBackdoKeepsAlliedPiecesIndependentWhenStackingDisabled(t *testing.T) {
	settings := room.DefaultSettings()
	settings.PieceCount = 2
	settings.StackingEnabled = false
	game := newCanonicalGame(t, settings)
	applyMove(t, game, domain.TeamA, "A-1", domain.YutGeol, "")
	applyMove(t, game, domain.TeamA, "A-2", domain.YutGeol, "")

	outcome := applyBackdo(t, game, domain.TeamA, "A-1")
	if !reflect.DeepEqual(outcome.MovedPieceIDs, []domain.PieceID{"A-1"}) ||
		outcome.StackedPieceIDs != nil {
		t.Fatalf("independent Backdo outcome = %#v", outcome)
	}
	first := requirePiece(t, game.Snapshot(), "A-1")
	second := requirePiece(t, game.Snapshot(), "A-2")
	if first.CurrentSpaceID != "gae" || first.ActualPreviousSpace != "geol" {
		t.Fatalf("A-1 after Backdo = %#v", first)
	}
	if second.CurrentSpaceID != "geol" || second.ActualPreviousSpace != "gae" {
		t.Fatalf("A-2 changed with independent ally = %#v", second)
	}
}

func TestBackdoCapturesEveryOpponentAndUsesSpecialCapturePolicy(t *testing.T) {
	settings := room.DefaultSettings()
	settings.PieceCount = 2
	settings.StackingEnabled = false
	settings.CaptureExtraThrow = room.CaptureExtraThrowDoToGeolPlusSpecial
	game := newCanonicalGame(t, settings)
	applyMove(t, game, domain.TeamB, "B-1", domain.YutGae, "")
	applyMove(t, game, domain.TeamB, "B-2", domain.YutGae, "")
	applyMove(t, game, domain.TeamA, "A-1", domain.YutGeol, "")

	outcome := applyBackdo(t, game, domain.TeamA, "A-1")
	if !reflect.DeepEqual(outcome.CapturedPieceIDs, []domain.PieceID{"B-1", "B-2"}) ||
		!outcome.CaptureExtraThrow {
		t.Fatalf("Backdo capture outcome = %#v", outcome)
	}
	if got := outcome.TurnOutcome(); got != (turn.MoveOutcome{CaptureExtraThrow: true}) {
		t.Fatalf("TurnOutcome() = %#v, want capture extra throw", got)
	}
	for _, id := range outcome.CapturedPieceIDs {
		piece := requirePiece(t, game.Snapshot(), id)
		if piece.State != domain.PieceWaiting || piece.CurrentSpaceID != "" ||
			piece.ActualPreviousSpace != "" {
			t.Fatalf("captured piece %q = %#v", id, piece)
		}
	}
}

func TestBackdoRejectsUnusablePiecesAndInvalidOwnershipAtomically(t *testing.T) {
	settings := room.DefaultSettings()
	settings.PieceCount = 2
	game := newCanonicalGame(t, settings)
	tests := []struct {
		name      string
		teamID    domain.TeamID
		pieceID   domain.PieceID
		wantError error
	}{
		{name: "waiting piece", teamID: domain.TeamA, pieceID: "A-1", wantError: board.ErrBackdoMovementUnavailable},
		{name: "unknown piece", teamID: domain.TeamA, pieceID: "missing", wantError: ErrUnknownPiece},
		{name: "opponent piece", teamID: domain.TeamA, pieceID: "B-1", wantError: ErrPieceNotOwned},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			before := game.Snapshot()
			if _, err := game.ApplyBackdoMove(test.teamID, test.pieceID); !errors.Is(err, test.wantError) {
				t.Fatalf("ApplyBackdoMove() error = %v, want %v", err, test.wantError)
			}
			if after := game.Snapshot(); !reflect.DeepEqual(after, before) {
				t.Fatal("rejected Backdo changed game state")
			}
		})
	}

	moveToNalYut(t, game, "A-1")
	applyMove(t, game, domain.TeamA, "A-1", domain.YutGae, "")
	before := game.Snapshot()
	if _, err := game.ApplyBackdoMove(domain.TeamA, "A-1"); !errors.Is(err, board.ErrBackdoMovementUnavailable) {
		t.Fatalf("ApplyBackdoMove(finished) error = %v", err)
	}
	if after := game.Snapshot(); !reflect.DeepEqual(after, before) {
		t.Fatal("finished-piece Backdo changed game state")
	}
}

func TestBackdoPlanningAndApplicationRejectAfterMatchEnd(t *testing.T) {
	settings := room.DefaultSettings()
	settings.PieceCount = 2
	game := newCanonicalGame(t, settings)
	moveToNalYut(t, game, "A-1")
	applyMove(t, game, domain.TeamA, "A-1", domain.YutGae, "")
	moveToNalYut(t, game, "A-2")
	applyMove(t, game, domain.TeamA, "A-2", domain.YutGae, "")

	before := game.Snapshot()
	if _, err := game.BackdoMovePlan(domain.TeamB, "B-1"); !errors.Is(err, ErrMatchEnded) {
		t.Fatalf("BackdoMovePlan() error = %v, want ErrMatchEnded", err)
	}
	if _, err := game.ApplyBackdoMove(domain.TeamB, "B-1"); !errors.Is(err, ErrMatchEnded) {
		t.Fatalf("ApplyBackdoMove() error = %v, want ErrMatchEnded", err)
	}
	if after := game.Snapshot(); !reflect.DeepEqual(after, before) {
		t.Fatal("Backdo after match end changed game state")
	}
}

func TestGameSupportsConcurrentBackdoPlanningSnapshotsAndApplication(t *testing.T) {
	settings := room.DefaultSettings()
	settings.PieceCount = 2
	game := newCanonicalGame(t, settings)
	applyMove(t, game, domain.TeamA, "A-1", domain.YutDo, "")

	const attempts = 128
	errorsFound := make(chan error, 3)
	var waitGroup sync.WaitGroup
	waitGroup.Add(3)

	go func() {
		defer waitGroup.Done()
		for range attempts {
			if err := validateSnapshotPieceStates(game.Snapshot()); err != nil {
				errorsFound <- err
				return
			}
		}
	}()
	go func() {
		defer waitGroup.Done()
		for range attempts {
			plan, err := game.BackdoMovePlan(domain.TeamA, "A-1")
			if err != nil {
				errorsFound <- fmt.Errorf("planning Backdo: %w", err)
				return
			}
			if plan.DestinationSpaceID != "do" && plan.DestinationSpaceID != "chammeogi" {
				errorsFound <- fmt.Errorf("unexpected Backdo plan: %#v", plan)
				return
			}
		}
	}()
	go func() {
		defer waitGroup.Done()
		for range attempts {
			if _, err := game.ApplyBackdoMove(domain.TeamA, "A-1"); err != nil {
				errorsFound <- fmt.Errorf("applying Backdo: %w", err)
				return
			}
		}
	}()

	waitGroup.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Error(err)
	}
	if err := validateSnapshotPieceStates(game.Snapshot()); err != nil {
		t.Error(err)
	}
}

func applyBackdo(
	t *testing.T,
	game *Game,
	teamID domain.TeamID,
	pieceID domain.PieceID,
) MoveOutcome {
	t.Helper()
	outcome, err := game.ApplyBackdoMove(teamID, pieceID)
	if err != nil {
		t.Fatalf("ApplyBackdoMove(%q, %q) error = %v", teamID, pieceID, err)
	}
	return outcome
}
