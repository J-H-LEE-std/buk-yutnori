package application

import (
	"testing"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/room"
	"buk-yutnori/internal/domain/yut"
)

// scriptThrowsFor replaces the seeded sampler with per-player scripted
// sequences. An exhausted sequence repeats its last value so drivers can
// finish open turns without failing.
func (fixture *matchFixture) scriptThrowsFor(scripts map[domain.PlayerID][]domain.YutResult) {
	indexes := make(map[domain.PlayerID]int, len(scripts))
	rt := fixture.runtime()
	rt.throwResult = func(yut.Mode) (domain.YutResult, error) {
		player := rt.currentPlayer()
		script, ok := scripts[player]
		if !ok || len(script) == 0 {
			return domain.YutDo, nil
		}
		index := indexes[player]
		if index >= len(script) {
			index = len(script) - 1
		}
		indexes[player] = index + 1
		return script[index], nil
	}
}

func findEvent(events []matchEventEnvelope, kind string) *matchEventEnvelope {
	for index := range events {
		if events[index].Type == kind {
			return &events[index]
		}
	}
	return nil
}

// Buk at the queue head is resolved automatically by the server: no piece
// selection is offered and the turn ends when no candidate exists.
func TestBukHeadResolvesAutomaticallyWithoutPieceSelection(t *testing.T) {
	t.Parallel()

	fixture := newMatchFixture(t, func(settings *room.Settings) {
		settings.BukModeEnabled = true
	})
	defer fixture.recorder.close()
	firstPlayer := fixture.runtime().currentPlayer()
	fixture.scriptThrowsFor(map[domain.PlayerID][]domain.YutResult{
		firstPlayer: {domain.YutBuk},
	})

	if err := fixture.registry.ThrowYut(auth.UserID(firstPlayer), fixture.roomID, fixture.matchID); err != nil {
		t.Fatalf("THROW_YUT(buk) error = %v", err)
	}
	events := fixture.recorder.snapshotEvents()

	throws := fixture.recorder.ofTypes("YUT_RESULT")
	if len(throws) != 1 || throws[0].Payload.Token.Result != domain.YutBuk {
		t.Fatalf("YUT_RESULT = %+v", throws)
	}
	resolved := fixture.recorder.ofTypes("BUK_RESOLVED")
	if len(resolved) != 1 || !resolved[0].Payload.NoCandidate || len(resolved[0].Payload.PieceIDs) != 0 {
		t.Fatalf("BUK_RESOLVED = %+v, want automatic no-candidate resolution", resolved)
	}
	if findEvent(events, "MOVE_REQUIRED") != nil {
		t.Fatal("Buk head must never ask for a user decision")
	}
	turns := fixture.recorder.ofTypes("TURN_STARTED")
	if len(turns) < 2 || turns[len(turns)-1].Payload.PlayerID == firstPlayer {
		t.Fatalf("TURN_STARTED after Buk = %+v, want handover to the other player", turns)
	}
}

// Free movement order exposes every ordinary token before a Buk barrier;
// FIFO resolves strictly head-first. Same scripted throws, different input.
func TestQueueOrderingBarrierFreeVersusFIFO(t *testing.T) {
	cases := []struct {
		name           string
		order          room.MovementOrder
		required       string
		wantCandidates int
	}{
		{name: "free exposes tokens before buk barrier", order: room.MovementFree, required: "select_move", wantCandidates: 2},
		{name: "fifo resolves head only", order: room.MovementFIFO, required: "select_move", wantCandidates: 1},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			fixture := newMatchFixture(t, func(settings *room.Settings) {
				settings.MovementOrder = testCase.order
				settings.BukModeEnabled = true
			})
			defer fixture.recorder.close()
			firstPlayer := fixture.runtime().currentPlayer()
			// yut grants an extra throw, mo grants another, then buk lands
			// behind two ordinary tokens.
			fixture.scriptThrowsFor(map[domain.PlayerID][]domain.YutResult{
				firstPlayer: {domain.YutYut, domain.YutMo, domain.YutBuk},
			})

			fixture.throwUntilResolved(t, firstPlayer)

			moves := fixture.recorder.ofTypes("MOVE_REQUIRED")
			if len(moves) == 0 {
				t.Fatal("no MOVE_REQUIRED after throwing chain")
			}
			first := moves[0]
			if first.Payload.RequiredInput != testCase.required || len(first.Payload.Candidates) < testCase.wantCandidates {
				t.Fatalf("first MOVE_REQUIRED = %+v, want %s with at least %d candidates", first, testCase.required, testCase.wantCandidates)
			}

			// Resolve the ordinary tokens; the buk tail must resolve
			// automatically without ever being selectable.
			fixture.driveUntilPlayerOrEnd(t, firstPlayer)
			resolved := fixture.recorder.ofTypes("BUK_RESOLVED")
			if len(resolved) != 1 {
				t.Fatalf("BUK_RESOLVED count = %d, want 1", len(resolved))
			}
			bukToken := resolved[0].Payload.TokenID
			for _, move := range fixture.recorder.ofTypes("MOVE_REQUIRED") {
				for _, candidate := range move.Payload.Candidates {
					if candidate.TokenID == bukToken {
						t.Fatalf("Buk token %s was offered for selection", bukToken)
					}
				}
			}
		})
	}
}

// Capturing returns every opposing piece on the destination space to waiting
// state and grants the canonical capture extra throw.
func TestCaptureReturnsAllOpposingPiecesAndGrantsExtraThrow(t *testing.T) {
	t.Parallel()

	fixture := newMatchFixture(t, func(settings *room.Settings) {
		settings.StackingEnabled = true
		settings.MovementOrder = room.MovementFIFO
	})
	defer fixture.recorder.close()
	firstPlayer := fixture.runtime().currentPlayer()
	secondPlayer := fixture.otherPlayer(firstPlayer)
	firstTeam := fixture.runtime().teamOf[firstPlayer]
	fixture.scriptThrowsFor(map[domain.PlayerID][]domain.YutResult{
		firstPlayer:  {domain.YutDo},
		secondPlayer: {domain.YutDo},
	})

	fixture.throwUntilResolved(t, firstPlayer)
	fixture.driveUntilPlayerOrEnd(t, firstPlayer)

	var entrySpace domain.SpaceID
	for _, piece := range fixture.runtime().game.Snapshot().Pieces {
		if piece.TeamID == firstTeam && piece.State == domain.PieceOnBoard {
			entrySpace = piece.CurrentSpaceID
		}
	}
	if entrySpace == "" {
		t.Fatal("first mover never entered the board")
	}

	fixture.throwUntilResolved(t, secondPlayer)
	fixture.driveUntilPlayerOrEnd(t, secondPlayer)

	captured := fixture.recorder.ofTypes("PIECES_CAPTURED")
	if len(captured) == 0 || len(captured[0].Payload.CapturedPieceIDs) != 1 {
		t.Fatalf("PIECES_CAPTURED = %+v, want the lone opposing piece captured", captured)
	}
	if captured[0].Payload.SpaceID != entrySpace {
		t.Fatalf("capture space = %q, want %q", captured[0].Payload.SpaceID, entrySpace)
	}
	foundExtraThrow := false
	for _, envelope := range fixture.recorder.ofTypes("TURN_STARTED") {
		if envelope.Payload.PlayerID == secondPlayer && envelope.Payload.Phase == "wait_throw" &&
			envelope.Payload.RequiredInput == "throw" {
			foundExtraThrow = true
		}
	}
	if !foundExtraThrow {
		t.Fatal("capture extra throw missing from TURN_STARTED stream")
	}

	game := fixture.runtime().game.Snapshot()
	for _, piece := range game.Pieces {
		if piece.TeamID != firstTeam {
			continue
		}
		if piece.ID == captured[0].Payload.CapturedPieceIDs[0] && piece.State != domain.PieceWaiting {
			t.Fatalf("captured piece %+v did not return to waiting", piece)
		}
	}
}

// Without stacking, pieces sharing one space keep independent states
// (AGENTS.md review checklist: 업기 불가 상태 독립 유지).
func TestStackingDisabledKeepsSameSpacePiecesIndependent(t *testing.T) {
	t.Parallel()

	fixture := newMatchFixture(t, func(settings *room.Settings) {
		settings.StackingEnabled = false
		settings.MovementOrder = room.MovementFIFO
	})
	defer fixture.recorder.close()
	mover := fixture.runtime().currentPlayer()
	opponent := fixture.otherPlayer(mover)
	fixture.scriptThrowsFor(map[domain.PlayerID][]domain.YutResult{
		mover:    {domain.YutDo},
		opponent: {domain.YutGae},
	})

	// mover enters both pieces at "do"; the opponent's gae path never
	// touches that space, so nothing captures in between.
	for attempt := 0; attempt < 4; attempt++ {
		current := fixture.runtime().currentPlayer()
		if current != mover {
			fixture.throwUntilResolved(t, current)
			fixture.driveUntilPlayerOrEnd(t, current)
			continue
		}
		fixture.throwUntilResolved(t, current)
		rt := fixture.runtime()
		snapshot := rt.machine.Snapshot()
		if snapshot.RequiredInput != domain.InputSelectMove {
			continue
		}
		var target domain.PieceID
		for _, piece := range rt.game.Snapshot().Pieces {
			if piece.TeamID == rt.teamOf[mover] && piece.State == domain.PieceWaiting {
				target = piece.ID
				break
			}
		}
		if target == "" {
			t.Fatal("mover has no waiting piece left to enter")
		}
		candidates, _, err := rt.moveCandidates(snapshot)
		if err != nil {
			t.Fatalf("moveCandidates() error = %v", err)
		}
		var tokenID domain.ResultTokenID
		for _, candidate := range candidates {
			if candidate.PieceID == target {
				tokenID = candidate.TokenID
				break
			}
		}
		if tokenID == "" {
			t.Fatalf("target %s is not selectable", target)
		}
		if err := fixture.registry.SelectMove(auth.UserID(mover), fixture.roomID, fixture.matchID, tokenID, target); err != nil {
			t.Fatalf("SELECT_MOVE(%s) error = %v", target, err)
		}
		fixture.driveUntilPlayerOrEnd(t, mover)
	}

	spaceCounts := make(map[domain.SpaceID]int)
	for _, piece := range fixture.runtime().game.Snapshot().Pieces {
		if piece.TeamID == fixture.runtime().teamOf[mover] && piece.State == domain.PieceOnBoard {
			spaceCounts[piece.CurrentSpaceID]++
		}
	}
	coLocated := 0
	for _, count := range spaceCounts {
		if count > coLocated {
			coLocated = count
		}
	}
	if coLocated < 2 {
		t.Fatalf("scenario setup failed: no two mover pieces share a space (%+v)", spaceCounts)
	}

	if findEvent(fixture.recorder.snapshotEvents(), "PIECES_STACKED") != nil {
		t.Fatal("stacking disabled yet PIECES_STACKED emitted")
	}
}

// Backdo reverses along the recorded path history onto the home checkpoint.
func TestBackdoReversesAlongRecordedHistory(t *testing.T) {
	t.Parallel()

	fixture := newMatchFixture(t, func(settings *room.Settings) {
		settings.BackdoEnabled = true
		settings.MovementOrder = room.MovementFIFO
	})
	defer fixture.recorder.close()
	firstPlayer := fixture.runtime().currentPlayer()
	secondPlayer := fixture.otherPlayer(firstPlayer)
	fixture.scriptThrowsFor(map[domain.PlayerID][]domain.YutResult{
		firstPlayer:  {domain.YutDo, domain.YutBackdo},
		secondPlayer: {domain.YutGae},
	})

	// Turn 1: enter a piece at "do".
	fixture.throwUntilResolved(t, firstPlayer)
	fixture.driveUntilPlayerOrEnd(t, firstPlayer)

	// Skip the opponent's turn, then reverse with backdo on turn two.
	current := fixture.runtime().currentPlayer()
	fixture.throwUntilResolved(t, current)
	fixture.driveUntilPlayerOrEnd(t, current)
	before := len(fixture.recorder.snapshotEvents())
	fixture.throwUntilResolved(t, firstPlayer)
	fixture.driveUntilPlayerOrEnd(t, firstPlayer)

	var backdoMove *matchEventEnvelope
	for _, envelope := range fixture.recorder.snapshotEvents()[before:] {
		if envelope.Type == "PIECE_MOVED" && envelope.Payload.MovementKind == "backdo" {
			value := envelope
			backdoMove = &value
		}
	}
	if backdoMove == nil {
		t.Fatal("no backdo PIECE_MOVED recorded")
	}
	reversed := false
	for _, piece := range fixture.runtime().game.Snapshot().Pieces {
		if piece.TeamID == fixture.runtime().teamOf[firstPlayer] &&
			piece.State == domain.PieceHomeCheckpoint && piece.CurrentSpaceID == "chammeogi" {
			reversed = true
		}
	}
	if !reversed {
		t.Fatal("backdo destination missing a piece at chammeogi home checkpoint")
	}
}
