package application

import (
	"errors"
	"math"
	"sort"
	"sync"
	"testing"

	"buk-yutnori/internal/domain"
)

func TestRoomEventSequencesStartAtOneAndRemainRoomScoped(t *testing.T) {
	sequences := NewRoomEventSequences()
	roomOne := domain.RoomID("room-1")
	roomTwo := domain.RoomID("room-2")

	assertBoundary(t, sequences, roomOne, 0)
	assertCommittedSequence(t, sequences, roomOne, 1)
	assertCommittedSequence(t, sequences, roomOne, 2)
	assertCommittedSequence(t, sequences, roomTwo, 1)
	assertBoundary(t, sequences, roomOne, 2)
	assertBoundary(t, sequences, roomTwo, 1)
}

func TestRoomEventSequencesCommitConcurrentlyWithoutGapsOrDuplicates(t *testing.T) {
	sequences := NewRoomEventSequences()
	roomID := domain.RoomID("room-concurrent")
	const commits = 256

	started := make(chan struct{})
	results := make(chan uint64, commits)
	errorsSeen := make(chan error, commits)
	var workers sync.WaitGroup
	workers.Add(commits)
	for range commits {
		go func() {
			defer workers.Done()
			<-started
			sequence, err := sequences.CommitNext(roomID)
			if err != nil {
				errorsSeen <- err
				return
			}
			results <- sequence
		}()
	}

	close(started)
	workers.Wait()
	close(results)
	close(errorsSeen)

	for err := range errorsSeen {
		t.Fatalf("CommitNext() error = %v", err)
	}
	committed := make([]uint64, 0, commits)
	for sequence := range results {
		committed = append(committed, sequence)
	}
	sort.Slice(committed, func(left, right int) bool { return committed[left] < committed[right] })
	if len(committed) != commits {
		t.Fatalf("committed sequence count = %d, want %d", len(committed), commits)
	}
	for index, sequence := range committed {
		want := uint64(index + 1)
		if sequence != want {
			t.Fatalf("committed[%d] = %d, want %d", index, sequence, want)
		}
	}
	assertBoundary(t, sequences, roomID, commits)
}

func TestRoomEventSequencesRejectInvalidRoomWithoutMutation(t *testing.T) {
	sequences := NewRoomEventSequences()

	if _, err := sequences.CommitNext(""); !errors.Is(err, ErrInvalidRoomEventSequence) {
		t.Fatalf("CommitNext() error = %v, want %v", err, ErrInvalidRoomEventSequence)
	}
	if _, err := sequences.Boundary(""); !errors.Is(err, ErrInvalidRoomEventSequence) {
		t.Fatalf("Boundary() error = %v, want %v", err, ErrInvalidRoomEventSequence)
	}
	if len(sequences.values) != 0 {
		t.Fatalf("invalid room mutated values = %+v", sequences.values)
	}
}

func TestRoomEventSequencesRejectExhaustedSequence(t *testing.T) {
	sequences := NewRoomEventSequences()
	roomID := domain.RoomID("room-full")
	sequences.values[roomID] = math.MaxUint64

	if _, err := sequences.CommitNext(roomID); !errors.Is(err, ErrRoomEventSequenceExhausted) {
		t.Fatalf("CommitNext() error = %v, want %v", err, ErrRoomEventSequenceExhausted)
	}
	assertBoundary(t, sequences, roomID, math.MaxUint64)
}

func TestRoomEventSequencesForgetClosedRoomRemovesOnlyThatBoundary(t *testing.T) {
	sequences := NewRoomEventSequences()
	closedRoom := domain.RoomID("room-closed")
	openRoom := domain.RoomID("room-open")
	assertCommittedSequence(t, sequences, closedRoom, 1)
	assertCommittedSequence(t, sequences, openRoom, 1)
	assertCommittedSequence(t, sequences, openRoom, 2)

	sequences.ForgetClosedRoom(closedRoom)

	assertBoundary(t, sequences, closedRoom, 0)
	assertBoundary(t, sequences, openRoom, 2)
	if _, exists := sequences.values[closedRoom]; exists {
		t.Fatal("closed room boundary remains retained")
	}
}

func assertCommittedSequence(t *testing.T, sequences *RoomEventSequences, roomID domain.RoomID, want uint64) {
	t.Helper()
	got, err := sequences.CommitNext(roomID)
	if err != nil {
		t.Fatalf("CommitNext(%q) error = %v", roomID, err)
	}
	if got != want {
		t.Fatalf("CommitNext(%q) = %d, want %d", roomID, got, want)
	}
}

func assertBoundary(t *testing.T, sequences *RoomEventSequences, roomID domain.RoomID, want uint64) {
	t.Helper()
	got, err := sequences.Boundary(roomID)
	if err != nil {
		t.Fatalf("Boundary(%q) error = %v", roomID, err)
	}
	if got != want {
		t.Fatalf("Boundary(%q) = %d, want %d", roomID, got, want)
	}
}
