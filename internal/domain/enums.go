package domain

import "fmt"

// TeamID distinguishes the two canonical teams.
type TeamID string

const (
	TeamA TeamID = "A"
	TeamB TeamID = "B"
)

// PieceState describes a piece's progress through the board.
type PieceState string

const (
	PieceWaiting        PieceState = "waiting"
	PieceOnBoard        PieceState = "on_board"
	PieceHomeCheckpoint PieceState = "home_checkpoint"
	PieceFinished       PieceState = "finished"
)

// YutResult is one canonical throw result.
type YutResult string

const (
	YutDo     YutResult = "do"
	YutGae    YutResult = "gae"
	YutGeol   YutResult = "geol"
	YutYut    YutResult = "yut"
	YutMo     YutResult = "mo"
	YutBackdo YutResult = "backdo"
	YutBuk    YutResult = "buk"
)

// TurnPhase identifies a canonical turn state.
type TurnPhase string

const (
	TurnStart                     TurnPhase = "turn_start"
	TurnWaitThrow                 TurnPhase = "wait_throw"
	TurnThrowingChain             TurnPhase = "throwing_chain"
	TurnResolveQueue              TurnPhase = "resolve_queue"
	TurnWaitMoveSelection         TurnPhase = "wait_move_selection"
	TurnWaitRouteSelection        TurnPhase = "wait_route_selection"
	TurnApplyMove                 TurnPhase = "apply_move"
	TurnResolveStackCaptureFinish TurnPhase = "resolve_stack_capture_finish"
	TurnResolveBuk                TurnPhase = "resolve_buk"
	TurnCPUControl                TurnPhase = "cpu_control"
	TurnPaused                    TurnPhase = "paused"
	TurnEnd                       TurnPhase = "turn_end"
	TurnMatchEnd                  TurnPhase = "match_end"
)

// RequiredInput identifies the input expected during a turn.
type RequiredInput string

const (
	InputNone        RequiredInput = "none"
	InputThrow       RequiredInput = "throw"
	InputSelectMove  RequiredInput = "select_move"
	InputSelectRoute RequiredInput = "select_route"
)

// Route identifies the selected path at a board branch.
type Route string

const (
	RouteNormal   Route = "normal"
	RouteShortcut Route = "shortcut"
)

// MovementKind identifies the kind of an already resolved piece movement.
type MovementKind string

const (
	MovementForward MovementKind = "forward"
	MovementBackdo  MovementKind = "backdo"
	MovementBuk     MovementKind = "buk"
	MovementFinish  MovementKind = "finish"
)

func invalidEnum(kind, value string) error {
	return fmt.Errorf("%w: %s %q", ErrInvalidEnumValue, kind, value)
}

// Validate reports whether id is one of the canonical teams.
func (id TeamID) Validate() error {
	switch id {
	case TeamA, TeamB:
		return nil
	default:
		return invalidEnum("TeamID", string(id))
	}
}

// String returns the canonical JSON string value.
func (id TeamID) String() string {
	return string(id)
}

// MarshalJSON encodes id as a validated JSON string.
func (id TeamID) MarshalJSON() ([]byte, error) {
	return marshalValidatedString(id, TeamID.Validate)
}

// UnmarshalJSON decodes and validates a JSON string.
func (id *TeamID) UnmarshalJSON(data []byte) error {
	value, err := unmarshalValidatedString(data, TeamID.Validate)
	if err != nil {
		return err
	}
	*id = value
	return nil
}

// Validate reports whether state is canonical.
func (state PieceState) Validate() error {
	switch state {
	case PieceWaiting, PieceOnBoard, PieceHomeCheckpoint, PieceFinished:
		return nil
	default:
		return invalidEnum("PieceState", string(state))
	}
}

// String returns the canonical JSON string value.
func (state PieceState) String() string {
	return string(state)
}

// MarshalJSON encodes state as a validated JSON string.
func (state PieceState) MarshalJSON() ([]byte, error) {
	return marshalValidatedString(state, PieceState.Validate)
}

// UnmarshalJSON decodes and validates a JSON string.
func (state *PieceState) UnmarshalJSON(data []byte) error {
	value, err := unmarshalValidatedString(data, PieceState.Validate)
	if err != nil {
		return err
	}
	*state = value
	return nil
}

// Validate reports whether result is canonical.
func (result YutResult) Validate() error {
	switch result {
	case YutDo, YutGae, YutGeol, YutYut, YutMo, YutBackdo, YutBuk:
		return nil
	default:
		return invalidEnum("YutResult", string(result))
	}
}

// String returns the canonical JSON string value.
func (result YutResult) String() string {
	return string(result)
}

// MarshalJSON encodes result as a validated JSON string.
func (result YutResult) MarshalJSON() ([]byte, error) {
	return marshalValidatedString(result, YutResult.Validate)
}

// UnmarshalJSON decodes and validates a JSON string.
func (result *YutResult) UnmarshalJSON(data []byte) error {
	value, err := unmarshalValidatedString(data, YutResult.Validate)
	if err != nil {
		return err
	}
	*result = value
	return nil
}

// Validate reports whether phase is canonical.
func (phase TurnPhase) Validate() error {
	switch phase {
	case TurnStart,
		TurnWaitThrow,
		TurnThrowingChain,
		TurnResolveQueue,
		TurnWaitMoveSelection,
		TurnWaitRouteSelection,
		TurnApplyMove,
		TurnResolveStackCaptureFinish,
		TurnResolveBuk,
		TurnCPUControl,
		TurnPaused,
		TurnEnd,
		TurnMatchEnd:
		return nil
	default:
		return invalidEnum("TurnPhase", string(phase))
	}
}

// String returns the canonical JSON string value.
func (phase TurnPhase) String() string {
	return string(phase)
}

// MarshalJSON encodes phase as a validated JSON string.
func (phase TurnPhase) MarshalJSON() ([]byte, error) {
	return marshalValidatedString(phase, TurnPhase.Validate)
}

// UnmarshalJSON decodes and validates a JSON string.
func (phase *TurnPhase) UnmarshalJSON(data []byte) error {
	value, err := unmarshalValidatedString(data, TurnPhase.Validate)
	if err != nil {
		return err
	}
	*phase = value
	return nil
}

// Validate reports whether input is canonical.
func (input RequiredInput) Validate() error {
	switch input {
	case InputNone, InputThrow, InputSelectMove, InputSelectRoute:
		return nil
	default:
		return invalidEnum("RequiredInput", string(input))
	}
}

// String returns the canonical JSON string value.
func (input RequiredInput) String() string {
	return string(input)
}

// MarshalJSON encodes input as a validated JSON string.
func (input RequiredInput) MarshalJSON() ([]byte, error) {
	return marshalValidatedString(input, RequiredInput.Validate)
}

// UnmarshalJSON decodes and validates a JSON string.
func (input *RequiredInput) UnmarshalJSON(data []byte) error {
	value, err := unmarshalValidatedString(data, RequiredInput.Validate)
	if err != nil {
		return err
	}
	*input = value
	return nil
}

// Validate reports whether route is canonical.
func (route Route) Validate() error {
	switch route {
	case RouteNormal, RouteShortcut:
		return nil
	default:
		return invalidEnum("Route", string(route))
	}
}

// String returns the canonical JSON string value.
func (route Route) String() string {
	return string(route)
}

// MarshalJSON encodes route as a validated JSON string.
func (route Route) MarshalJSON() ([]byte, error) {
	return marshalValidatedString(route, Route.Validate)
}

// UnmarshalJSON decodes and validates a JSON string.
func (route *Route) UnmarshalJSON(data []byte) error {
	value, err := unmarshalValidatedString(data, Route.Validate)
	if err != nil {
		return err
	}
	*route = value
	return nil
}

// Validate reports whether kind is canonical.
func (kind MovementKind) Validate() error {
	switch kind {
	case MovementForward, MovementBackdo, MovementBuk, MovementFinish:
		return nil
	default:
		return invalidEnum("MovementKind", string(kind))
	}
}

// String returns the canonical JSON string value.
func (kind MovementKind) String() string {
	return string(kind)
}

// MarshalJSON encodes kind as a validated JSON string.
func (kind MovementKind) MarshalJSON() ([]byte, error) {
	return marshalValidatedString(kind, MovementKind.Validate)
}

// UnmarshalJSON decodes and validates a JSON string.
func (kind *MovementKind) UnmarshalJSON(data []byte) error {
	value, err := unmarshalValidatedString(data, MovementKind.Validate)
	if err != nil {
		return err
	}
	*kind = value
	return nil
}
