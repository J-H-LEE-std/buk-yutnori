// Canonical game_snapshot assembly and real-data RECONNECT support
// (issue #82). The JSON shapes mirror schemas/game_snapshot.schema.json
// field-for-field so the emitted document validates against the schema.

package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/match"
	"buk-yutnori/internal/domain/room"
	"buk-yutnori/internal/domain/turn"
	"buk-yutnori/internal/profile"
	"buk-yutnori/internal/protocol"
	"buk-yutnori/internal/storage"
)

// ErrClientSequenceAhead rejects a RECONNECT whose last_sequence is beyond
// the server boundary; clients must resynchronize from zero (ADR-0009).
var ErrClientSequenceAhead = errors.New("client sequence is ahead of the room boundary")

// ErrStoredEventsNotContiguous reports a corrupted canonical store whose rows
// no longer form an unbroken sequence; such rows are never served as replay.
var ErrStoredEventsNotContiguous = errors.New("stored room events are not contiguous")

const (
	snapshotStatusActive = "active"

	snapshotTimerPhaseThrow = "throw"
	snapshotTimerPhaseMove  = "move"
	snapshotTimerPhaseNone  = "none"

	participantPermissionControl = "control_game"
	participantPermissionChat    = "chat"
	participantPermissionPause   = "pause_game"
	participantPermissionManage  = "manage_room"

	cpuControlReasonDisconnected = "disconnected"
)

type gameSnapshotJSON struct {
	RoomID         domain.RoomID           `json:"room_id"`
	MatchID        domain.MatchID          `json:"match_id"`
	Sequence       uint64                  `json:"sequence"`
	Status         string                  `json:"status"`
	Teams          []snapshotTeamJSON      `json:"teams"`
	Participants   []snapshotParticipant   `json:"participants"`
	CurrentTurn    snapshotCurrentTurnJSON `json:"current_turn"`
	ResultQueue    []turnTokenJSON         `json:"result_queue"`
	Pieces         []snapshotPieceJSON     `json:"pieces"`
	Stacks         []snapshotStackJSON     `json:"stacks"`
	PositionGroups []snapshotPositionGroup `json:"position_groups"`
	Buk            snapshotBukJSON         `json:"buk"`
	Pause          snapshotPauseJSON       `json:"pause"`
}

type snapshotTeamJSON struct {
	TeamID    domain.TeamID     `json:"team_id"`
	PlayerIDs []domain.PlayerID `json:"player_ids"`
	TurnOrder []domain.PlayerID `json:"turn_order"`
}

type snapshotParticipant struct {
	UserID      auth.UserID            `json:"user_id"`
	Nickname    string                 `json:"nickname"`
	Role        string                 `json:"role"`
	TeamID      *domain.TeamID         `json:"team_id"`
	Permissions []string               `json:"permissions"`
	Connected   bool                   `json:"connected"`
	CPUControl  snapshotCPUControlJSON `json:"cpu_control"`
}

type snapshotCPUControlJSON struct {
	Active bool    `json:"active"`
	Reason *string `json:"reason"`
}

type snapshotTimerJSON struct {
	Phase       string  `json:"phase"`
	RemainingMS uint64  `json:"remaining_ms"`
	DeadlineAt  *string `json:"deadline_at"`
}

type snapshotCurrentTurnJSON struct {
	PlayerID      *domain.PlayerID              `json:"player_id"`
	Phase         string                        `json:"phase"`
	RequiredInput string                        `json:"required_input"`
	MoveRequest   *protocol.MoveRequiredPayload `json:"move_request"`
	Timer         snapshotTimerJSON             `json:"timer"`
}

type turnTokenJSON struct {
	TokenID             domain.ResultTokenID `json:"token_id"`
	Result              domain.YutResult     `json:"result"`
	Origin              domain.ResultOrigin  `json:"origin"`
	GeneratedByPlayerID domain.PlayerID      `json:"generated_by_player_id"`
}

type snapshotPieceJSON struct {
	PieceID             domain.PieceID    `json:"piece_id"`
	TeamID              domain.TeamID     `json:"team_id"`
	State               domain.PieceState `json:"state"`
	CurrentSpaceID      *domain.SpaceID   `json:"current_space_id"`
	StackID             *string           `json:"stack_id"`
	PositionGroupID     *string           `json:"position_group_id"`
	ActualPreviousSpace *domain.SpaceID   `json:"actual_previous_space"`
}

type snapshotStackJSON struct {
	StackID             string           `json:"stack_id"`
	TeamID              domain.TeamID    `json:"team_id"`
	SpaceID             domain.SpaceID   `json:"space_id"`
	PieceIDs            []domain.PieceID `json:"piece_ids"`
	ActualPreviousSpace *domain.SpaceID  `json:"actual_previous_space"`
}

type snapshotPositionGroup struct {
	GroupID  string           `json:"group_id"`
	TeamID   domain.TeamID    `json:"team_id"`
	SpaceID  domain.SpaceID   `json:"space_id"`
	PieceIDs []domain.PieceID `json:"piece_ids"`
}

type snapshotBukJSON struct {
	Enabled            bool            `json:"enabled"`
	DestinationSpaceID *domain.SpaceID `json:"destination_space_id"`
}

type snapshotPauseJSON struct {
	Used   bool    `json:"used"`
	Paused bool    `json:"paused"`
	EndsAt *string `json:"ends_at"`
}

// assembleGameSnapshotLocked builds the atomic snapshot at the given room
// sequence boundary while the registry mutex is held.
func (registry *RoomRegistry) assembleGameSnapshotLocked(entry *registeredRoom, sequence uint64) (gameSnapshotJSON, error) {
	return registry.assembleGameSnapshotWithNicknamesLocked(entry, sequence, nil)
}

// assembleGameSnapshotWithNicknamesLocked builds a snapshot with display names
// resolved before the registry mutex is acquired. Missing entries deliberately
// fall back to their stable user_id.
func (registry *RoomRegistry) assembleGameSnapshotWithNicknamesLocked(entry *registeredRoom, sequence uint64, nicknames map[auth.UserID]string) (gameSnapshotJSON, error) {
	rt := entry.runtime
	if rt == nil {
		return gameSnapshotJSON{}, fmt.Errorf("%w: assembled game snapshot requires a live runtime", ErrInvalidConfiguration)
	}
	now := registry.matchClock.Now()
	game := rt.game.Snapshot()
	machine := rt.machine.Snapshot()

	players := entry.lobby.Players()
	teams := make([]snapshotTeamJSON, 0, 2)
	for _, team := range []domain.TeamID{domain.TeamA, domain.TeamB} {
		ids := make([]domain.PlayerID, 0, len(players))
		for id, player := range players {
			if player.Team == team {
				ids = append(ids, id)
			}
		}
		sort.Slice(ids, func(left, right int) bool { return ids[left] < ids[right] })
		teamOrder := make([]domain.PlayerID, 0, len(ids))
		for _, playerID := range rt.order {
			for _, id := range ids {
				if id == playerID {
					teamOrder = append(teamOrder, playerID)
					break
				}
			}
		}
		teams = append(teams, snapshotTeamJSON{
			TeamID:    team,
			PlayerIDs: ids,
			TurnOrder: teamOrder,
		})
	}

	participants := make([]snapshotParticipant, 0, len(players)+len(entry.spectators))
	currentPlayer := rt.currentPlayer()
	playerIDs := make([]domain.PlayerID, 0, len(players))
	for id := range players {
		playerIDs = append(playerIDs, id)
	}
	sort.Slice(playerIDs, func(left, right int) bool { return playerIDs[left] < playerIDs[right] })
	for _, id := range playerIDs {
		player := players[id]
		team := player.Team
		permissions := []string{participantPermissionControl, participantPermissionChat}
		if player.CPU {
			permissions = []string{}
		}
		if auth.UserID(id) == entry.host {
			permissions = append(permissions, participantPermissionPause, participantPermissionManage)
		}
		cpuControl := snapshotCPUControlJSON{Active: false}
		if player.CPU {
			reason := cpuControlReasonLobbyPlayer
			cpuControl = snapshotCPUControlJSON{Active: true, Reason: &reason}
		} else if rt.cpuControlled && id == currentPlayer {
			reason := cpuControlReasonTimeout
			cpuControl = snapshotCPUControlJSON{Active: true, Reason: &reason}
		}
		userID := auth.UserID(id)
		nickname := snapshotNickname(nicknames, userID)
		if player.CPU {
			nickname = "CPU"
		}
		participants = append(participants, snapshotParticipant{
			UserID:      userID,
			Nickname:    nickname,
			Role:        RolePlayer,
			TeamID:      &team,
			Permissions: permissions,
			Connected:   !player.CPU,
			CPUControl:  cpuControl,
		})
	}
	spectatorIDs := make([]auth.UserID, 0, len(entry.spectators))
	for id := range entry.spectators {
		spectatorIDs = append(spectatorIDs, id)
	}
	sort.Slice(spectatorIDs, func(left, right int) bool { return spectatorIDs[left] < spectatorIDs[right] })
	for _, id := range spectatorIDs {
		participants = append(participants, snapshotParticipant{
			UserID:      id,
			Nickname:    snapshotNickname(nicknames, id),
			Role:        RoleSpectator,
			TeamID:      nil,
			Permissions: []string{participantPermissionChat},
			Connected:   true,
			CPUControl:  snapshotCPUControlJSON{},
		})
	}

	queue := make([]turnTokenJSON, 0, len(machine.ResultQueue))
	for _, token := range machine.ResultQueue {
		queue = append(queue, turnTokenJSON{
			TokenID:             token.ID,
			Result:              token.Result,
			Origin:              token.Origin,
			GeneratedByPlayerID: token.GeneratedByPlayerID,
		})
	}

	pieces, stacks, groups := buildPieceViews(rt.settings, game)
	turnPlayer := currentPlayer
	timer := buildTimerView(rt, now)
	phase := string(machine.Phase)
	moveRequest, err := rt.snapshotMoveRequest(machine)
	if err != nil {
		return gameSnapshotJSON{}, err
	}
	// Used tracks per-match consumption even after resume so reconnecting
	// clients see that the one-time host pause is spent (docs/03 경기당 1회).
	pauseView := snapshotPauseJSON{Used: rt.pauseUsed}
	if rt.paused || rt.storagePaused {
		phase = string(domain.TurnPaused)
		timer = snapshotTimerJSON{
			Phase:       "paused",
			RemainingMS: uint64(rt.preservedRemaining.Milliseconds()),
			DeadlineAt:  pauseEndsAtPointer(rt),
		}
		pauseView.Paused = true
		if rt.paused {
			// Only a host pause carries a scheduled auto-resume instant; a
			// pure storage-failure pause has no deadline to expose.
			endsAt := rt.pauseEndsAt.UTC().Format(time.RFC3339)
			pauseView.EndsAt = &endsAt
		}
	}

	return gameSnapshotJSON{
		RoomID:       rt.roomID,
		MatchID:      rt.matchID,
		Sequence:     sequence,
		Status:       snapshotStatusActive,
		Teams:        teams,
		Participants: participants,
		CurrentTurn: snapshotCurrentTurnJSON{
			PlayerID:      &turnPlayer,
			Phase:         phase,
			RequiredInput: string(machine.RequiredInput),
			MoveRequest:   moveRequest,
			Timer:         timer,
		},
		ResultQueue:    queue,
		Pieces:         pieces,
		Stacks:         stacks,
		PositionGroups: groups,
		Buk: snapshotBukJSON{
			Enabled:            rt.settings.BukModeEnabled,
			DestinationSpaceID: optionalSpaceID(game.BukDestinationSpaceID),
		},
		Pause: pauseView,
	}, nil
}

func snapshotNickname(nicknames map[auth.UserID]string, userID auth.UserID) string {
	if nickname := nicknames[userID]; nickname != "" {
		return nickname
	}
	return string(userID)
}

func snapshotParticipantIDsLocked(entry *registeredRoom) []auth.UserID {
	players := entry.lobby.Players()
	ids := make([]auth.UserID, 0, len(players)+len(entry.spectators))
	for id := range players {
		ids = append(ids, auth.UserID(id))
	}
	for id := range entry.spectators {
		ids = append(ids, id)
	}
	return ids
}

func resolveSnapshotNicknames(ctx context.Context, store profile.Store, userIDs []auth.UserID) map[auth.UserID]string {
	nicknames := make(map[auth.UserID]string, len(userIDs))
	if store == nil {
		return nicknames
	}
	for _, userID := range userIDs {
		value, err := store.Lookup(ctx, userID)
		if err != nil {
			if !errors.Is(err, profile.ErrNotFound) {
				slog.Warn("falling back to internal snapshot participant identifier after profile lookup failure", "user_id", userID, "error", err)
			}
			continue
		}
		if err := value.Validate(); err != nil || value.UserID != userID {
			slog.Warn("falling back to internal snapshot participant identifier after invalid profile lookup", "user_id", userID)
			continue
		}
		nicknames[userID] = string(value.Nickname)
	}
	return nicknames
}

func (rt *matchRuntime) snapshotMoveRequest(machine turn.Snapshot) (*protocol.MoveRequiredPayload, error) {
	request := &protocol.MoveRequiredPayload{
		RequiredInput: machine.RequiredInput,
		TokenIDs:      []domain.ResultTokenID{},
		PieceIDs:      []domain.PieceID{},
		Routes:        []domain.Route{},
	}
	switch machine.RequiredInput {
	case domain.InputSelectResult:
		request.TokenIDs = availableTokenIDs(
			availableTokensFor(rt.settings.MovementOrder, machine.ResultQueue),
		)
	case domain.InputSelectPiece:
		request.TokenIDs = []domain.ResultTokenID{machine.SelectedTokenID}
		movable, err := rt.movablePieceIDs(machine)
		if err != nil {
			return nil, fmt.Errorf("assemble move request: %w", err)
		}
		request.PieceIDs = movable
	case domain.InputSelectRoute:
		if machine.SelectedTokenID == "" || rt.pendingMovePiece == "" {
			return nil, fmt.Errorf("%w: route request is missing its selected token or piece", ErrInvalidConfiguration)
		}
		request.TokenIDs = []domain.ResultTokenID{machine.SelectedTokenID}
		request.PieceIDs = []domain.PieceID{rt.pendingMovePiece}
		request.Routes = []domain.Route{domain.RouteNormal, domain.RouteShortcut}
	default:
		return nil, nil
	}
	return request, nil
}

func pauseEndsAtPointer(rt *matchRuntime) *string {
	if !rt.paused {
		return nil
	}
	value := rt.pauseEndsAt.UTC().Format(time.RFC3339)
	return &value
}

func buildTimerView(rt *matchRuntime, now time.Time) snapshotTimerJSON {
	view := snapshotTimerJSON{Phase: snapshotTimerPhaseNone}
	switch rt.timerKind {
	case matchTimerKindThrow:
		view.Phase = snapshotTimerPhaseThrow
	case matchTimerKindMove:
		view.Phase = snapshotTimerPhaseMove
	default:
		return view
	}
	view.RemainingMS = rt.remainingMS(now)
	deadline := rt.timerDeadline.UTC().Format(time.RFC3339)
	view.DeadlineAt = &deadline
	return view
}

type pieceGroupKey struct {
	team  domain.TeamID
	state domain.PieceState
	space domain.SpaceID
}

func buildPieceViews(
	settings room.Settings,
	game match.Snapshot,
) ([]snapshotPieceJSON, []snapshotStackJSON, []snapshotPositionGroup) {
	stackByGroup := make(map[pieceGroupKey]string)
	groupOrderByKey := make([]pieceGroupKey, 0)
	groupPieces := make(map[pieceGroupKey][]domain.PieceID)
	groupPrevious := make(map[pieceGroupKey]*domain.SpaceID)

	pieces := make([]snapshotPieceJSON, 0, len(game.Pieces))
	for _, piece := range game.Pieces {
		view := snapshotPieceJSON{
			PieceID:             piece.ID,
			TeamID:              piece.TeamID,
			State:               piece.State,
			CurrentSpaceID:      nil,
			StackID:             nil,
			PositionGroupID:     nil,
			ActualPreviousSpace: nil,
		}
		switch piece.State {
		case domain.PieceOnBoard, domain.PieceHomeCheckpoint:
			key := pieceGroupKey{team: piece.TeamID, state: piece.State, space: piece.CurrentSpaceID}
			if _, seen := groupPieces[key]; !seen {
				groupOrderByKey = append(groupOrderByKey, key)
			}
			groupPieces[key] = append(groupPieces[key], piece.ID)
			if piece.ActualPreviousSpace != "" && groupPrevious[key] == nil {
				previous := piece.ActualPreviousSpace
				groupPrevious[key] = &previous
			}
			positionGroupID := positionGroupIDFor(piece.TeamID, piece.State, piece.CurrentSpaceID)
			view.CurrentSpaceID = &key.space
			view.PositionGroupID = &positionGroupID
			view.ActualPreviousSpace = optionalSpaceID(piece.ActualPreviousSpace)
		default:
			// waiting and finished pieces keep every location pointer null.
		}
		pieces = append(pieces, view)
	}

	stacks := make([]snapshotStackJSON, 0)
	groups := make([]snapshotPositionGroup, 0, len(groupOrderByKey))
	for _, key := range groupOrderByKey {
		ids := groupPieces[key]
		sort.Slice(ids, func(left, right int) bool { return ids[left] < ids[right] })
		groupID := positionGroupIDFor(key.team, key.state, key.space)
		groups = append(groups, snapshotPositionGroup{
			GroupID:  groupID,
			TeamID:   key.team,
			SpaceID:  key.space,
			PieceIDs: ids,
		})
		if settings.StackingEnabled && len(ids) >= 2 {
			stackID := stackIDFor(key.team, key.space)
			stackByGroup[key] = stackID
			stacks = append(stacks, snapshotStackJSON{
				StackID:             stackID,
				TeamID:              key.team,
				SpaceID:             key.space,
				PieceIDs:            append([]domain.PieceID(nil), ids...),
				ActualPreviousSpace: cloneSpace(groupPrevious[key]),
			})
		}
	}
	for index := range pieces {
		key, ok := pieceKeyOf(pieces[index])
		if !ok {
			continue
		}
		if stackID, stacked := stackByGroup[key]; stacked {
			value := stackID
			pieces[index].StackID = &value
		}
	}
	return pieces, stacks, groups
}

func pieceKeyOf(view snapshotPieceJSON) (pieceGroupKey, bool) {
	if view.CurrentSpaceID == nil || view.State != domain.PieceOnBoard && view.State != domain.PieceHomeCheckpoint {
		return pieceGroupKey{}, false
	}
	return pieceGroupKey{team: view.TeamID, state: view.State, space: *view.CurrentSpaceID}, true
}

func cloneSpace(value *domain.SpaceID) *domain.SpaceID {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

// ReconnectSnapshot builds the authoritative game snapshot for one member's
// RECONNECT command. It never consumes a room sequence (ADR-0009) and fails
// closed with typed errors the executor maps onto protocol rejections.
func (registry *RoomRegistry) ReconnectBundle(user auth.UserID, roomID domain.RoomID, matchID domain.MatchID, lastSequence uint64) (*ReconnectBundle, error) {
	if err := user.Validate(); err != nil {
		return nil, err
	}

	registry.mutex.Lock()

	entry, exists := registry.rooms[roomID]
	if !exists {
		registry.mutex.Unlock()
		return nil, ErrRoomNotFound
	}
	if entry.poisoned {
		registry.mutex.Unlock()
		return nil, ErrEventStoreUnavailable
	}
	if !entry.hasMember(user) {
		registry.mutex.Unlock()
		return nil, ErrNotMember
	}
	if !entry.started || entry.runtime == nil {
		registry.mutex.Unlock()
		return nil, ErrMatchNotActive
	}
	if entry.runtime.matchID != matchID {
		registry.mutex.Unlock()
		return nil, ErrMatchScopeMismatch
	}
	profileStore := registry.profiles
	participantIDs := snapshotParticipantIDsLocked(entry)
	registry.mutex.Unlock()

	ctx, cancel := eventStoreContext()
	nicknames := resolveSnapshotNicknames(ctx, profileStore, participantIDs)
	cancel()

	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	entry, exists = registry.rooms[roomID]
	if !exists {
		return nil, ErrRoomNotFound
	}
	if entry.poisoned {
		return nil, ErrEventStoreUnavailable
	}
	if !entry.hasMember(user) {
		return nil, ErrNotMember
	}
	if !entry.started || entry.runtime == nil {
		return nil, ErrMatchNotActive
	}
	if entry.runtime.matchID != matchID {
		return nil, ErrMatchScopeMismatch
	}
	boundary, err := registry.sequences.Boundary(roomID)
	if err != nil {
		return nil, fmt.Errorf("read room sequence boundary: %w", err)
	}
	if lastSequence > boundary {
		return nil, ErrClientSequenceAhead
	}
	snapshot, err := registry.assembleGameSnapshotWithNicknamesLocked(entry, boundary, nicknames)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("encode game snapshot: %w", err)
	}

	// ADR-0009 bundles may append the contiguous stored events that follow
	// the snapshot boundary. Snapshots are always assembled at the current
	// boundary, so the replay tail is empty today; the read path keeps the
	// bundle contract honest and serves checkpoint-based historical
	// snapshots without protocol change once they exist.
	var events []json.RawMessage
	if registry.store != nil {
		ctx, cancel := eventStoreContext()
		defer cancel()
		rows, err := registry.store.ReadRoomEventsAfter(ctx, roomID, boundary)
		if err != nil {
			return nil, fmt.Errorf("%w: read replay events: %v", ErrEventStoreUnavailable, err)
		}
		events, err = replayPayloads(rows)
		if err != nil {
			return nil, err
		}
	}
	return &ReconnectBundle{Snapshot: encoded, Events: events}, nil
}

// ReconnectBundle carries one atomic game snapshot plus the contiguous
// stored events following its sequence boundary (ADR-0009).
type ReconnectBundle struct {
	Snapshot json.RawMessage
	Events   []json.RawMessage
}

// replayPayloads converts stored rows into verbatim wire events and verifies
// their contiguity so a corrupted store can never produce an invalid bundle.
func replayPayloads(rows []storage.EventRow) ([]json.RawMessage, error) {
	events := make([]json.RawMessage, 0, len(rows))
	expected := uint64(0)
	for index, row := range rows {
		if expected != 0 && row.Sequence != expected {
			return nil, fmt.Errorf("%w: stored event %d is not contiguous", ErrStoredEventsNotContiguous, index)
		}
		expected = row.Sequence + 1
		events = append(events, json.RawMessage(row.PayloadJSON))
	}
	return events, nil
}
