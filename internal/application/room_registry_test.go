package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/room"
	"buk-yutnori/internal/profile"
	"time"
)

func newTestRegistry(t *testing.T) *RoomRegistry {
	t.Helper()

	registry, err := NewRoomRegistry(time.Now)
	if err != nil {
		t.Fatalf("NewRoomRegistry(time.Now) error = %v", err)
	}
	return registry
}

func createDefaultRoom(t *testing.T, registry *RoomRegistry, creator auth.UserID) RoomSummary {
	t.Helper()

	summary, err := registry.Create(CreateRoomInput{
		Creator:  creator,
		Creation: room.Creation{Title: "테스트 방"},
		Settings: room.DefaultSettings(),
		Team:     domain.TeamA,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	return summary
}

func TestCreateAdmitsCreatorAsFirstPlayer(t *testing.T) {
	t.Parallel()

	registry := newTestRegistry(t)
	creator := auth.UserID("user-1")

	summary := createDefaultRoom(t, registry, creator)

	if summary.PlayerCount != 1 {
		t.Fatalf("PlayerCount = %d, want 1", summary.PlayerCount)
	}
	if summary.HasPassword {
		t.Fatal("HasPassword = true, want false without password")
	}
	if summary.Title != "테스트 방" {
		t.Fatalf("Title = %q, want canonical title", summary.Title)
	}
}

func TestListReturnsCreationOrderAndLiveCounts(t *testing.T) {
	t.Parallel()

	registry := newTestRegistry(t)
	first := createDefaultRoom(t, registry, auth.UserID("user-1"))
	second := createDefaultRoom(t, registry, auth.UserID("user-2"))

	summaries := registry.List()
	if len(summaries) != 2 {
		t.Fatalf("len(List()) = %d, want 2", len(summaries))
	}
	if summaries[0].RoomID != first.RoomID || summaries[1].RoomID != second.RoomID {
		t.Fatalf("List order = [%s %s], want creation order", summaries[0].RoomID, summaries[1].RoomID)
	}

	if _, err := registry.Join(JoinRoomInput{
		User:   auth.UserID("user-3"),
		RoomID: first.RoomID,
		Role:   RolePlayer,
		Team:   domain.TeamB,
	}); err != nil {
		t.Fatalf("Join(player) error = %v", err)
	}
	summaries = registry.List()
	if summaries[0].PlayerCount != 2 {
		t.Fatalf("PlayerCount = %d, want live count 2", summaries[0].PlayerCount)
	}
	if summaries[1].PlayerCount != 1 {
		t.Fatalf("PlayerCount = %d, want untouched room count 1", summaries[1].PlayerCount)
	}
}

func TestCreateRejectsCanonicalViolations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   CreateRoomInput
		wantErr error
	}{
		{
			name: "empty title",
			input: CreateRoomInput{
				Creator:  auth.UserID("user-1"),
				Creation: room.Creation{},
				Settings: room.DefaultSettings(),
				Team:     domain.TeamA,
			},
			wantErr: room.ErrInvalidCreation,
		},
		{
			name: "invalid settings",
			input: CreateRoomInput{
				Creator:  auth.UserID("user-1"),
				Creation: room.Creation{Title: "방"},
				Settings: func() room.Settings {
					settings := room.DefaultSettings()
					settings.MaxPlayers = 3
					return settings
				}(),
				Team: domain.TeamA,
			},
			wantErr: room.ErrInvalidSettings,
		},
		{
			name: "invalid team",
			input: CreateRoomInput{
				Creator:  auth.UserID("user-1"),
				Creation: room.Creation{Title: "방"},
				Settings: room.DefaultSettings(),
				Team:     domain.TeamID("C"),
			},
		},
		{
			name: "empty creator",
			input: CreateRoomInput{
				Creation: room.Creation{Title: "방"},
				Settings: room.DefaultSettings(),
				Team:     domain.TeamA,
			},
			wantErr: auth.ErrUnauthenticated,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := newTestRegistry(t)

			_, err := registry.Create(tt.input)
			if err == nil {
				t.Fatal("Create() error = nil, want error")
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("Create() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestJoinPlayerRespectsCapacityTeamAndReadyState(t *testing.T) {
	t.Parallel()

	registry := newTestRegistry(t)
	creator := auth.UserID("creator")

	summary, err := registry.Create(CreateRoomInput{
		Creator:  creator,
		Creation: room.Creation{Title: "작은 방"},
		Settings: func() room.Settings {
			settings := room.DefaultSettings()
			settings.MaxPlayers = 2
			return settings
		}(),
		Team: domain.TeamA,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	joined, err := registry.Join(JoinRoomInput{
		User:   auth.UserID("joiner"),
		RoomID: summary.RoomID,
		Role:   RolePlayer,
		Team:   domain.TeamB,
	})
	if err != nil {
		t.Fatalf("Join(player) error = %v", err)
	}
	if joined.PlayerCount != 2 {
		t.Fatalf("PlayerCount = %d, want 2", joined.PlayerCount)
	}

	overflow, err := registry.Join(JoinRoomInput{
		User:   auth.UserID("third"),
		RoomID: summary.RoomID,
		Role:   RolePlayer,
		Team:   domain.TeamA,
	})
	if !errors.Is(err, room.ErrLobbyFull) {
		t.Fatalf("Join(full room) error = %v, want ErrLobbyFull", err)
	}
	if overflow.PlayerCount != 0 {
		t.Fatalf("rejected join returned PlayerCount = %d, want zero value", overflow.PlayerCount)
	}
}

func TestJoinRejectsDuplicateMembership(t *testing.T) {
	t.Parallel()

	registry := newTestRegistry(t)
	summary := createDefaultRoom(t, registry, auth.UserID("creator"))

	if _, err := registry.Join(JoinRoomInput{
		User:   auth.UserID("creator"),
		RoomID: summary.RoomID,
		Role:   RoleSpectator,
	}); !errors.Is(err, ErrAlreadyMember) {
		t.Fatalf("Join(existing player as spectator) error = %v, want ErrAlreadyMember", err)
	}

	if _, err := registry.Join(JoinRoomInput{
		User:   auth.UserID("spectator"),
		RoomID: summary.RoomID,
		Role:   RoleSpectator,
	}); err != nil {
		t.Fatalf("Join(spectator) error = %v", err)
	}
	if _, err := registry.Join(JoinRoomInput{
		User:   auth.UserID("spectator"),
		RoomID: summary.RoomID,
		Role:   RolePlayer,
		Team:   domain.TeamA,
	}); !errors.Is(err, ErrAlreadyMember) {
		t.Fatalf("Join(existing spectator as player) error = %v, want ErrAlreadyMember", err)
	}
}

func TestJoinEnforcesPasswordContract(t *testing.T) {
	t.Parallel()

	registry := newTestRegistry(t)

	protected, err := registry.Create(CreateRoomInput{
		Creator:  auth.UserID("creator"),
		Creation: room.Creation{Title: "비밀방", Password: "secret01"},
		Settings: room.DefaultSettings(),
		Team:     domain.TeamA,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if !protected.HasPassword {
		t.Fatal("HasPassword = false, want true")
	}

	open := createDefaultRoom(t, registry, auth.UserID("creator-2"))

	tests := []struct {
		name     string
		roomID   domain.RoomID
		password string
		wantErr  error
	}{
		{
			name:     "missing password on protected room",
			roomID:   protected.RoomID,
			password: "",
			wantErr:  ErrPasswordRequired,
		},
		{
			name:     "wrong password on protected room",
			roomID:   protected.RoomID,
			password: "wrong999",
			wantErr:  ErrInvalidRoomPassword,
		},
		{
			name:     "correct password joins",
			roomID:   protected.RoomID,
			password: "secret01",
		},
		{
			name:   "open room ignores supplied password",
			roomID: open.RoomID,
		},
	}

	for index, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user := auth.UserID("user-" + string(rune('a'+index)))

			_, err := registry.Join(JoinRoomInput{
				User:     user,
				RoomID:   tt.roomID,
				Role:     RolePlayer,
				Team:     domain.TeamB,
				Password: tt.password,
			})
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Join(password=%q) error = %v, want %v", tt.password, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Join(password=%q) error = %v, want nil", tt.password, err)
			}
		})
	}
}

func TestJoinSpectatorHonorsCombinedCapacity(t *testing.T) {
	t.Parallel()

	registry := newTestRegistry(t)
	summary := createDefaultRoom(t, registry, auth.UserID("creator"))

	for i := 0; i < combinedMemberCapacity-1; i++ {
		user := auth.UserID("spectator-" + string(rune('a'+i%26)) + string(rune('0'+i/26)))
		if _, err := registry.Join(JoinRoomInput{
			User:   user,
			RoomID: summary.RoomID,
			Role:   RoleSpectator,
		}); err != nil {
			t.Fatalf("Join(spectator %d) error = %v", i, err)
		}
	}

	if _, err := registry.Join(JoinRoomInput{
		User:   auth.UserID("over-capacity"),
		RoomID: summary.RoomID,
		Role:   RoleSpectator,
	}); !errors.Is(err, ErrCombinedCapacityFull) {
		t.Fatalf("Join(beyond capacity) error = %v, want ErrCombinedCapacityFull", err)
	}
}

func TestJoinPlayerHonorsCombinedCapacityBeforeLobbyLimit(t *testing.T) {
	t.Parallel()

	registry := newTestRegistry(t)
	summary := createDefaultRoom(t, registry, auth.UserID("creator"))

	for i := 0; i < combinedMemberCapacity-1; i++ {
		user := auth.UserID("spectator-" + string(rune('a'+i%26)) + string(rune('0'+i/26)))
		if _, err := registry.Join(JoinRoomInput{
			User:   user,
			RoomID: summary.RoomID,
			Role:   RoleSpectator,
		}); err != nil {
			t.Fatalf("Join(spectator %d) error = %v", i, err)
		}
	}

	for i := 0; i < 7; i++ {
		user := auth.UserID("player-" + string(rune('a'+i%26)) + string(rune('0'+i/26)))
		summary, err := registry.Join(JoinRoomInput{
			User:   user,
			RoomID: summary.RoomID,
			Role:   RolePlayer,
			Team:   domain.TeamB,
		})
		if !errors.Is(err, ErrCombinedCapacityFull) {
			t.Fatalf("Join(player %d) error = %v, want ErrCombinedCapacityFull", i, err)
		}
		if summary.PlayerCount != 0 {
			t.Fatalf("rejected player join returned PlayerCount = %d, want zero value", summary.PlayerCount)
		}
	}

	summaries := registry.List()
	if summaries[0].PlayerCount != 1 {
		t.Fatalf("PlayerCount after rejected joins = %d, want unchanged 1", summaries[0].PlayerCount)
	}
}

func TestSpectatorsMayFillCombinedCapacityUnderFullLobby(t *testing.T) {
	t.Parallel()

	registry := newTestRegistry(t)

	summary, err := registry.Create(CreateRoomInput{
		Creator:  auth.UserID("creator"),
		Creation: room.Creation{Title: "꽉 찬 방"},
		Settings: func() room.Settings {
			settings := room.DefaultSettings()
			settings.MaxPlayers = 2
			return settings
		}(),
		Team: domain.TeamA,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := registry.Join(JoinRoomInput{
		User:   auth.UserID("second-player"),
		RoomID: summary.RoomID,
		Role:   RolePlayer,
		Team:   domain.TeamB,
	}); err != nil {
		t.Fatalf("Join(second player into MaxPlayers=2) error = %v", err)
	}
	if _, err := registry.Join(JoinRoomInput{
		User:   auth.UserID("third-player"),
		RoomID: summary.RoomID,
		Role:   RolePlayer,
		Team:   domain.TeamA,
	}); !errors.Is(err, room.ErrLobbyFull) {
		t.Fatalf("Join(third player into MaxPlayers=2) error = %v, want ErrLobbyFull", err)
	}

	for i := 0; i < combinedMemberCapacity-2; i++ {
		user := auth.UserID("spectator-" + string(rune('a'+i%26)) + string(rune('0'+i/26)))
		if _, err := registry.Join(JoinRoomInput{
			User:   user,
			RoomID: summary.RoomID,
			Role:   RoleSpectator,
		}); err != nil {
			t.Fatalf("Join(spectator %d) error = %v", i, err)
		}
	}

	if _, err := registry.Join(JoinRoomInput{
		User:   auth.UserID("final-spectator"),
		RoomID: summary.RoomID,
		Role:   RoleSpectator,
	}); !errors.Is(err, ErrCombinedCapacityFull) {
		t.Fatalf("Join(final spectator beyond combined cap) error = %v, want ErrCombinedCapacityFull", err)
	}
}

func TestJoinUnknownRoomIsNotFound(t *testing.T) {
	t.Parallel()

	registry := newTestRegistry(t)

	_, err := registry.Join(JoinRoomInput{
		User:   auth.UserID("user-1"),
		RoomID: domain.RoomID("00000000000000000000000000000000"),
		Role:   RolePlayer,
		Team:   domain.TeamA,
	})
	if !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("Join(unknown room) error = %v, want ErrRoomNotFound", err)
	}
}

func TestJoinRejectsInvalidRole(t *testing.T) {
	t.Parallel()

	registry := newTestRegistry(t)
	summary := createDefaultRoom(t, registry, auth.UserID("creator"))

	if _, err := registry.Join(JoinRoomInput{
		User:   auth.UserID("user-1"),
		RoomID: summary.RoomID,
		Role:   "owner",
	}); err == nil || errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("Join(invalid role) error = %v, want role validation error", err)
	}
}

func TestChangeTeamAppliesCanonicalRules(t *testing.T) {
	t.Parallel()

	registry := newTestRegistry(t)
	summary := createDefaultRoom(t, registry, auth.UserID("creator"))
	joiner := auth.UserID("joiner")

	if _, err := registry.Join(JoinRoomInput{
		User:   joiner,
		RoomID: summary.RoomID,
		Role:   RolePlayer,
		Team:   domain.TeamB,
	}); err != nil {
		t.Fatalf("Join(joiner) error = %v", err)
	}

	if err := registry.ChangeTeam(joiner, summary.RoomID, domain.TeamA); err != nil {
		t.Fatalf("ChangeTeam(B to A) error = %v", err)
	}
	membership, err := registry.Membership(joiner, summary.RoomID)
	if err != nil {
		t.Fatalf("Membership() error = %v", err)
	}
	if membership.Team != domain.TeamA || membership.Ready {
		t.Fatalf("membership = %+v, want team A not ready", membership)
	}

	if err := registry.ChangeTeam(joiner, summary.RoomID, domain.TeamA); err != nil {
		t.Fatalf("ChangeTeam(same team idempotent) error = %v", err)
	}

	if err := registry.ChangeTeam(joiner, summary.RoomID, domain.TeamID("C")); err == nil {
		t.Fatal("ChangeTeam(invalid team) error = nil, want validation error")
	}
	if _, err := registry.Membership(joiner, domain.RoomID("00000000000000000000000000000000")); !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("Membership(unknown room) error = %v, want ErrRoomNotFound", err)
	}
}

func TestChangeTeamBlocksReadyPlayersAndNonMembers(t *testing.T) {
	t.Parallel()

	registry := newTestRegistry(t)
	summary := createDefaultRoom(t, registry, auth.UserID("creator"))

	spectator := auth.UserID("spectator")
	if _, err := registry.Join(JoinRoomInput{
		User:   spectator,
		RoomID: summary.RoomID,
		Role:   RoleSpectator,
	}); err != nil {
		t.Fatalf("Join(spectator) error = %v", err)
	}
	if err := registry.SetReady(spectator, summary.RoomID, true); !errors.Is(err, room.ErrPlayerNotFound) {
		t.Fatalf("SetReady(spectator) error = %v, want ErrPlayerNotFound", err)
	}

	player := auth.UserID("player")
	if _, err := registry.Join(JoinRoomInput{
		User:   player,
		RoomID: summary.RoomID,
		Role:   RolePlayer,
		Team:   domain.TeamB,
	}); err != nil {
		t.Fatalf("Join(player) error = %v", err)
	}
	if err := registry.SetReady(player, summary.RoomID, true); err != nil {
		t.Fatalf("SetReady(true) error = %v", err)
	}
	if err := registry.ChangeTeam(player, summary.RoomID, domain.TeamA); !errors.Is(err, room.ErrReadyPlayerTeamChange) {
		t.Fatalf("ChangeTeam(ready player) error = %v, want ErrReadyPlayerTeamChange", err)
	}

	if err := registry.SetReady(player, summary.RoomID, false); err != nil {
		t.Fatalf("SetReady(false) error = %v", err)
	}
	if err := registry.ChangeTeam(player, summary.RoomID, domain.TeamA); err != nil {
		t.Fatalf("ChangeTeam(after unready) error = %v", err)
	}

	if err := registry.ChangeTeam(auth.UserID("outsider"), summary.RoomID, domain.TeamA); !errors.Is(err, room.ErrPlayerNotFound) {
		t.Fatalf("ChangeTeam(outsider) error = %v, want ErrPlayerNotFound", err)
	}
	if err := registry.SetReady(auth.UserID("creator"), domain.RoomID("00000000000000000000000000000000"), true); !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("SetReady(unknown room) error = %v, want ErrRoomNotFound", err)
	}
}

func TestSetReadyTogglesOnlyOwnState(t *testing.T) {
	t.Parallel()

	registry := newTestRegistry(t)
	summary := createDefaultRoom(t, registry, auth.UserID("creator"))
	other := auth.UserID("other")
	if _, err := registry.Join(JoinRoomInput{
		User:   other,
		RoomID: summary.RoomID,
		Role:   RolePlayer,
		Team:   domain.TeamB,
	}); err != nil {
		t.Fatalf("Join(other) error = %v", err)
	}

	if err := registry.SetReady(auth.UserID("creator"), summary.RoomID, true); err != nil {
		t.Fatalf("SetReady(creator) error = %v", err)
	}

	creatorMembership, err := registry.Membership(auth.UserID("creator"), summary.RoomID)
	if err != nil {
		t.Fatalf("Membership(creator) error = %v", err)
	}
	otherMembership, err := registry.Membership(other, summary.RoomID)
	if err != nil {
		t.Fatalf("Membership(other) error = %v", err)
	}
	if !creatorMembership.Ready || otherMembership.Ready {
		t.Fatalf("ready states = creator %v other %v, want only creator ready", creatorMembership.Ready, otherMembership.Ready)
	}
}

func TestDetailVisibilityContract(t *testing.T) {
	registry := newTestRegistryWithClock(t, time.Now)
	summary, err := registry.Create(CreateRoomInput{
		Creator:  lobbyCreatorID,
		Creation: room.Creation{Title: "상세 방"},
		Settings: room.DefaultSettings(),
		Team:     domain.TeamA,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := registry.Join(JoinRoomInput{
		User:   auth.UserID(startRosterIDs[1]),
		RoomID: summary.RoomID,
		Role:   RoleSpectator,
	}); err != nil {
		t.Fatalf("Join(spectator) error = %v", err)
	}

	if _, err := registry.Detail(auth.UserID(startRosterIDs[2]), summary.RoomID); !errors.Is(err, ErrNotMember) {
		t.Fatalf("Detail(non-member) error = %v, want ErrNotMember", err)
	}
	if _, err := registry.Detail(lobbyCreatorID, domain.RoomID("00000000000000000000000000000000")); !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("Detail(unknown room) error = %v, want ErrRoomNotFound", err)
	}

	detail, err := registry.Detail(lobbyCreatorID, summary.RoomID)
	if err != nil {
		t.Fatalf("Detail(member) error = %v", err)
	}
	if detail.ActiveStart != nil || detail.ActiveMatch != nil {
		t.Fatalf("idle detail has active scope: %+v", detail)
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		t.Fatalf("Marshal(detail) error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("Unmarshal(detail) error = %v", err)
	}
	if _, present := decoded["active_start"]; present {
		t.Fatalf("windowless response serialized active_start: %s", encoded)
	}
	if _, present := decoded["active_match"]; present {
		t.Fatalf("idle response serialized active_match: %s", encoded)
	}
	if len(detail.Members) != 2 || detail.Members[0].Role != RoleSpectator ||
		detail.Members[0].Nickname != string(startRosterIDs[1]) ||
		detail.Members[1].Team != domain.TeamA || detail.Members[1].Nickname != string(lobbyCreatorID) {
		t.Fatalf("members = %+v, want deterministic spectator-first roster", detail.Members)
	}

	playerTeams := []domain.TeamID{domain.TeamB, domain.TeamA, domain.TeamB}
	for index, user := range []auth.UserID{auth.UserID(startRosterIDs[2]), auth.UserID(startRosterIDs[3]), auth.UserID(startRosterIDs[4])} {
		if _, err := registry.Join(JoinRoomInput{
			User: user, RoomID: summary.RoomID, Role: RolePlayer, Team: playerTeams[index],
		}); err != nil {
			t.Fatalf("Join(player %d) error = %v", index, err)
		}
		if err := registry.SetReady(user, summary.RoomID, true); err != nil {
			t.Fatalf("SetReady(%s) error = %v", user, err)
		}
	}
	if err := registry.SetReady(lobbyCreatorID, summary.RoomID, true); err != nil {
		t.Fatalf("SetReady(host) error = %v", err)
	}
	fixture := startFixture{registry: registry, roomID: summary.RoomID}
	if err := fixture.registry.RequestStart(lobbyCreatorID, summary.RoomID); err != nil {
		t.Fatalf("RequestStart() error = %v", err)
	}
	_, _, activeMatchID := readStartState(registry, summary.RoomID)

	detail, err = registry.Detail(lobbyCreatorID, summary.RoomID)
	if err != nil || detail.ActiveStart == nil || detail.ActiveStart.MatchID != activeMatchID {
		t.Fatalf("open-window detail = %+v error = %v, want active start contract", detail, err)
	}

	for _, user := range []auth.UserID{
		lobbyCreatorID, auth.UserID(startRosterIDs[2]), auth.UserID(startRosterIDs[3]), auth.UserID(startRosterIDs[4]),
	} {
		if err := registry.ConfirmStart(user, summary.RoomID, activeMatchID); err != nil {
			t.Fatalf("ConfirmStart(%s) error = %v", user, err)
		}
	}
	detail, err = registry.Detail(auth.UserID(startRosterIDs[1]), summary.RoomID)
	if err != nil || detail.ActiveStart != nil || detail.ActiveMatch == nil || detail.ActiveMatch.MatchID != activeMatchID {
		t.Fatalf("post-start detail = %+v error = %v, want only active match scope", detail, err)
	}
	encoded, err = json.Marshal(detail)
	if err != nil {
		t.Fatalf("Marshal(post-start detail) error = %v", err)
	}
	if bytes.Contains(encoded, []byte(`"active_start"`)) || !bytes.Contains(encoded, []byte(`"active_match"`)) {
		t.Fatalf("post-start detail scope serialization = %s, want active_match only", encoded)
	}
}

func TestDetailResolvesPersistentNicknamesAndFallsBack(t *testing.T) {
	registry := newTestRegistryWithClock(t, time.Now)
	summary := createDefaultRoom(t, registry, lobbyCreatorID)
	spectator := auth.UserID(startRosterIDs[1])
	if _, err := registry.Join(JoinRoomInput{User: spectator, RoomID: summary.RoomID, Role: RoleSpectator}); err != nil {
		t.Fatalf("Join(spectator) error = %v", err)
	}
	failed := auth.UserID(startRosterIDs[2])
	if _, err := registry.Join(JoinRoomInput{User: failed, RoomID: summary.RoomID, Role: RoleSpectator}); err != nil {
		t.Fatalf("Join(failed-profile spectator) error = %v", err)
	}
	profiles := &snapshotProfileStore{values: map[auth.UserID]profile.Profile{
		lobbyCreatorID: {UserID: lobbyCreatorID, Nickname: "가나다", Public: true},
		// A record for another account must not become the spectator display name.
		spectator: {UserID: lobbyCreatorID, Nickname: "나다라", Public: false},
	}, errors: map[auth.UserID]error{failed: errors.New("profile store unavailable")}}
	if err := registry.AttachProfileStore(profiles); err != nil {
		t.Fatalf("AttachProfileStore() error = %v", err)
	}

	detail, err := registry.Detail(lobbyCreatorID, summary.RoomID)
	if err != nil {
		t.Fatalf("Detail() error = %v", err)
	}
	byUser := make(map[auth.UserID]string, len(detail.Members))
	for _, member := range detail.Members {
		byUser[member.UserID] = member.Nickname
	}
	if byUser[lobbyCreatorID] != "가나다" {
		t.Fatalf("configured nickname = %q, want 가나다", byUser[lobbyCreatorID])
	}
	if byUser[spectator] != string(spectator) {
		t.Fatalf("mismatched profile fallback = %q, want %q", byUser[spectator], spectator)
	}
	if byUser[failed] != string(failed) {
		t.Fatalf("failed profile fallback = %q, want %q", byUser[failed], failed)
	}
}

func TestDetailRevalidatesMembershipAfterProfileLookup(t *testing.T) {
	registry := newTestRegistryWithClock(t, time.Now)
	summary := createDefaultRoom(t, registry, lobbyCreatorID)
	spectator := auth.UserID(startRosterIDs[1])
	if _, err := registry.Join(JoinRoomInput{User: spectator, RoomID: summary.RoomID, Role: RoleSpectator}); err != nil {
		t.Fatalf("Join(spectator) error = %v", err)
	}
	profiles := &blockingSnapshotProfileStore{started: make(chan struct{}), release: make(chan struct{})}
	if err := registry.AttachProfileStore(profiles); err != nil {
		t.Fatalf("AttachProfileStore() error = %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := registry.Detail(spectator, summary.RoomID)
		done <- err
	}()
	<-profiles.started
	registry.mutex.Lock()
	delete(registry.rooms[summary.RoomID].spectators, spectator)
	registry.mutex.Unlock()
	close(profiles.release)
	if err := <-done; !errors.Is(err, ErrNotMember) {
		t.Fatalf("Detail() after membership changed = %v, want %v", err, ErrNotMember)
	}
}

func TestDetailRetriesProfileLookupWhenRosterChanges(t *testing.T) {
	registry := newTestRegistryWithClock(t, time.Now)
	summary := createDefaultRoom(t, registry, lobbyCreatorID)
	spectator := auth.UserID(startRosterIDs[1])
	if _, err := registry.Join(JoinRoomInput{User: spectator, RoomID: summary.RoomID, Role: RoleSpectator}); err != nil {
		t.Fatalf("Join(spectator) error = %v", err)
	}
	joiner := auth.UserID(startRosterIDs[2])
	profiles := &roomDetailBlockingProfileStore{
		snapshotProfileStore: snapshotProfileStore{values: map[auth.UserID]profile.Profile{
			lobbyCreatorID: {UserID: lobbyCreatorID, Nickname: "가나다", Public: true},
			spectator:      {UserID: spectator, Nickname: "나다라", Public: true},
			joiner:         {UserID: joiner, Nickname: "새 관전자", Public: true},
		}},
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	if err := registry.AttachProfileStore(profiles); err != nil {
		t.Fatalf("AttachProfileStore() error = %v", err)
	}

	details := make(chan RoomDetailSnapshot, 1)
	errs := make(chan error, 1)
	go func() {
		detail, err := registry.Detail(lobbyCreatorID, summary.RoomID)
		details <- detail
		errs <- err
	}()
	<-profiles.started
	if _, err := registry.Join(JoinRoomInput{User: joiner, RoomID: summary.RoomID, Role: RoleSpectator}); err != nil {
		t.Fatalf("Join(joiner) error = %v", err)
	}
	close(profiles.release)
	if err := <-errs; err != nil {
		t.Fatalf("Detail() error = %v", err)
	}
	for _, member := range (<-details).Members {
		if member.UserID == joiner && member.Nickname == "새 관전자" {
			return
		}
	}
	t.Fatal("Detail() omitted the resolved nickname for a member that joined during profile lookup")
}

func TestDetailBoundsRetriesDuringRosterChurn(t *testing.T) {
	registry := newTestRegistryWithClock(t, time.Now)
	summary := createDefaultRoom(t, registry, lobbyCreatorID)
	firstJoiner := auth.UserID(startRosterIDs[1])
	secondJoiner := auth.UserID(startRosterIDs[2])
	profiles := &roomDetailRetryProfileStore{
		snapshotProfileStore: snapshotProfileStore{values: map[auth.UserID]profile.Profile{
			lobbyCreatorID: {UserID: lobbyCreatorID, Nickname: "가나다", Public: true},
			firstJoiner:    {UserID: firstJoiner, Nickname: "나다라", Public: true},
			secondJoiner:   {UserID: secondJoiner, Nickname: "마바사", Public: true},
		}},
		started: make(chan struct{}, roomDetailNicknameLookupAttempts),
		release: make(chan struct{}, roomDetailNicknameLookupAttempts),
	}
	if err := registry.AttachProfileStore(profiles); err != nil {
		t.Fatalf("AttachProfileStore() error = %v", err)
	}

	details := make(chan RoomDetailSnapshot, 1)
	errs := make(chan error, 1)
	go func() {
		detail, err := registry.Detail(lobbyCreatorID, summary.RoomID)
		details <- detail
		errs <- err
	}()
	<-profiles.started
	if _, err := registry.Join(JoinRoomInput{User: firstJoiner, RoomID: summary.RoomID, Role: RoleSpectator}); err != nil {
		t.Fatalf("Join(first joiner) error = %v", err)
	}
	profiles.release <- struct{}{}
	<-profiles.started
	if _, err := registry.Join(JoinRoomInput{User: secondJoiner, RoomID: summary.RoomID, Role: RoleSpectator}); err != nil {
		t.Fatalf("Join(second joiner) error = %v", err)
	}
	profiles.release <- struct{}{}
	if err := <-errs; err != nil {
		t.Fatalf("Detail() error = %v", err)
	}
	for _, member := range (<-details).Members {
		if member.UserID == secondJoiner && member.Nickname == string(secondJoiner) {
			return
		}
	}
	t.Fatal("Detail() did not return the final roster with a safe fallback after bounded retries")
}

type roomDetailBlockingProfileStore struct {
	snapshotProfileStore
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (store *roomDetailBlockingProfileStore) Lookup(ctx context.Context, userID auth.UserID) (profile.Profile, error) {
	shouldWait := false
	store.once.Do(func() {
		close(store.started)
		shouldWait = true
	})
	if shouldWait {
		select {
		case <-store.release:
		case <-ctx.Done():
			return profile.Profile{}, ctx.Err()
		}
	}
	return store.snapshotProfileStore.Lookup(ctx, userID)
}

type roomDetailRetryProfileStore struct {
	snapshotProfileStore
	started chan struct{}
	release chan struct{}
	mutex   sync.Mutex
	lookups int
}

func (store *roomDetailRetryProfileStore) Lookup(ctx context.Context, userID auth.UserID) (profile.Profile, error) {
	store.mutex.Lock()
	store.lookups++
	lookup := store.lookups
	store.mutex.Unlock()
	if lookup <= roomDetailNicknameLookupAttempts {
		store.started <- struct{}{}
		select {
		case <-store.release:
		case <-ctx.Done():
			return profile.Profile{}, ctx.Err()
		}
	}
	return store.snapshotProfileStore.Lookup(ctx, userID)
}
