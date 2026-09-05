package protocol

import (
	"fmt"

	"buk-yutnori/internal/domain"
)

const (
	EventGameStarted        = "GAME_STARTED"
	EventTurnStarted        = "TURN_STARTED"
	EventYutResult          = "YUT_RESULT"
	EventResultSelected     = "RESULT_SELECTED"
	EventPieceSelected      = "PIECE_SELECTED"
	EventResultQueueUpdated = "RESULT_QUEUE_UPDATED"
	EventMoveRequired       = "MOVE_REQUIRED"
	EventPieceMoved         = "PIECE_MOVED"
	EventPiecesStacked      = "PIECES_STACKED"
	EventPiecesCaptured     = "PIECES_CAPTURED"
	EventBukResolved        = "BUK_RESOLVED"
	EventCPUControlStarted  = "CPU_CONTROL_STARTED"
	EventPlayerDisconnected = "PLAYER_DISCONNECTED"
	EventPlayerReconnected  = "PLAYER_RECONNECTED"
	EventGamePaused         = "GAME_PAUSED"
	EventGameResumed        = "GAME_RESUMED"
	EventGameEnded          = "GAME_ENDED"

	// Pause reasons follow schemas/ws_server_event.schema.json enums.
	PauseReasonHostRequest       = "host_request"
	PauseReasonStorageFailure    = "storage_failure"
	ResumeReasonHostRequest      = "host_request"
	ResumeReasonPauseExpired     = "pause_expired"
	ResumeReasonHostDisconnected = "host_disconnected"
	// ResumeReasonStorageRecovered closes the operational pause from #87.
	ResumeReasonStorageRecovered = "storage_recovered"

	// MovementBukNoCandidate marks the free-form GAME_ENDED reason recorded
	// when every piece of one team finished.
	GameEndedReasonAllFinished = "all_pieces_finished"
)

// PlayerDisconnectedPayload identifies a human match player whose last
// authenticated WebSocket closed.
type PlayerDisconnectedPayload struct {
	PlayerID domain.PlayerID `json:"player_id"`
}

// PlayerDisconnectedEvent announces a server-authoritative presence loss.
type PlayerDisconnectedEvent struct {
	Version   int                       `json:"version"`
	Direction Direction                 `json:"direction"`
	Type      string                    `json:"type"`
	Sequence  uint64                    `json:"sequence"`
	RoomID    domain.RoomID             `json:"room_id"`
	MatchID   domain.MatchID            `json:"match_id"`
	Payload   PlayerDisconnectedPayload `json:"payload"`
}

func NewPlayerDisconnectedEvent(roomID domain.RoomID, matchID domain.MatchID, sequence uint64, playerID domain.PlayerID) (PlayerDisconnectedEvent, error) {
	if err := validateMatchEventScope(roomID, matchID, sequence); err != nil {
		return PlayerDisconnectedEvent{}, err
	}
	if err := playerID.Validate(); err != nil {
		return PlayerDisconnectedEvent{}, fmt.Errorf("%w: player_id: %v", ErrInvalidServerEvent, err)
	}
	return PlayerDisconnectedEvent{
		Version: Version1, Direction: DirectionServerEvent, Type: EventPlayerDisconnected,
		Sequence: sequence, RoomID: roomID, MatchID: matchID,
		Payload: PlayerDisconnectedPayload{PlayerID: playerID},
	}, nil
}

// PlayerReconnectedPayload identifies a returned human player. ControlRestored
// means future eligible windows are human-controlled; committed CPU work is
// never rolled back.
type PlayerReconnectedPayload struct {
	PlayerID        domain.PlayerID `json:"player_id"`
	ControlRestored bool            `json:"control_restored"`
}

type PlayerReconnectedEvent struct {
	Version   int                      `json:"version"`
	Direction Direction                `json:"direction"`
	Type      string                   `json:"type"`
	Sequence  uint64                   `json:"sequence"`
	RoomID    domain.RoomID            `json:"room_id"`
	MatchID   domain.MatchID           `json:"match_id"`
	Payload   PlayerReconnectedPayload `json:"payload"`
}

func NewPlayerReconnectedEvent(roomID domain.RoomID, matchID domain.MatchID, sequence uint64, playerID domain.PlayerID, controlRestored bool) (PlayerReconnectedEvent, error) {
	if err := validateMatchEventScope(roomID, matchID, sequence); err != nil {
		return PlayerReconnectedEvent{}, err
	}
	if err := playerID.Validate(); err != nil {
		return PlayerReconnectedEvent{}, fmt.Errorf("%w: player_id: %v", ErrInvalidServerEvent, err)
	}
	return PlayerReconnectedEvent{
		Version: Version1, Direction: DirectionServerEvent, Type: EventPlayerReconnected,
		Sequence: sequence, RoomID: roomID, MatchID: matchID,
		Payload: PlayerReconnectedPayload{PlayerID: playerID, ControlRestored: controlRestored},
	}, nil
}

// ResultTokenView is the public v1 result token shape. It intentionally omits
// the generating player because YUT_RESULT already carries player_id.
type ResultTokenView struct {
	TokenID domain.ResultTokenID `json:"token_id"`
	Result  domain.YutResult     `json:"result"`
	Origin  domain.ResultOrigin  `json:"origin"`
}

// GameStartedPayload announces the canonical first player and Buk target.
type GameStartedPayload struct {
	FirstPlayerID       domain.PlayerID `json:"first_player_id"`
	BukDestinationSpace *domain.SpaceID `json:"buk_destination_space_id"`
}

// GameStartedEvent is the typed v1 match-scoped GAME_STARTED server event.
type GameStartedEvent struct {
	Version   int                `json:"version"`
	Direction Direction          `json:"direction"`
	Type      string             `json:"type"`
	Sequence  uint64             `json:"sequence"`
	RoomID    domain.RoomID      `json:"room_id"`
	MatchID   domain.MatchID     `json:"match_id"`
	Payload   GameStartedPayload `json:"payload"`
}

// NewGameStartedEvent constructs a validated immutable GAME_STARTED event.
func NewGameStartedEvent(roomID domain.RoomID, matchID domain.MatchID, sequence uint64, payload GameStartedPayload) (GameStartedEvent, error) {
	if err := validateMatchEventScope(roomID, matchID, sequence); err != nil {
		return GameStartedEvent{}, err
	}
	if err := payload.FirstPlayerID.Validate(); err != nil {
		return GameStartedEvent{}, fmt.Errorf("%w: first_player_id: %v", ErrInvalidServerEvent, err)
	}
	if payload.BukDestinationSpace != nil {
		if err := payload.BukDestinationSpace.Validate(); err != nil {
			return GameStartedEvent{}, fmt.Errorf("%w: buk_destination_space_id: %v", ErrInvalidServerEvent, err)
		}
	}
	return GameStartedEvent{
		Version: Version1, Direction: DirectionServerEvent, Type: EventGameStarted,
		Sequence: sequence, RoomID: roomID, MatchID: matchID, Payload: payload,
	}, nil
}

// TurnStartedPayload reports whose decision window opened and its deadline.
type TurnStartedPayload struct {
	PlayerID      domain.PlayerID      `json:"player_id"`
	Phase         domain.TurnPhase     `json:"phase"`
	RequiredInput domain.RequiredInput `json:"required_input"`
	RemainingMS   uint64               `json:"remaining_ms"`
}

// TurnStartedEvent is the typed v1 match-scoped TURN_STARTED server event.
type TurnStartedEvent struct {
	Version   int                `json:"version"`
	Direction Direction          `json:"direction"`
	Type      string             `json:"type"`
	Sequence  uint64             `json:"sequence"`
	RoomID    domain.RoomID      `json:"room_id"`
	MatchID   domain.MatchID     `json:"match_id"`
	Payload   TurnStartedPayload `json:"payload"`
}

// NewTurnStartedEvent constructs a validated immutable TURN_STARTED event.
func NewTurnStartedEvent(roomID domain.RoomID, matchID domain.MatchID, sequence uint64, payload TurnStartedPayload) (TurnStartedEvent, error) {
	if err := validateMatchEventScope(roomID, matchID, sequence); err != nil {
		return TurnStartedEvent{}, err
	}
	if err := payload.PlayerID.Validate(); err != nil {
		return TurnStartedEvent{}, fmt.Errorf("%w: player_id: %v", ErrInvalidServerEvent, err)
	}
	if err := payload.Phase.Validate(); err != nil {
		return TurnStartedEvent{}, fmt.Errorf("%w: phase: %v", ErrInvalidServerEvent, err)
	}
	if err := payload.RequiredInput.Validate(); err != nil {
		return TurnStartedEvent{}, fmt.Errorf("%w: required_input: %v", ErrInvalidServerEvent, err)
	}
	return TurnStartedEvent{
		Version: Version1, Direction: DirectionServerEvent, Type: EventTurnStarted,
		Sequence: sequence, RoomID: roomID, MatchID: matchID, Payload: payload,
	}, nil
}

// YutResultPayload announces one server-generated throw result.
type YutResultPayload struct {
	PlayerID domain.PlayerID `json:"player_id"`
	Token    ResultTokenView `json:"token"`
}

// YutResultEvent is the typed v1 match-scoped YUT_RESULT server event.
type YutResultEvent struct {
	Version   int              `json:"version"`
	Direction Direction        `json:"direction"`
	Type      string           `json:"type"`
	Sequence  uint64           `json:"sequence"`
	RoomID    domain.RoomID    `json:"room_id"`
	MatchID   domain.MatchID   `json:"match_id"`
	Payload   YutResultPayload `json:"payload"`
}

// NewYutResultEvent constructs a validated immutable YUT_RESULT event.
func NewYutResultEvent(roomID domain.RoomID, matchID domain.MatchID, sequence uint64, payload YutResultPayload) (YutResultEvent, error) {
	if err := validateMatchEventScope(roomID, matchID, sequence); err != nil {
		return YutResultEvent{}, err
	}
	if err := payload.PlayerID.Validate(); err != nil {
		return YutResultEvent{}, fmt.Errorf("%w: player_id: %v", ErrInvalidServerEvent, err)
	}
	if err := validateResultTokenView(payload.Token); err != nil {
		return YutResultEvent{}, err
	}
	return YutResultEvent{
		Version: Version1, Direction: DirectionServerEvent, Type: EventYutResult,
		Sequence: sequence, RoomID: roomID, MatchID: matchID, Payload: payload,
	}, nil
}

// ResultQueueUpdatedPayload carries the complete unresolved queue snapshot.
type ResultQueueUpdatedPayload struct {
	ResultQueue []ResultTokenView `json:"result_queue"`
}

// ResultQueueUpdatedEvent is the typed v1 RESULT_QUEUE_UPDATED server event.
type ResultQueueUpdatedEvent struct {
	Version   int                       `json:"version"`
	Direction Direction                 `json:"direction"`
	Type      string                    `json:"type"`
	Sequence  uint64                    `json:"sequence"`
	RoomID    domain.RoomID             `json:"room_id"`
	MatchID   domain.MatchID            `json:"match_id"`
	Payload   ResultQueueUpdatedPayload `json:"payload"`
}

// NewResultQueueUpdatedEvent constructs a validated immutable event.
func NewResultQueueUpdatedEvent(roomID domain.RoomID, matchID domain.MatchID, sequence uint64, tokens []ResultTokenView) (ResultQueueUpdatedEvent, error) {
	if err := validateMatchEventScope(roomID, matchID, sequence); err != nil {
		return ResultQueueUpdatedEvent{}, err
	}
	queue := make([]ResultTokenView, 0, len(tokens))
	for index, token := range tokens {
		if err := validateResultTokenView(token); err != nil {
			return ResultQueueUpdatedEvent{}, fmt.Errorf("result_queue[%d]: %w", index, err)
		}
		queue = append(queue, token)
	}
	return ResultQueueUpdatedEvent{
		Version: Version1, Direction: DirectionServerEvent, Type: EventResultQueueUpdated,
		Sequence: sequence, RoomID: roomID, MatchID: matchID,
		Payload: ResultQueueUpdatedPayload{ResultQueue: queue},
	}, nil
}

// ResultSelectedPayload records the result component of an accepted atomic
// SELECT_MOVE. It is always immediately followed by PIECE_SELECTED.
type ResultSelectedPayload struct {
	TokenID domain.ResultTokenID `json:"token_id"`
}

type ResultSelectedEvent struct {
	Version   int                   `json:"version"`
	Direction Direction             `json:"direction"`
	Type      string                `json:"type"`
	Sequence  uint64                `json:"sequence"`
	RoomID    domain.RoomID         `json:"room_id"`
	MatchID   domain.MatchID        `json:"match_id"`
	Payload   ResultSelectedPayload `json:"payload"`
}

func NewResultSelectedEvent(roomID domain.RoomID, matchID domain.MatchID, sequence uint64, payload ResultSelectedPayload) (ResultSelectedEvent, error) {
	if err := validateMatchEventScope(roomID, matchID, sequence); err != nil {
		return ResultSelectedEvent{}, err
	}
	if err := payload.TokenID.Validate(); err != nil {
		return ResultSelectedEvent{}, fmt.Errorf("%w: token_id: %v", ErrInvalidServerEvent, err)
	}
	return ResultSelectedEvent{Version: Version1, Direction: DirectionServerEvent, Type: EventResultSelected, Sequence: sequence, RoomID: roomID, MatchID: matchID, Payload: payload}, nil
}

// PieceSelectedPayload records the piece component of an accepted atomic SELECT_MOVE.
type PieceSelectedPayload struct {
	TokenID domain.ResultTokenID `json:"token_id"`
	PieceID domain.PieceID       `json:"piece_id"`
}

type PieceSelectedEvent struct {
	Version   int                  `json:"version"`
	Direction Direction            `json:"direction"`
	Type      string               `json:"type"`
	Sequence  uint64               `json:"sequence"`
	RoomID    domain.RoomID        `json:"room_id"`
	MatchID   domain.MatchID       `json:"match_id"`
	Payload   PieceSelectedPayload `json:"payload"`
}

func NewPieceSelectedEvent(roomID domain.RoomID, matchID domain.MatchID, sequence uint64, payload PieceSelectedPayload) (PieceSelectedEvent, error) {
	if err := validateMatchEventScope(roomID, matchID, sequence); err != nil {
		return PieceSelectedEvent{}, err
	}
	if err := payload.TokenID.Validate(); err != nil {
		return PieceSelectedEvent{}, fmt.Errorf("%w: token_id: %v", ErrInvalidServerEvent, err)
	}
	if err := payload.PieceID.Validate(); err != nil {
		return PieceSelectedEvent{}, fmt.Errorf("%w: piece_id: %v", ErrInvalidServerEvent, err)
	}
	return PieceSelectedEvent{Version: Version1, Direction: DirectionServerEvent, Type: EventPieceSelected, Sequence: sequence, RoomID: roomID, MatchID: matchID, Payload: payload}, nil
}

// MoveCandidate is one server-calculated legal result/piece pair and its
// possible routes. An empty route list is valid for a Backdo move.
type MoveCandidate struct {
	TokenID domain.ResultTokenID `json:"token_id"`
	PieceID domain.PieceID       `json:"piece_id"`
	Routes  []domain.Route       `json:"routes"`
}

// MoveRequiredPayload asks the acting player for the next atomic move or
// the sole pending shortcut choice.
type MoveRequiredPayload struct {
	RequiredInput domain.RequiredInput `json:"required_input"`
	Candidates    []MoveCandidate      `json:"candidates"`
}

// MoveRequiredEvent is the typed v1 MOVE_REQUIRED server event.
type MoveRequiredEvent struct {
	Version   int                 `json:"version"`
	Direction Direction           `json:"direction"`
	Type      string              `json:"type"`
	Sequence  uint64              `json:"sequence"`
	RoomID    domain.RoomID       `json:"room_id"`
	MatchID   domain.MatchID      `json:"match_id"`
	Payload   MoveRequiredPayload `json:"payload"`
}

// NewMoveRequiredEvent constructs a validated immutable MOVE_REQUIRED event.
func NewMoveRequiredEvent(roomID domain.RoomID, matchID domain.MatchID, sequence uint64, payload MoveRequiredPayload) (MoveRequiredEvent, error) {
	if err := validateMatchEventScope(roomID, matchID, sequence); err != nil {
		return MoveRequiredEvent{}, err
	}
	switch payload.RequiredInput {
	case domain.InputSelectMove, domain.InputSelectRoute:
	default:
		return MoveRequiredEvent{}, fmt.Errorf("%w: required_input %q", ErrInvalidServerEvent, payload.RequiredInput)
	}
	if len(payload.Candidates) == 0 {
		return MoveRequiredEvent{}, fmt.Errorf("%w: candidates is empty", ErrInvalidServerEvent)
	}
	candidates := make([]MoveCandidate, 0, len(payload.Candidates))
	seen := make(map[string]struct{}, len(payload.Candidates))
	for index, candidate := range payload.Candidates {
		if err := candidate.TokenID.Validate(); err != nil {
			return MoveRequiredEvent{}, fmt.Errorf("%w: candidates[%d].token_id: %v", ErrInvalidServerEvent, index, err)
		}
		if err := candidate.PieceID.Validate(); err != nil {
			return MoveRequiredEvent{}, fmt.Errorf("%w: candidates[%d].piece_id: %v", ErrInvalidServerEvent, index, err)
		}
		key := string(candidate.TokenID) + "\x00" + string(candidate.PieceID)
		if _, duplicate := seen[key]; duplicate {
			return MoveRequiredEvent{}, fmt.Errorf("%w: duplicate candidates[%d]", ErrInvalidServerEvent, index)
		}
		seen[key] = struct{}{}
		routes := append([]domain.Route(nil), candidate.Routes...)
		seenRoutes := make(map[domain.Route]struct{}, len(routes))
		for routeIndex, route := range routes {
			if err := route.Validate(); err != nil {
				return MoveRequiredEvent{}, fmt.Errorf("%w: candidates[%d].routes[%d]: %v", ErrInvalidServerEvent, index, routeIndex, err)
			}
			if _, duplicate := seenRoutes[route]; duplicate {
				return MoveRequiredEvent{}, fmt.Errorf("%w: duplicate candidates[%d].routes[%d]", ErrInvalidServerEvent, index, routeIndex)
			}
			seenRoutes[route] = struct{}{}
		}
		candidates = append(candidates, MoveCandidate{TokenID: candidate.TokenID, PieceID: candidate.PieceID, Routes: routes})
	}
	if payload.RequiredInput == domain.InputSelectRoute {
		if len(candidates) != 1 || len(candidates[0].Routes) != 2 || candidates[0].Routes[0] != domain.RouteNormal || candidates[0].Routes[1] != domain.RouteShortcut {
			return MoveRequiredEvent{}, fmt.Errorf("%w: select_route requires one candidate with normal and shortcut routes", ErrInvalidServerEvent)
		}
	}
	return MoveRequiredEvent{
		Version: Version1, Direction: DirectionServerEvent, Type: EventMoveRequired,
		Sequence: sequence, RoomID: roomID, MatchID: matchID,
		Payload: MoveRequiredPayload{RequiredInput: payload.RequiredInput, Candidates: candidates},
	}, nil
}

// PieceMovedPayload reports one committed movement of one moving group.
type PieceMovedPayload struct {
	PieceIDs     []domain.PieceID    `json:"piece_ids"`
	FromSpaceID  *domain.SpaceID     `json:"from_space_id"`
	ToSpaceID    *domain.SpaceID     `json:"to_space_id"`
	MovementKind domain.MovementKind `json:"movement_kind"`
}

// PieceMovedEvent is the typed v1 PIECE_MOVED server event.
type PieceMovedEvent struct {
	Version   int               `json:"version"`
	Direction Direction         `json:"direction"`
	Type      string            `json:"type"`
	Sequence  uint64            `json:"sequence"`
	RoomID    domain.RoomID     `json:"room_id"`
	MatchID   domain.MatchID    `json:"match_id"`
	Payload   PieceMovedPayload `json:"payload"`
}

// NewPieceMovedEvent constructs a validated immutable PIECE_MOVED event.
func NewPieceMovedEvent(roomID domain.RoomID, matchID domain.MatchID, sequence uint64, payload PieceMovedPayload) (PieceMovedEvent, error) {
	if err := validateMatchEventScope(roomID, matchID, sequence); err != nil {
		return PieceMovedEvent{}, err
	}
	if len(payload.PieceIDs) == 0 {
		return PieceMovedEvent{}, fmt.Errorf("%w: piece_ids is empty", ErrInvalidServerEvent)
	}
	seen := make(map[domain.PieceID]struct{}, len(payload.PieceIDs))
	for index, pieceID := range payload.PieceIDs {
		if err := pieceID.Validate(); err != nil {
			return PieceMovedEvent{}, fmt.Errorf("%w: piece_ids[%d]: %v", ErrInvalidServerEvent, index, err)
		}
		if _, duplicate := seen[pieceID]; duplicate {
			return PieceMovedEvent{}, fmt.Errorf("%w: duplicate piece_ids[%d]", ErrInvalidServerEvent, index)
		}
		seen[pieceID] = struct{}{}
	}
	switch payload.MovementKind {
	case domain.MovementForward, domain.MovementBackdo, domain.MovementBuk, domain.MovementFinish:
	default:
		return PieceMovedEvent{}, fmt.Errorf("%w: movement_kind %q", ErrInvalidServerEvent, payload.MovementKind)
	}
	if err := validateOptionalSpace(payload.FromSpaceID, "from_space_id"); err != nil {
		return PieceMovedEvent{}, err
	}
	if err := validateOptionalSpace(payload.ToSpaceID, "to_space_id"); err != nil {
		return PieceMovedEvent{}, err
	}
	return PieceMovedEvent{
		Version: Version1, Direction: DirectionServerEvent, Type: EventPieceMoved,
		Sequence: sequence, RoomID: roomID, MatchID: matchID,
		Payload: clonePieceMovedPayload(payload),
	}, nil
}

// PiecesStackedPayload reports one merged friendly group after a move.
type PiecesStackedPayload struct {
	StackID             string           `json:"stack_id"`
	PieceIDs            []domain.PieceID `json:"piece_ids"`
	SpaceID             domain.SpaceID   `json:"space_id"`
	ActualPreviousSpace *domain.SpaceID  `json:"actual_previous_space"`
}

// PiecesStackedEvent is the typed v1 PIECES_STACKED server event.
type PiecesStackedEvent struct {
	Version   int                  `json:"version"`
	Direction Direction            `json:"direction"`
	Type      string               `json:"type"`
	Sequence  uint64               `json:"sequence"`
	RoomID    domain.RoomID        `json:"room_id"`
	MatchID   domain.MatchID       `json:"match_id"`
	Payload   PiecesStackedPayload `json:"payload"`
}

// NewPiecesStackedEvent constructs a validated immutable PIECES_STACKED event.
func NewPiecesStackedEvent(roomID domain.RoomID, matchID domain.MatchID, sequence uint64, payload PiecesStackedPayload) (PiecesStackedEvent, error) {
	if err := validateMatchEventScope(roomID, matchID, sequence); err != nil {
		return PiecesStackedEvent{}, err
	}
	if payload.StackID == "" {
		return PiecesStackedEvent{}, fmt.Errorf("%w: stack_id must not be empty", ErrInvalidServerEvent)
	}
	if len(payload.PieceIDs) < 2 {
		return PiecesStackedEvent{}, fmt.Errorf("%w: stacked group needs at least two pieces", ErrInvalidServerEvent)
	}
	seen := make(map[domain.PieceID]struct{}, len(payload.PieceIDs))
	for index, pieceID := range payload.PieceIDs {
		if err := pieceID.Validate(); err != nil {
			return PiecesStackedEvent{}, fmt.Errorf("%w: piece_ids[%d]: %v", ErrInvalidServerEvent, index, err)
		}
		if _, duplicate := seen[pieceID]; duplicate {
			return PiecesStackedEvent{}, fmt.Errorf("%w: duplicate piece_ids[%d]", ErrInvalidServerEvent, index)
		}
		seen[pieceID] = struct{}{}
	}
	if err := payload.SpaceID.Validate(); err != nil {
		return PiecesStackedEvent{}, fmt.Errorf("%w: space_id: %v", ErrInvalidServerEvent, err)
	}
	if err := validateOptionalSpace(payload.ActualPreviousSpace, "actual_previous_space"); err != nil {
		return PiecesStackedEvent{}, err
	}
	return PiecesStackedEvent{
		Version: Version1, Direction: DirectionServerEvent, Type: EventPiecesStacked,
		Sequence: sequence, RoomID: roomID, MatchID: matchID,
		Payload: clonePiecesStackedPayload(payload),
	}, nil
}

// PiecesCapturedPayload reports opposing pieces returned to waiting state.
type PiecesCapturedPayload struct {
	CapturedPieceIDs []domain.PieceID `json:"captured_piece_ids"`
	SpaceID          domain.SpaceID   `json:"space_id"`
}

// PiecesCapturedEvent is the typed v1 PIECES_CAPTURED server event.
type PiecesCapturedEvent struct {
	Version   int                   `json:"version"`
	Direction Direction             `json:"direction"`
	Type      string                `json:"type"`
	Sequence  uint64                `json:"sequence"`
	RoomID    domain.RoomID         `json:"room_id"`
	MatchID   domain.MatchID        `json:"match_id"`
	Payload   PiecesCapturedPayload `json:"payload"`
}

// NewPiecesCapturedEvent constructs a validated immutable PIECES_CAPTURED event.
func NewPiecesCapturedEvent(roomID domain.RoomID, matchID domain.MatchID, sequence uint64, payload PiecesCapturedPayload) (PiecesCapturedEvent, error) {
	if err := validateMatchEventScope(roomID, matchID, sequence); err != nil {
		return PiecesCapturedEvent{}, err
	}
	if len(payload.CapturedPieceIDs) == 0 {
		return PiecesCapturedEvent{}, fmt.Errorf("%w: captured_piece_ids is empty", ErrInvalidServerEvent)
	}
	seen := make(map[domain.PieceID]struct{}, len(payload.CapturedPieceIDs))
	for index, pieceID := range payload.CapturedPieceIDs {
		if err := pieceID.Validate(); err != nil {
			return PiecesCapturedEvent{}, fmt.Errorf("%w: captured_piece_ids[%d]: %v", ErrInvalidServerEvent, index, err)
		}
		if _, duplicate := seen[pieceID]; duplicate {
			return PiecesCapturedEvent{}, fmt.Errorf("%w: duplicate captured_piece_ids[%d]", ErrInvalidServerEvent, index)
		}
		seen[pieceID] = struct{}{}
	}
	if err := payload.SpaceID.Validate(); err != nil {
		return PiecesCapturedEvent{}, fmt.Errorf("%w: space_id: %v", ErrInvalidServerEvent, err)
	}
	return PiecesCapturedEvent{
		Version: Version1, Direction: DirectionServerEvent, Type: EventPiecesCaptured,
		Sequence: sequence, RoomID: roomID, MatchID: matchID,
		Payload: clonePiecesCapturedPayload(payload),
	}, nil
}

// BukResolvedPayload reports one automatic Buk resolution.
type BukResolvedPayload struct {
	TokenID            domain.ResultTokenID `json:"token_id"`
	DestinationSpaceID domain.SpaceID       `json:"destination_space_id"`
	MovedPieceIDs      []domain.PieceID     `json:"moved_piece_ids"`
	SourceSpaceID      *domain.SpaceID      `json:"source_space_id"`
	NoCandidate        bool                 `json:"no_candidate"`
}

// BukResolvedEvent is the typed v1 BUK_RESOLVED server event.
type BukResolvedEvent struct {
	Version   int                `json:"version"`
	Direction Direction          `json:"direction"`
	Type      string             `json:"type"`
	Sequence  uint64             `json:"sequence"`
	RoomID    domain.RoomID      `json:"room_id"`
	MatchID   domain.MatchID     `json:"match_id"`
	Payload   BukResolvedPayload `json:"payload"`
}

// NewBukResolvedEvent constructs a validated immutable BUK_RESOLVED event.
func NewBukResolvedEvent(roomID domain.RoomID, matchID domain.MatchID, sequence uint64, payload BukResolvedPayload) (BukResolvedEvent, error) {
	if err := validateMatchEventScope(roomID, matchID, sequence); err != nil {
		return BukResolvedEvent{}, err
	}
	if err := payload.TokenID.Validate(); err != nil {
		return BukResolvedEvent{}, fmt.Errorf("%w: token_id: %v", ErrInvalidServerEvent, err)
	}
	if err := payload.DestinationSpaceID.Validate(); err != nil {
		return BukResolvedEvent{}, fmt.Errorf("%w: destination_space_id: %v", ErrInvalidServerEvent, err)
	}
	seen := make(map[domain.PieceID]struct{}, len(payload.MovedPieceIDs))
	for index, pieceID := range payload.MovedPieceIDs {
		if err := pieceID.Validate(); err != nil {
			return BukResolvedEvent{}, fmt.Errorf("%w: moved_piece_ids[%d]: %v", ErrInvalidServerEvent, index, err)
		}
		if _, duplicate := seen[pieceID]; duplicate {
			return BukResolvedEvent{}, fmt.Errorf("%w: duplicate moved_piece_ids[%d]", ErrInvalidServerEvent, index)
		}
		seen[pieceID] = struct{}{}
	}
	if err := validateOptionalSpace(payload.SourceSpaceID, "source_space_id"); err != nil {
		return BukResolvedEvent{}, err
	}
	moved := make([]domain.PieceID, 0, len(payload.MovedPieceIDs))
	moved = append(moved, payload.MovedPieceIDs...)
	return BukResolvedEvent{
		Version: Version1, Direction: DirectionServerEvent, Type: EventBukResolved,
		Sequence: sequence, RoomID: roomID, MatchID: matchID,
		Payload: BukResolvedPayload{
			TokenID:            payload.TokenID,
			DestinationSpaceID: payload.DestinationSpaceID,
			MovedPieceIDs:      moved,
			SourceSpaceID:      cloneOptionalSpace(payload.SourceSpaceID),
			NoCandidate:        payload.NoCandidate,
		},
	}, nil
}

// CPUControlStartedPayload announces server-side play substitution.
type CPUControlStartedPayload struct {
	PlayerID domain.PlayerID `json:"player_id"`
	Reason   string          `json:"reason"`
}

// CPUControlStartedEvent is the typed v1 CPU_CONTROL_STARTED server event.
type CPUControlStartedEvent struct {
	Version   int                      `json:"version"`
	Direction Direction                `json:"direction"`
	Type      string                   `json:"type"`
	Sequence  uint64                   `json:"sequence"`
	RoomID    domain.RoomID            `json:"room_id"`
	MatchID   domain.MatchID           `json:"match_id"`
	Payload   CPUControlStartedPayload `json:"payload"`
}

// NewCPUControlStartedEvent constructs a validated immutable event. Reason is
// restricted to the schema enum disconnected|timeout|lobby_player.
func NewCPUControlStartedEvent(roomID domain.RoomID, matchID domain.MatchID, sequence uint64, payload CPUControlStartedPayload) (CPUControlStartedEvent, error) {
	if err := validateMatchEventScope(roomID, matchID, sequence); err != nil {
		return CPUControlStartedEvent{}, err
	}
	if err := payload.PlayerID.Validate(); err != nil {
		return CPUControlStartedEvent{}, fmt.Errorf("%w: player_id: %v", ErrInvalidServerEvent, err)
	}
	switch payload.Reason {
	case "disconnected", "timeout", "lobby_player":
	default:
		return CPUControlStartedEvent{}, fmt.Errorf("%w: reason %q", ErrInvalidServerEvent, payload.Reason)
	}
	return CPUControlStartedEvent{
		Version: Version1, Direction: DirectionServerEvent, Type: EventCPUControlStarted,
		Sequence: sequence, RoomID: roomID, MatchID: matchID, Payload: payload,
	}, nil
}

// GamePausedPayload announces one match pause. PreservedTimerMS carries the
// remaining milliseconds of the active turn window at pause time; EndsAt is
// the wall-clock auto-resume instant when one is scheduled.
type GamePausedPayload struct {
	Reason           string           `json:"reason"`
	PausedByPlayerID *domain.PlayerID `json:"paused_by_player_id"`
	EndsAt           *string          `json:"ends_at"`
	PreservedTimerMS uint64           `json:"preserved_timer_remaining_ms"`
}

// GamePausedEvent is the typed v1 GAME_PAUSED server event.
type GamePausedEvent struct {
	Version   int               `json:"version"`
	Direction Direction         `json:"direction"`
	Type      string            `json:"type"`
	Sequence  uint64            `json:"sequence"`
	RoomID    domain.RoomID     `json:"room_id"`
	MatchID   domain.MatchID    `json:"match_id"`
	Payload   GamePausedPayload `json:"payload"`
}

// NewGamePausedEvent constructs a validated immutable GAME_PAUSED event.
func NewGamePausedEvent(roomID domain.RoomID, matchID domain.MatchID, sequence uint64, payload GamePausedPayload) (GamePausedEvent, error) {
	if err := validateMatchEventScope(roomID, matchID, sequence); err != nil {
		return GamePausedEvent{}, err
	}
	switch payload.Reason {
	case PauseReasonHostRequest, PauseReasonStorageFailure:
	default:
		return GamePausedEvent{}, fmt.Errorf("%w: reason %q", ErrInvalidServerEvent, payload.Reason)
	}
	if payload.PausedByPlayerID != nil {
		if err := payload.PausedByPlayerID.Validate(); err != nil {
			return GamePausedEvent{}, fmt.Errorf("%w: paused_by_player_id: %v", ErrInvalidServerEvent, err)
		}
	}
	return GamePausedEvent{
		Version: Version1, Direction: DirectionServerEvent, Type: EventGamePaused,
		Sequence: sequence, RoomID: roomID, MatchID: matchID,
		Payload: GamePausedPayload{
			Reason:           payload.Reason,
			PausedByPlayerID: cloneOptionalPlayer(payload.PausedByPlayerID),
			EndsAt:           cloneOptionalString(payload.EndsAt),
			PreservedTimerMS: payload.PreservedTimerMS,
		},
	}, nil
}

// GameResumedPayload announces the end of one match pause.
type GameResumedPayload struct {
	Reason string `json:"reason"`
}

// GameResumedEvent is the typed v1 GAME_RESUMED server event.
type GameResumedEvent struct {
	Version   int                `json:"version"`
	Direction Direction          `json:"direction"`
	Type      string             `json:"type"`
	Sequence  uint64             `json:"sequence"`
	RoomID    domain.RoomID      `json:"room_id"`
	MatchID   domain.MatchID     `json:"match_id"`
	Payload   GameResumedPayload `json:"payload"`
}

// NewGameResumedEvent constructs a validated immutable GAME_RESUMED event.
func NewGameResumedEvent(roomID domain.RoomID, matchID domain.MatchID, sequence uint64, payload GameResumedPayload) (GameResumedEvent, error) {
	if err := validateMatchEventScope(roomID, matchID, sequence); err != nil {
		return GameResumedEvent{}, err
	}
	switch payload.Reason {
	case ResumeReasonHostRequest, ResumeReasonPauseExpired, ResumeReasonHostDisconnected, ResumeReasonStorageRecovered:
	default:
		return GameResumedEvent{}, fmt.Errorf("%w: reason %q", ErrInvalidServerEvent, payload.Reason)
	}
	return GameResumedEvent{
		Version: Version1, Direction: DirectionServerEvent, Type: EventGameResumed,
		Sequence: sequence, RoomID: roomID, MatchID: matchID, Payload: payload,
	}, nil
}

// NewInvalidGameEndedEvent constructs the terminal GAME_ENDED event for an
// invalidated match: status=invalid, no winning team, free-form reason.
func NewInvalidGameEndedEvent(roomID domain.RoomID, matchID domain.MatchID, sequence uint64, reason string) (GameEndedEvent, error) {
	if err := validateMatchEventScope(roomID, matchID, sequence); err != nil {
		return GameEndedEvent{}, err
	}
	if reason == "" {
		return GameEndedEvent{}, fmt.Errorf("%w: reason is required", ErrInvalidServerEvent)
	}
	return GameEndedEvent{
		Version: Version1, Direction: DirectionServerEvent, Type: EventGameEnded,
		Sequence: sequence, RoomID: roomID, MatchID: matchID,
		Payload: GameEndedPayload{Status: "invalid", WinnerTeamID: nil, Reason: reason},
	}, nil
}

func cloneOptionalPlayer(value *domain.PlayerID) *domain.PlayerID {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

// GameEndedPayload terminates one match scope.
type GameEndedPayload struct {
	Status       string         `json:"status"`
	WinnerTeamID *domain.TeamID `json:"winner_team_id"`
	Reason       string         `json:"reason"`
}

// GameEndedEvent is the typed v1 GAME_ENDED server event.
type GameEndedEvent struct {
	Version   int              `json:"version"`
	Direction Direction        `json:"direction"`
	Type      string           `json:"type"`
	Sequence  uint64           `json:"sequence"`
	RoomID    domain.RoomID    `json:"room_id"`
	MatchID   domain.MatchID   `json:"match_id"`
	Payload   GameEndedPayload `json:"payload"`
}

// NewFinishedGameEndedEvent constructs the normal-end GAME_ENDED event.
func NewFinishedGameEndedEvent(roomID domain.RoomID, matchID domain.MatchID, sequence uint64, winner domain.TeamID, reason string) (GameEndedEvent, error) {
	if err := validateMatchEventScope(roomID, matchID, sequence); err != nil {
		return GameEndedEvent{}, err
	}
	if err := winner.Validate(); err != nil {
		return GameEndedEvent{}, fmt.Errorf("%w: winner_team_id: %v", ErrInvalidServerEvent, err)
	}
	if reason == "" {
		return GameEndedEvent{}, fmt.Errorf("%w: reason is required", ErrInvalidServerEvent)
	}
	winnerTeamID := winner
	return GameEndedEvent{
		Version: Version1, Direction: DirectionServerEvent, Type: EventGameEnded,
		Sequence: sequence, RoomID: roomID, MatchID: matchID,
		Payload: GameEndedPayload{Status: "finished", WinnerTeamID: &winnerTeamID, Reason: reason},
	}, nil
}

func validateMatchEventScope(roomID domain.RoomID, matchID domain.MatchID, sequence uint64) error {
	if err := roomID.Validate(); err != nil {
		return fmt.Errorf("%w: room_id: %v", ErrInvalidServerEvent, err)
	}
	if err := matchID.Validate(); err != nil {
		return fmt.Errorf("%w: match_id: %v", ErrInvalidServerEvent, err)
	}
	if sequence == 0 {
		return fmt.Errorf("%w: sequence must start at one", ErrInvalidServerEvent)
	}
	return nil
}

func validateResultTokenView(token ResultTokenView) error {
	if err := token.TokenID.Validate(); err != nil {
		return fmt.Errorf("%w: token.token_id: %v", ErrInvalidServerEvent, err)
	}
	if err := token.Result.Validate(); err != nil {
		return fmt.Errorf("%w: token.result: %v", ErrInvalidServerEvent, err)
	}
	if err := token.Origin.Validate(); err != nil {
		return fmt.Errorf("%w: token.origin: %v", ErrInvalidServerEvent, err)
	}
	return nil
}

func validateOptionalSpace(space *domain.SpaceID, field string) error {
	if space == nil {
		return nil
	}
	if err := space.Validate(); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrInvalidServerEvent, field, err)
	}
	return nil
}

func cloneOptionalSpace(space *domain.SpaceID) *domain.SpaceID {
	if space == nil {
		return nil
	}
	value := *space
	return &value
}

func clonePieceMovedPayload(payload PieceMovedPayload) PieceMovedPayload {
	payload.PieceIDs = append([]domain.PieceID(nil), payload.PieceIDs...)
	payload.FromSpaceID = cloneOptionalSpace(payload.FromSpaceID)
	payload.ToSpaceID = cloneOptionalSpace(payload.ToSpaceID)
	return payload
}

func clonePiecesStackedPayload(payload PiecesStackedPayload) PiecesStackedPayload {
	payload.PieceIDs = append([]domain.PieceID(nil), payload.PieceIDs...)
	payload.ActualPreviousSpace = cloneOptionalSpace(payload.ActualPreviousSpace)
	return payload
}

func clonePiecesCapturedPayload(payload PiecesCapturedPayload) PiecesCapturedPayload {
	payload.CapturedPieceIDs = append([]domain.PieceID(nil), payload.CapturedPieceIDs...)
	return payload
}
