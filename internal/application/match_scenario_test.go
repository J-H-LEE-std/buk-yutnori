package application

import (
	"bytes"
	"encoding/json"
	"testing"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/room"
	"buk-yutnori/internal/domain/yut"
)

// Seeded whole-game vertical scenario: two humans play a complete match
// through the canonical commands; the runtime must end with GAME_ENDED, the
// post_match waiting-room return, and released started state.
func TestSeededWholeMatchRunsToGameEndedAndReturnsToWaitingRoom(t *testing.T) {
	t.Parallel()

	fixture := newMatchFixture(t, func(settings *room.Settings) {
		settings.MovementOrder = "fifo"
	})
	defer fixture.recorder.close()
	firstPlayer := fixture.runtime().currentPlayer()

	fixture.playWholeMatch(t)
	events := fixture.recorder.snapshotEvents()
	if len(events) < 10 {
		t.Fatalf("unexpectedly short event stream: %d events", len(events))
	}

	assertContiguousRoomSequences(t, events)

	started := fixture.recorder.ofTypes("GAME_STARTED")
	if len(started) != 1 {
		t.Fatalf("GAME_STARTED count = %d, want 1", len(started))
	}
	if started[0].Payload.FirstPlayerID != firstPlayer {
		t.Fatalf("first player = %s, want %s", started[0].Payload.FirstPlayerID, firstPlayer)
	}

	ended := fixture.recorder.ofTypes("GAME_ENDED")
	if len(ended) != 1 || ended[0].Payload.Status != "finished" || ended[0].Payload.WinnerTeamID == nil {
		t.Fatalf("GAME_ENDED events = %+v", ended)
	}
	winner := *ended[0].Payload.WinnerTeamID
	if winner != domain.TeamA && winner != domain.TeamB {
		t.Fatalf("winner = %q", winner)
	}

	roomUpdates := fixture.recorder.ofTypes("ROOM_UPDATED")
	if len(roomUpdates) == 0 || roomUpdates[len(roomUpdates)-1].Payload.Status != "post_match" {
		t.Fatalf("final ROOM_UPDATED = %+v, want post_match after GAME_ENDED", roomUpdates)
	}
	lastEndedSequence := ended[0].Sequence
	for _, envelope := range roomUpdates {
		if envelope.Payload.Status == "post_match" && envelope.Sequence < lastEndedSequence {
			t.Fatalf("post_match broadcast preceded GAME_ENDED")
		}
	}

	turns := fixture.recorder.ofTypes("TURN_STARTED")
	if turns[0].Payload.PlayerID != firstPlayer {
		t.Fatalf("first TURN_STARTED = %+v, want acting first player", turns[0])
	}
	throws := fixture.recorder.ofTypes("YUT_RESULT")
	if len(throws) == 0 {
		t.Fatal("no YUT_RESULT events recorded")
	}
	for index, throw := range throws {
		if throw.Payload.Token == nil || throw.Payload.Token.Result.Validate() != nil {
			t.Fatalf("YUT_RESULT[%d] = %+v lacks a valid token", index, throw)
		}
	}

	// started is released: lobby mutations are accepted again and a rematch
	// window can open after players re-ready.
	rt := fixture.runtime()
	if rt != nil {
		t.Fatal("runtime survived GAME_ENDED")
	}
	detail, err := fixture.registry.Detail(fixture.users[0], fixture.roomID)
	if err != nil {
		t.Fatalf("Detail(post_match) error = %v", err)
	}
	if detail.ActiveStart != nil || detail.ActiveMatch != nil {
		t.Fatalf("post_match detail has active scope: %+v", detail)
	}
	encodedDetail, err := json.Marshal(detail)
	if err != nil {
		t.Fatalf("Marshal(post_match detail) error = %v", err)
	}
	if bytes.Contains(encodedDetail, []byte(`"active_start"`)) || bytes.Contains(encodedDetail, []byte(`"active_match"`)) {
		t.Fatalf("post_match detail serialized active scope: %s", encodedDetail)
	}
	for _, user := range fixture.users {
		if err := fixture.registry.SetReady(user, fixture.roomID, true); err != nil {
			t.Fatalf("SetReady(%s) after match error = %v", user, err)
		}
	}
	if err := fixture.registry.RequestStart(fixture.users[0], fixture.roomID); err != nil {
		t.Fatalf("RequestStart(rematch window) error = %v", err)
	}
}

func assertContiguousRoomSequences(t *testing.T, events []matchEventEnvelope) {
	t.Helper()
	if len(events) == 0 {
		t.Fatal("no events to assert")
	}
	// The recorder subscribes mid-lifecycle, so the first delivered frame is
	// the hub's cached latest ROOM_UPDATED; continuity starts there.
	expected := events[0].Sequence
	for index, envelope := range events {
		if envelope.Sequence != expected {
			t.Fatalf("events[%d] %s sequence = %d, want %d", index, envelope.Type, envelope.Sequence, expected)
		}
		expected++
	}
}

// Throw timeout substitutes the CPU for that entire turn only.
func TestThrowTimeoutSubstitutesCPUForSingleTurn(t *testing.T) {
	t.Parallel()

	fixture := newMatchFixture(t, nil)
	defer fixture.recorder.close()
	rt := fixture.runtime()
	firstPlayer := rt.currentPlayer()

	fixture.clock.Advance(fixture.throwTimeout())

	cpuEvents := fixture.recorder.ofTypes("CPU_CONTROL_STARTED")
	if len(cpuEvents) != 1 || cpuEvents[0].Payload.PlayerID != firstPlayer || cpuEvents[0].Payload.Reason != "timeout" {
		t.Fatalf("CPU_CONTROL_STARTED events = %+v", cpuEvents)
	}
	throws := fixture.recorder.ofTypes("YUT_RESULT")
	if len(throws) == 0 || throws[0].Payload.PlayerID != firstPlayer {
		t.Fatalf("CPU throws = %+v, want at least one by %s", throws, firstPlayer)
	}

	nextTurns := fixture.recorder.ofTypes("TURN_STARTED")
	var handover *matchEventEnvelope
	for index, envelope := range nextTurns {
		if envelope.Payload.PlayerID != firstPlayer {
			handover = &nextTurns[index]
			break
		}
	}
	if handover == nil {
		t.Fatal("CPU turn never handed over to the other player")
	}
	rt = fixture.runtime()
	if rt == nil || rt.cpuControlled {
		t.Fatal("CPU control leaked past the substituted turn")
	}
	if rt.currentPlayer() != handover.Payload.PlayerID {
		t.Fatalf("acting player = %s, want %s", rt.currentPlayer(), handover.Payload.PlayerID)
	}

	// The restored human can act directly on their next turn.
	scripted := fixture.runtime()
	scripted.throwResult = func(yut.Mode) (domain.YutResult, error) {
		return domain.YutGae, nil
	}
	if err := fixture.registry.ThrowYut(auth.UserID(rt.currentPlayer()), fixture.roomID, fixture.matchID); err != nil {
		t.Fatalf("human THROW_YUT after CPU turn error = %v", err)
	}
	handovers := fixture.recorder.ofTypes("CPU_CONTROL_STARTED")
	if len(handovers) != 1 {
		t.Fatalf("unexpected extra CPU substitutions = %+v", handovers)
	}
}

// Move-phase timeout substitutes the CPU while a selection decision is open,
// and only until the turn completes.
func TestMoveTimeoutSubstitutesCPUForOpenSelection(t *testing.T) {
	t.Parallel()

	fixture := newMatchFixture(t, nil)
	defer fixture.recorder.close()
	scripted := fixture.runtime()
	scripted.throwResult = func(yut.Mode) (domain.YutResult, error) { return domain.YutGae, nil }
	firstPlayer := scripted.currentPlayer()

	// Both humans complete one full turn so the second player owns an open
	// selection when their move window expires.
	fixture.driveUntilPlayerOrEnd(t, firstPlayer)
	secondPlayer := fixture.runtime().currentPlayer()
	if secondPlayer == firstPlayer {
		t.Fatal("drive did not advance to the second player")
	}
	if err := fixture.registry.ThrowYut(auth.UserID(secondPlayer), fixture.roomID, fixture.matchID); err != nil {
		t.Fatalf("THROW_YUT(%s) error = %v", secondPlayer, err)
	}
	moveRequired := fixture.recorder.ofTypes("MOVE_REQUIRED")
	if len(moveRequired) == 0 {
		t.Fatal("second player never received a move decision request")
	}

	// Exactly one deadline is armed (the open move window), so one Advance
	// fires exactly one substitution.
	fixture.clock.Advance(fixture.moveTimeout())

	cpuEvents := fixture.recorder.ofTypes("CPU_CONTROL_STARTED")
	if len(cpuEvents) != 1 || cpuEvents[0].Payload.PlayerID != secondPlayer || cpuEvents[0].Payload.Reason != "timeout" {
		t.Fatalf("CPU_CONTROL_STARTED events = %+v", cpuEvents)
	}
	moves := fixture.recorder.ofTypes("PIECE_MOVED")
	if len(moves) == 0 {
		t.Fatal("CPU never applied a piece movement")
	}
	rt := fixture.runtime()
	if rt == nil || rt.cpuControlled {
		t.Fatal("CPU control persisted beyond the substituted turn")
	}
}
