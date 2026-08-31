package application

import (
	"errors"
	"testing"
	"time"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/room"
	"buk-yutnori/internal/storage"
)

func TestHostCanStartSoloMatchWithAutomaticallyConfirmedCPUPlayer(t *testing.T) {
	t.Parallel()
	clock := &manualClock{current: time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)}
	registry := newTestRegistryWithClock(t, clock.Now)
	summary, err := registry.Create(CreateRoomInput{Creator: lobbyCreatorID, Creation: room.Creation{Title: "CPU 테스트"}, Settings: room.DefaultSettings(), Team: domain.TeamA})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	cpuID, err := registry.AddCPUPlayer(lobbyCreatorID, summary.RoomID, domain.TeamB)
	if err != nil {
		t.Fatalf("AddCPUPlayer() error = %v", err)
	}
	if player, ok := registry.rooms[summary.RoomID].lobby.Player(cpuID); !ok || !player.CPU || !player.Ready || player.Team != domain.TeamB {
		t.Fatalf("CPU player = %+v, exists=%v", player, ok)
	}
	if err := registry.SetReady(lobbyCreatorID, summary.RoomID, true); err != nil {
		t.Fatalf("SetReady(host) error = %v", err)
	}
	if err := registry.RequestStart(lobbyCreatorID, summary.RoomID); err != nil {
		t.Fatalf("RequestStart() error = %v", err)
	}
	matchID := readActiveStart(t, registry, summary.RoomID).MatchID
	if err := registry.ConfirmStart(lobbyCreatorID, summary.RoomID, matchID); err != nil {
		t.Fatalf("ConfirmStart(host) error = %v", err)
	}
	if !registry.rooms[summary.RoomID].started || registry.rooms[summary.RoomID].runtime == nil {
		t.Fatal("human confirmation did not start the CPU match")
	}
	if !registry.rooms[summary.RoomID].runtime.cpuPlayers[cpuID] {
		t.Fatal("runtime lost CPU roster identity")
	}
	if err := registry.RemoveCPUPlayer(lobbyCreatorID, summary.RoomID, cpuID); !errors.Is(err, ErrRoomAlreadyStarted) {
		t.Fatalf("RemoveCPUPlayer(in match) error = %v, want ErrRoomAlreadyStarted", err)
	}
}

func TestMatchResultExcludesLobbyCPUPlayerButKeepsHumanOutcome(t *testing.T) {
	t.Parallel()
	human := domain.PlayerID(matchHostID)
	cpu := domain.PlayerID("cpu-1")
	result := matchResultForRuntime(&matchRuntime{
		order: []domain.PlayerID{human, cpu}, teamOf: map[domain.PlayerID]domain.TeamID{human: domain.TeamA, cpu: domain.TeamB},
		cpuPlayers: map[domain.PlayerID]bool{cpu: true},
	}, domain.TeamA)
	if len(result.Winners) != 1 || result.Winners[0] != auth.UserID(human) || len(result.Losers) != 0 {
		t.Fatalf("CPU-excluded result = %+v", result)
	}
	if err := result.Validate(); err != nil {
		t.Fatalf("solo human MatchResult.Validate() error = %v", err)
	}
	if err := (storage.MatchResult{}).Validate(); err == nil {
		t.Fatal("empty MatchResult.Validate() error = nil")
	}
}

func TestOnlyHostCanAlterCPULobbyPlayers(t *testing.T) {
	t.Parallel()
	registry := newTestRegistry(t)
	summary := createDefaultRoom(t, registry, auth.UserID("host"))
	if _, err := registry.AddCPUPlayer(auth.UserID("outsider"), summary.RoomID, domain.TeamB); !errors.Is(err, ErrNotRoomHost) {
		t.Fatalf("non-host AddCPUPlayer() error = %v, want ErrNotRoomHost", err)
	}
	cpuID, err := registry.AddCPUPlayer(auth.UserID("host"), summary.RoomID, domain.TeamB)
	if err != nil {
		t.Fatalf("host AddCPUPlayer() error = %v", err)
	}
	if err := registry.RemoveCPUPlayer(auth.UserID("outsider"), summary.RoomID, cpuID); !errors.Is(err, ErrNotRoomHost) {
		t.Fatalf("non-host RemoveCPUPlayer() error = %v, want ErrNotRoomHost", err)
	}
}
