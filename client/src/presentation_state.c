#include "buk_client/presentation_state.h"

#include <stdint.h>
#include <stdlib.h>
#include <string.h>

typedef struct BukClientEnumName {
    const char *name;
    int value;
} BukClientEnumName;

static const BukClientEnumName match_status_names[] = {
    { "starting", BUK_CLIENT_MATCH_STARTING },
    { "active", BUK_CLIENT_MATCH_ACTIVE },
    { "paused", BUK_CLIENT_MATCH_PAUSED },
    { "finished", BUK_CLIENT_MATCH_FINISHED },
    { "invalid", BUK_CLIENT_MATCH_INVALID },
};

static const BukClientEnumName team_names[] = {
    { "", BUK_CLIENT_TEAM_NONE },
    { "A", BUK_CLIENT_TEAM_A },
    { "B", BUK_CLIENT_TEAM_B },
};

static const BukClientEnumName turn_phase_names[] = {
    { "turn_start", BUK_CLIENT_TURN_TURN_START },
    { "wait_throw", BUK_CLIENT_TURN_WAIT_THROW },
    { "throwing_chain", BUK_CLIENT_TURN_THROWING_CHAIN },
    { "resolve_queue", BUK_CLIENT_TURN_RESOLVE_QUEUE },
    { "wait_result_selection", BUK_CLIENT_TURN_WAIT_RESULT_SELECTION },
    { "wait_piece_selection", BUK_CLIENT_TURN_WAIT_PIECE_SELECTION },
    { "wait_route_selection", BUK_CLIENT_TURN_WAIT_ROUTE_SELECTION },
    { "apply_move", BUK_CLIENT_TURN_APPLY_MOVE },
    { "resolve_stack_capture_finish", BUK_CLIENT_TURN_RESOLVE_STACK_CAPTURE_FINISH },
    { "resolve_buk", BUK_CLIENT_TURN_RESOLVE_BUK },
    { "cpu_control", BUK_CLIENT_TURN_CPU_CONTROL },
    { "paused", BUK_CLIENT_TURN_PAUSED },
    { "turn_end", BUK_CLIENT_TURN_TURN_END },
    { "match_end", BUK_CLIENT_TURN_MATCH_END },
};

static const BukClientEnumName required_input_names[] = {
    { "none", BUK_CLIENT_REQUIRED_NONE },
    { "throw", BUK_CLIENT_REQUIRED_THROW },
    { "select_result", BUK_CLIENT_REQUIRED_SELECT_RESULT },
    { "select_piece", BUK_CLIENT_REQUIRED_SELECT_PIECE },
    { "select_route", BUK_CLIENT_REQUIRED_SELECT_ROUTE },
};

static const BukClientEnumName timer_phase_names[] = {
    { "throw", BUK_CLIENT_TIMER_THROW },
    { "move", BUK_CLIENT_TIMER_MOVE },
    { "paused", BUK_CLIENT_TIMER_PAUSED },
    { "none", BUK_CLIENT_TIMER_NONE },
};

static const BukClientEnumName piece_state_names[] = {
    { "waiting", BUK_CLIENT_PIECE_WAITING },
    { "on_board", BUK_CLIENT_PIECE_ON_BOARD },
    { "home_checkpoint", BUK_CLIENT_PIECE_HOME_CHECKPOINT },
    { "finished", BUK_CLIENT_PIECE_FINISHED },
};

static const BukClientEnumName result_names[] = {
    { "do", BUK_CLIENT_RESULT_DO },
    { "gae", BUK_CLIENT_RESULT_GAE },
    { "geol", BUK_CLIENT_RESULT_GEOL },
    { "yut", BUK_CLIENT_RESULT_YUT },
    { "mo", BUK_CLIENT_RESULT_MO },
    { "backdo", BUK_CLIENT_RESULT_BACKDO },
    { "buk", BUK_CLIENT_RESULT_BUK },
};

static bool ParseEnum(const BukClientEnumName *names, size_t count,
                      const char *text, int *value)
{
    size_t index;

    if ((names == NULL) || (text == NULL) || (value == NULL)) return false;
    for (index = 0U; index < count; index++) {
        if (strcmp(text, names[index].name) == 0) {
            *value = names[index].value;
            return true;
        }
    }
    return false;
}

static const char *EnumName(const BukClientEnumName *names, size_t count, int value)
{
    size_t index;

    for (index = 0U; index < count; index++) {
        if (names[index].value == value) return names[index].name;
    }
    return "unknown";
}

static void SnapshotDestroy(BukClientPresentationSnapshot *snapshot)
{
    if (snapshot == NULL) return;
    free(snapshot->pieces);
    free(snapshot->results);
    *snapshot = (BukClientPresentationSnapshot){ 0 };
}

static bool EnsurePieceCapacity(BukClientPresentationState *state)
{
    BukClientPresentationPiece *pieces;
    size_t new_capacity;

    if (state->pending.piece_count < state->pending.piece_capacity) return true;
    new_capacity = state->pending.piece_capacity == 0U
                       ? 8U
                       : state->pending.piece_capacity * 2U;
    if ((new_capacity < state->pending.piece_capacity) ||
        (new_capacity > SIZE_MAX / sizeof(*pieces))) {
        state->pending_failed = true;
        return false;
    }
    pieces = realloc(state->pending.pieces, new_capacity * sizeof(*pieces));
    if (pieces == NULL) {
        state->pending_failed = true;
        return false;
    }
    state->pending.pieces = pieces;
    state->pending.piece_capacity = new_capacity;
    return true;
}

static bool EnsureResultCapacity(BukClientPresentationState *state)
{
    BukClientResult *results;
    size_t new_capacity;

    if (state->pending.result_count < state->pending.result_capacity) return true;
    new_capacity = state->pending.result_capacity == 0U
                       ? 8U
                       : state->pending.result_capacity * 2U;
    if ((new_capacity < state->pending.result_capacity) ||
        (new_capacity > SIZE_MAX / sizeof(*results))) {
        state->pending_failed = true;
        return false;
    }
    results = realloc(state->pending.results, new_capacity * sizeof(*results));
    if (results == NULL) {
        state->pending_failed = true;
        return false;
    }
    state->pending.results = results;
    state->pending.result_capacity = new_capacity;
    return true;
}

void BukClientPresentationStateInit(BukClientPresentationState *state)
{
    if (state == NULL) return;
    *state = (BukClientPresentationState){ 0 };
}

void BukClientPresentationStateDestroy(BukClientPresentationState *state)
{
    if (state == NULL) return;
    SnapshotDestroy(&state->confirmed);
    SnapshotDestroy(&state->pending);
    *state = (BukClientPresentationState){ 0 };
}

void BukClientPresentationBeginSnapshot(BukClientPresentationState *state)
{
    if (state == NULL) return;
    SnapshotDestroy(&state->pending);
    state->staging = true;
    state->pending_metadata_set = false;
    state->pending_failed = false;
}

void BukClientPresentationAbortSnapshot(BukClientPresentationState *state)
{
    if (state == NULL) return;
    SnapshotDestroy(&state->pending);
    state->staging = false;
    state->pending_metadata_set = false;
    state->pending_failed = false;
}

bool BukClientPresentationStageMetadata(BukClientPresentationState *state,
                                        BukClientMatchStatus status,
                                        BukClientTurnPhase phase,
                                        BukClientRequiredInput required_input,
                                        BukClientTimerPhase timer_phase,
                                        BukClientTeam current_team,
                                        uint64_t remaining_ms)
{
    if ((state == NULL) || !state->staging || state->pending_failed ||
        state->pending_metadata_set || (status < 0) ||
        (status >= BUK_CLIENT_MATCH_STATUS_COUNT) || (phase < 0) ||
        (phase >= BUK_CLIENT_TURN_PHASE_COUNT) || (required_input < 0) ||
        (required_input >= BUK_CLIENT_REQUIRED_INPUT_COUNT) || (timer_phase < 0) ||
        (timer_phase >= BUK_CLIENT_TIMER_PHASE_COUNT) || (current_team < 0) ||
        (current_team >= BUK_CLIENT_TEAM_COUNT)) {
        if (state != NULL) state->pending_failed = true;
        return false;
    }
    state->pending.status = status;
    state->pending.phase = phase;
    state->pending.required_input = required_input;
    state->pending.timer_phase = timer_phase;
    state->pending.current_team = current_team;
    state->pending.remaining_ms = remaining_ms;
    state->pending_metadata_set = true;
    return true;
}

bool BukClientPresentationStagePiece(BukClientPresentationState *state,
                                     BukClientTeam team,
                                     BukClientPieceState piece_state,
                                     BukClientBoardNodeId node,
                                     bool stacked,
                                     size_t stack_size)
{
    bool has_board_node;

    if ((state == NULL) || !state->staging || state->pending_failed ||
        (team != BUK_CLIENT_TEAM_A && team != BUK_CLIENT_TEAM_B) ||
        (piece_state < 0) || (piece_state >= BUK_CLIENT_PIECE_STATE_COUNT)) {
        if (state != NULL) state->pending_failed = true;
        return false;
    }
    if ((!stacked && stack_size != 0U) ||
        (stacked && stack_size < 2U) ||
        (stacked && (piece_state == BUK_CLIENT_PIECE_WAITING ||
                     piece_state == BUK_CLIENT_PIECE_FINISHED))) {
        state->pending_failed = true;
        return false;
    }
    has_board_node = (node >= 0) && (node < BUK_CLIENT_BOARD_NODE_COUNT);
    if (((piece_state == BUK_CLIENT_PIECE_WAITING ||
          piece_state == BUK_CLIENT_PIECE_FINISHED) && has_board_node) ||
        ((piece_state == BUK_CLIENT_PIECE_ON_BOARD ||
          piece_state == BUK_CLIENT_PIECE_HOME_CHECKPOINT) && !has_board_node) ||
        (piece_state == BUK_CLIENT_PIECE_HOME_CHECKPOINT &&
         node != BUK_CLIENT_BOARD_NODE_CHAMMEOGI)) {
        state->pending_failed = true;
        return false;
    }
    if (!EnsurePieceCapacity(state)) return false;
    state->pending.pieces[state->pending.piece_count] =
        (BukClientPresentationPiece){ team, piece_state, node, stacked, stack_size };
    state->pending.piece_count++;
    return true;
}

bool BukClientPresentationStageResult(BukClientPresentationState *state,
                                      BukClientResult result)
{
    if ((state == NULL) || !state->staging || state->pending_failed ||
        (result < 0) || (result >= BUK_CLIENT_RESULT_COUNT)) {
        if (state != NULL) state->pending_failed = true;
        return false;
    }
    if (!EnsureResultCapacity(state)) return false;
    state->pending.results[state->pending.result_count] = result;
    state->pending.result_count++;
    return true;
}

bool BukClientPresentationStageMoveRequest(
    BukClientPresentationState *state, BukClientRequiredInput required_input,
    bool normal_route_available, bool shortcut_route_available,
    BukClientBoardNodeId route_origin)
{
    BukClientBoardNodeId normal;
    BukClientBoardNodeId shortcut;
    bool route_request = required_input == BUK_CLIENT_REQUIRED_SELECT_ROUTE;

    if ((state == NULL) || !state->staging || state->pending_failed ||
        state->pending.move_request_set || !state->pending_metadata_set ||
        (required_input != BUK_CLIENT_REQUIRED_SELECT_RESULT &&
         required_input != BUK_CLIENT_REQUIRED_SELECT_PIECE &&
         required_input != BUK_CLIENT_REQUIRED_SELECT_ROUTE) ||
        state->pending.required_input != required_input ||
        (route_request &&
         (!normal_route_available || !shortcut_route_available ||
          !BukClientBoardRouteTargets(route_origin, &normal, &shortcut))) ||
        (!route_request &&
         (normal_route_available || shortcut_route_available ||
          route_origin != BUK_CLIENT_BOARD_NODE_COUNT))) {
        if (state != NULL) state->pending_failed = true;
        return false;
    }
    state->pending.move_request_set = true;
    state->pending.move_request_input = required_input;
    state->pending.normal_route_available = normal_route_available;
    state->pending.shortcut_route_available = shortcut_route_available;
    state->pending.route_origin = route_origin;
    return true;
}

bool BukClientPresentationCanCommit(const BukClientPresentationState *state)
{
    bool selection_input;

    if ((state == NULL) || !state->staging || !state->pending_metadata_set ||
        state->pending_failed) {
        return false;
    }
    selection_input =
        state->pending.required_input == BUK_CLIENT_REQUIRED_SELECT_RESULT ||
        state->pending.required_input == BUK_CLIENT_REQUIRED_SELECT_PIECE ||
        state->pending.required_input == BUK_CLIENT_REQUIRED_SELECT_ROUTE;
    return selection_input == state->pending.move_request_set;
}

bool BukClientPresentationCommitSnapshot(BukClientPresentationState *state)
{
    if (!BukClientPresentationCanCommit(state)) return false;
    SnapshotDestroy(&state->confirmed);
    state->confirmed = state->pending;
    state->pending = (BukClientPresentationSnapshot){ 0 };
    state->confirmed_set = true;
    state->staging = false;
    state->pending_metadata_set = false;
    state->pending_failed = false;
    return true;
}

const BukClientPresentationSnapshot *BukClientPresentationConfirmed(
    const BukClientPresentationState *state)
{
    if ((state == NULL) || !state->confirmed_set) return NULL;
    return &state->confirmed;
}

#define DEFINE_PARSE_FUNCTION(function_name, enum_type, table_name)                  \
    bool function_name(const char *text, enum_type *value)                           \
    {                                                                                 \
        int parsed;                                                                   \
        if ((value == NULL) ||                                                        \
            !ParseEnum(table_name, sizeof(table_name) / sizeof(table_name[0]), text,  \
                       &parsed)) {                                                    \
            return false;                                                            \
        }                                                                             \
        *value = (enum_type)parsed;                                                   \
        return true;                                                                  \
    }

DEFINE_PARSE_FUNCTION(BukClientParseMatchStatus, BukClientMatchStatus,
                      match_status_names)
DEFINE_PARSE_FUNCTION(BukClientParseTeam, BukClientTeam, team_names)
DEFINE_PARSE_FUNCTION(BukClientParseTurnPhase, BukClientTurnPhase, turn_phase_names)
DEFINE_PARSE_FUNCTION(BukClientParseRequiredInput, BukClientRequiredInput,
                      required_input_names)
DEFINE_PARSE_FUNCTION(BukClientParseTimerPhase, BukClientTimerPhase, timer_phase_names)
DEFINE_PARSE_FUNCTION(BukClientParsePieceState, BukClientPieceState, piece_state_names)
DEFINE_PARSE_FUNCTION(BukClientParseResult, BukClientResult, result_names)

#define DEFINE_NAME_FUNCTION(function_name, enum_type, table_name)                   \
    const char *function_name(enum_type value)                                        \
    {                                                                                 \
        return EnumName(table_name, sizeof(table_name) / sizeof(table_name[0]),       \
                        (int)value);                                                   \
    }

DEFINE_NAME_FUNCTION(BukClientMatchStatusName, BukClientMatchStatus,
                     match_status_names)
DEFINE_NAME_FUNCTION(BukClientTeamName, BukClientTeam, team_names)
DEFINE_NAME_FUNCTION(BukClientTurnPhaseName, BukClientTurnPhase, turn_phase_names)
DEFINE_NAME_FUNCTION(BukClientRequiredInputName, BukClientRequiredInput,
                     required_input_names)
DEFINE_NAME_FUNCTION(BukClientTimerPhaseName, BukClientTimerPhase, timer_phase_names)
DEFINE_NAME_FUNCTION(BukClientResultName, BukClientResult, result_names)
