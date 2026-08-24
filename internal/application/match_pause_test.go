package application

import (
	"errors"
	"testing"
	"time"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/yut"
)

// Pausing preserves the active turn window kind plus remaining milliseconds,
// blocks every live command, and shows paused state in the snapshot.
func TestHostPausePreservesWindowAndBlocksCommands(t *testing.T) {
	t.Parallel()

	fixture := newMatchFixture(t, nil)
	defer fixture.recorder.close()
	host := fixture.users[0]
	guest := fixture.users[1]

	if err := fixture.registry.PauseGame(guest, fixture.roomID, fixture.matchID, 5); !errors.Is(err, ErrNotRoomHost) {
		t.Fatalf("guest PauseGame = %v, want ErrNotRoomHost", err)
	}
	if err := fixture.registry.PauseGame(auth.UserID(host), fixture.roomID, fixture.matchID, 0); err == nil {
		t.Fatal("zero-minute pause unexpectedly accepted")
	}
	if err := fixture.registry.PauseGame(auth.UserID(host), fixture.roomID, fixture.matchID, 31); err == nil {
		t.Fatal("31-minute pause unexpectedly accepted")
	}
	if err := fixture.registry.PauseGame(auth.UserID(host), fixture.roomID, fixture.matchID, 5); err != nil {
		t.Fatalf("PauseGame(host) error = %v", err)
	}

	paused := fixture.recorder.ofTypes("GAME_PAUSED")
	if len(paused) != 1 || paused[0].Payload.Reason != "host_request" {
		t.Fatalf("GAME_PAUSED = %+v", paused)
	}

	rt := fixture.runtime()
	if !rt.paused || !rt.pauseUsed || rt.preservedTimerKind != matchTimerKindThrow {
		t.Fatalf("runtime pause state = %+v", rt)
	}
	if rt.preservedRemaining <= 0 {
		t.Fatalf("preserved remaining = %v, want the armed throw window", rt.preservedRemaining)
	}

	if err := fixture.registry.ThrowYut(auth.UserID(rt.currentPlayer()), fixture.roomID, fixture.matchID); !errors.Is(err, ErrMatchPaused) {
		t.Fatalf("THROW_YUT while paused = %v, want ErrMatchPaused", err)
	}
	if err := fixture.registry.ResumeGame(guest, fixture.roomID, fixture.matchID); !errors.Is(err, ErrNotRoomHost) {
		t.Fatalf("guest ResumeGame = %v, want ErrNotRoomHost", err)
	}

	boundary := boundaryOf(t, fixture.registry, fixture.roomID)
	entry := fixture.registry.rooms[fixture.roomID]
	snapshot, snapErr := fixture.registry.assembleGameSnapshotLocked(entry, boundary)
	if snapErr != nil {
		t.Fatalf("assemble snapshot error = %v", snapErr)
	}
	if snapshot.CurrentTurn.Phase != "paused" || snapshot.CurrentTurn.Timer.Phase != "paused" ||
		snapshot.CurrentTurn.Timer.RemainingMS == 0 || snapshot.CurrentTurn.Timer.DeadlineAt == nil {
		t.Fatalf("paused snapshot turn = %+v", snapshot.CurrentTurn)
	}
	if !snapshot.Pause.Used || !snapshot.Pause.Paused || snapshot.Pause.EndsAt == nil {
		t.Fatalf("paused snapshot pause block = %+v", snapshot.Pause)
	}
}

// Resuming early restores the preserved movement window: the same deadline
// later expires into the canonical CPU substitution.
func TestResumeRestoresPreservedMoveWindow(t *testing.T) {
	t.Parallel()

	fixture := newMatchFixture(t, nil)
	defer fixture.recorder.close()
	scripted := fixture.runtime()
	scripted.throwResult = func(yut.Mode) (domain.YutResult, error) { return domain.YutGae, nil }
	firstPlayer := scripted.currentPlayer()
	fixture.driveUntilPlayerOrEnd(t, firstPlayer)

	secondPlayer := fixture.runtime().currentPlayer()
	if err := fixture.registry.ThrowYut(auth.UserID(secondPlayer), fixture.roomID, fixture.matchID); err != nil {
		t.Fatalf("THROW_YUT(%s) error = %v", secondPlayer, err)
	}
	if got := fixture.runtime().timerKind; got != matchTimerKindMove {
		t.Fatalf("armed window = %q, want move", got)
	}

	if err := fixture.registry.PauseGame(auth.UserID(secondPlayer), fixture.roomID, fixture.matchID, 5); err != nil {
		t.Fatalf("PauseGame error = %v", err)
	}
	if err := fixture.registry.ResumeGame(auth.UserID(secondPlayer), fixture.roomID, fixture.matchID); err != nil {
		t.Fatalf("ResumeGame error = %v", err)
	}

	resumed := fixture.recorder.ofTypes("GAME_RESUMED")
	if len(resumed) != 1 || resumed[0].Payload.Reason != "host_request" {
		t.Fatalf("GAME_RESUMED = %+v", resumed)
	}
	if got := fixture.runtime().timerKind; got != matchTimerKindMove {
		t.Fatalf("restored window = %q, want move", got)
	}

	// The restored window still belongs to the same player's decision and
	// expires into the CPU substitution exactly once.
	fixture.clock.Advance(fixture.moveTimeout())
	cpuEvents := fixture.recorder.ofTypes("CPU_CONTROL_STARTED")
	if len(cpuEvents) != 1 || cpuEvents[0].Payload.PlayerID != secondPlayer {
		t.Fatalf("CPU substitution after resume = %+v", cpuEvents)
	}
}

// The scheduled pause deadline auto-resumes the match.
func TestPauseAutoResumesOnExpiry(t *testing.T) {
	t.Parallel()

	fixture := newMatchFixture(t, nil)
	defer fixture.recorder.close()
	host := fixture.users[0]
	rt := fixture.runtime()
	current := rt.currentPlayer()
	scripted := rt
	scripted.throwResult = func(yut.Mode) (domain.YutResult, error) { return domain.YutGae, nil }

	if err := fixture.registry.PauseGame(auth.UserID(host), fixture.roomID, fixture.matchID, 2); err != nil {
		t.Fatalf("PauseGame error = %v", err)
	}
	fixture.clock.Advance(2 * time.Minute)

	resumed := fixture.recorder.ofTypes("GAME_RESUMED")
	if len(resumed) != 1 || resumed[0].Payload.Reason != "pause_expired" {
		t.Fatalf("GAME_RESUMED = %+v, want pause_expired", resumed)
	}
	rt = fixture.runtime()
	if rt.paused {
		t.Fatal("match stayed paused after the scheduled expiry")
	}
	if err := fixture.registry.ThrowYut(auth.UserID(current), fixture.roomID, fixture.matchID); err != nil {
		t.Fatalf("THROW_YUT after auto-resume error = %v", err)
	}

	// The per-match budget stays consumed across resumes.
	if err := fixture.registry.PauseGame(auth.UserID(host), fixture.roomID, fixture.matchID, 1); !errors.Is(err, ErrMatchPauseUsed) {
		t.Fatalf("second PauseGame = %v, want ErrMatchPauseUsed", err)
	}
}

// Resuming without an active pause is a deterministic rejection.
func TestResumeWithoutPauseRejected(t *testing.T) {
	t.Parallel()

	fixture := newMatchFixture(t, nil)
	defer fixture.recorder.close()
	if err := fixture.registry.ResumeGame(fixture.users[0], fixture.roomID, fixture.matchID); !errors.Is(err, ErrMatchNotPaused) {
		t.Fatalf("ResumeGame without pause = %v, want ErrMatchNotPaused", err)
	}
}

// After the one-time pause is consumed and resumed, reconnecting clients must
// still see pause.used=true so the spent budget stays visible (Claude review
// blocker, issue #86).
func TestSnapshotShowsSpentPauseAfterResume(t *testing.T) {
	t.Parallel()

	fixture := newMatchFixture(t, nil)
	defer fixture.recorder.close()
	host := fixture.users[0]

	if err := fixture.registry.PauseGame(auth.UserID(host), fixture.roomID, fixture.matchID, 5); err != nil {
		t.Fatalf("PauseGame error = %v", err)
	}
	if err := fixture.registry.ResumeGame(auth.UserID(host), fixture.roomID, fixture.matchID); err != nil {
		t.Fatalf("ResumeGame error = %v", err)
	}

	boundary := boundaryOf(t, fixture.registry, fixture.roomID)
	entry := fixture.registry.rooms[fixture.roomID]
	snapshot, snapErr := fixture.registry.assembleGameSnapshotLocked(entry, boundary)
	if snapErr != nil {
		t.Fatalf("assemble snapshot error = %v", snapErr)
	}
	if !snapshot.Pause.Used {
		t.Fatalf("snapshot.pause.used = false after the one-time pause was consumed: %+v", snapshot.Pause)
	}
	if snapshot.Pause.Paused || snapshot.Pause.EndsAt != nil {
		t.Fatalf("snapshot.pause still reports an active pause: %+v", snapshot.Pause)
	}
	if snapshot.CurrentTurn.Phase == "paused" {
		t.Fatal("resumed match still reports phase=paused")
	}
	// The server-side budget stays spent regardless.
	rt := fixture.runtime()
	if !rt.pauseUsed || rt.paused {
		t.Fatalf("runtime pause state = used=%v paused=%v", rt.pauseUsed, rt.paused)
	}
}
