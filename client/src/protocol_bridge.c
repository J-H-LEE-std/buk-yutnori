#include "buk_client/bridge.h"
#include "buk_client/board_layout.h"
#include "buk_client/presentation_state.h"
#include "buk_client/protocol_state.h"

#include <inttypes.h>
#include <limits.h>
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>

enum {
    SEQUENCE_TEXT_CAPACITY = 21,
    ROUTE_INTENT_NONE = 0,
    ROUTE_INTENT_NORMAL = 1,
    ROUTE_INTENT_SHORTCUT = 2,
};

static BukClientProtocolState protocol_state;
static BukClientPresentationState presentation_state;
static bool presentation_runtime_initialized;
static bool pending_event_tail;
static bool snapshot_sequence_staged;
static bool route_interaction_enabled;
static bool route_command_pending;
static int route_intent;
static bool route_intent_set;
static char last_sequence_text[SEQUENCE_TEXT_CAPACITY];
static char remaining_ms_text[SEQUENCE_TEXT_CAPACITY];

static void ResetRouteInteraction(void)
{
    route_interaction_enabled = false;
    route_command_pending = false;
    route_intent = ROUTE_INTENT_NONE;
    route_intent_set = false;
}

static void FailRuntimeSynchronization(void)
{
    BukClientProtocolBeginSynchronization(&protocol_state);
    (void)BukClientProtocolApplySnapshot(&protocol_state, 0U);
    BukClientPresentationAbortSnapshot(&presentation_state);
    pending_event_tail = false;
    snapshot_sequence_staged = false;
    ResetRouteInteraction();
}

static bool ParseSequence(const char *text, uint64_t *sequence)
{
    uint64_t value = 0U;
    size_t index = 0U;

    if ((text == NULL) || (sequence == NULL) || (text[0] == '\0')) return false;
    while (text[index] != '\0') {
        uint64_t digit;

        if ((text[index] < '0') || (text[index] > '9')) return false;
        digit = (uint64_t)(text[index] - '0');
        if (value > (UINT64_MAX - digit) / 10U) return false;
        value = value * 10U + digit;
        index++;
    }
    *sequence = value;
    return true;
}

void BukClientProtocolRuntimeInit(void)
{
    if (presentation_runtime_initialized) {
        BukClientPresentationStateDestroy(&presentation_state);
    }
    BukClientProtocolStateInit(&protocol_state);
    BukClientPresentationStateInit(&presentation_state);
    presentation_runtime_initialized = true;
    pending_event_tail = false;
    snapshot_sequence_staged = false;
    ResetRouteInteraction();
}

int BukClientBeginSynchronization(void)
{
    BukClientProtocolBeginSynchronization(&protocol_state);
    BukClientPresentationBeginSnapshot(&presentation_state);
    pending_event_tail = false;
    snapshot_sequence_staged = false;
    ResetRouteInteraction();
    return 1;
}

int BukClientApplySnapshotSequence(const char *sequence)
{
    uint64_t parsed;

    if (!ParseSequence(sequence, &parsed)) {
        FailRuntimeSynchronization();
        return 0;
    }
    if (!BukClientProtocolApplySnapshot(&protocol_state, parsed)) {
        FailRuntimeSynchronization();
        return 0;
    }
    snapshot_sequence_staged = true;
    return 1;
}

int BukClientApplyEventSequence(const char *sequence)
{
    uint64_t parsed;

    if (!snapshot_sequence_staged || !ParseSequence(sequence, &parsed)) {
        FailRuntimeSynchronization();
        return 0;
    }
    if (!BukClientProtocolApplyEvent(&protocol_state, parsed)) {
        FailRuntimeSynchronization();
        return 0;
    }
    pending_event_tail = true;
    return 1;
}

int BukClientStageSnapshotMetadata(const char *status, const char *phase,
                                   const char *required_input,
                                   const char *timer_phase,
                                   const char *current_team,
                                   const char *remaining_ms)
{
    BukClientMatchStatus parsed_status;
    BukClientTurnPhase parsed_phase;
    BukClientRequiredInput parsed_required_input;
    BukClientTimerPhase parsed_timer_phase;
    BukClientTeam parsed_team;
    uint64_t parsed_remaining_ms;

    if (!BukClientParseMatchStatus(status, &parsed_status) ||
        !BukClientParseTurnPhase(phase, &parsed_phase) ||
        !BukClientParseRequiredInput(required_input, &parsed_required_input) ||
        !BukClientParseTimerPhase(timer_phase, &parsed_timer_phase) ||
        !BukClientParseTeam(current_team, &parsed_team) ||
        !ParseSequence(remaining_ms, &parsed_remaining_ms) ||
        !BukClientPresentationStageMetadata(
            &presentation_state, parsed_status, parsed_phase, parsed_required_input,
            parsed_timer_phase, parsed_team, parsed_remaining_ms)) {
        FailRuntimeSynchronization();
        return 0;
    }
    return 1;
}

int BukClientStageSnapshotPiece(const char *team, const char *piece_state,
                                const char *space_id, int stacked, int stack_size)
{
    BukClientTeam parsed_team;
    BukClientPieceState parsed_piece_state;
    BukClientBoardNodeId node = BUK_CLIENT_BOARD_NODE_COUNT;

    if (!BukClientParseTeam(team, &parsed_team) ||
        !BukClientParsePieceState(piece_state, &parsed_piece_state) ||
        (stacked != 0 && stacked != 1) || stack_size < 0 ||
        (space_id == NULL) ||
        (space_id[0] != '\0' && !BukClientBoardFindNode(space_id, &node)) ||
        !BukClientPresentationStagePiece(&presentation_state, parsed_team,
                                         parsed_piece_state, node, stacked == 1,
                                         (size_t)stack_size)) {
        FailRuntimeSynchronization();
        return 0;
    }
    return 1;
}

int BukClientStageSnapshotResult(const char *result)
{
    BukClientResult parsed_result;

    if (!BukClientParseResult(result, &parsed_result) ||
        !BukClientPresentationStageResult(&presentation_state, parsed_result)) {
        FailRuntimeSynchronization();
        return 0;
    }
    return 1;
}

int BukClientStageSnapshotMoveRequest(const char *required_input,
                                      int normal_route_available,
                                      int shortcut_route_available,
                                      const char *route_origin_space_id)
{
    BukClientRequiredInput parsed_required_input;
    BukClientBoardNodeId route_origin = BUK_CLIENT_BOARD_NODE_COUNT;

    if (!BukClientParseRequiredInput(required_input, &parsed_required_input) ||
        (normal_route_available != 0 && normal_route_available != 1) ||
        (shortcut_route_available != 0 && shortcut_route_available != 1) ||
        (route_origin_space_id == NULL) ||
        (route_origin_space_id[0] != '\0' &&
         !BukClientBoardFindNode(route_origin_space_id, &route_origin)) ||
        !BukClientPresentationStageMoveRequest(
            &presentation_state, parsed_required_input,
            normal_route_available == 1, shortcut_route_available == 1,
            route_origin)) {
        FailRuntimeSynchronization();
        return 0;
    }
    return 1;
}

int BukClientIsCanonicalBoardSpace(const char *space_id)
{
    BukClientBoardNodeId node;

    return BukClientBoardFindNode(space_id, &node) ? 1 : 0;
}

int BukClientFailSynchronization(void)
{
    FailRuntimeSynchronization();
    return 1;
}

int BukClientCompleteSynchronization(void)
{
    BukClientProtocolState previous_protocol_state;

    if (pending_event_tail ||
        !BukClientPresentationCanCommit(&presentation_state)) {
        FailRuntimeSynchronization();
        return 0;
    }
    previous_protocol_state = protocol_state;
    if (!BukClientProtocolCompleteSynchronization(&protocol_state)) {
        FailRuntimeSynchronization();
        return 0;
    }
    if (!BukClientPresentationCommitSnapshot(&presentation_state)) {
        protocol_state = previous_protocol_state;
        FailRuntimeSynchronization();
        return 0;
    }
    pending_event_tail = false;
    snapshot_sequence_staged = false;
    ResetRouteInteraction();
    return 1;
}

int BukClientCanSendStateCommands(void)
{
    return BukClientProtocolCanSendCommands(&protocol_state) ? 1 : 0;
}

int BukClientRequiresResynchronization(void)
{
    return BukClientProtocolRequiresSynchronization(&protocol_state) ? 1 : 0;
}

const char *BukClientLastSequence(void)
{
    (void)snprintf(last_sequence_text, sizeof(last_sequence_text), "%" PRIu64,
                   BukClientProtocolLastSequence(&protocol_state));
    return last_sequence_text;
}

const BukClientPresentationSnapshot *BukClientConfirmedPresentation(void)
{
    return BukClientPresentationConfirmed(&presentation_state);
}

int BukClientHasPresentationSnapshot(void)
{
    return BukClientConfirmedPresentation() != NULL ? 1 : 0;
}

int BukClientPresentationPieceCount(void)
{
    const BukClientPresentationSnapshot *snapshot = BukClientConfirmedPresentation();

    if (snapshot == NULL) return 0;
    if (snapshot->piece_count > (size_t)INT_MAX) return INT_MAX;
    return (int)snapshot->piece_count;
}

int BukClientPresentationResultCount(void)
{
    const BukClientPresentationSnapshot *snapshot = BukClientConfirmedPresentation();

    if (snapshot == NULL) return 0;
    if (snapshot->result_count > (size_t)INT_MAX) return INT_MAX;
    return (int)snapshot->result_count;
}

const char *BukClientPresentationStatus(void)
{
    const BukClientPresentationSnapshot *snapshot = BukClientConfirmedPresentation();

    return snapshot == NULL ? "none" : BukClientMatchStatusName(snapshot->status);
}

const char *BukClientPresentationTurnPhase(void)
{
    const BukClientPresentationSnapshot *snapshot = BukClientConfirmedPresentation();

    return snapshot == NULL ? "none" : BukClientTurnPhaseName(snapshot->phase);
}

const char *BukClientPresentationRequiredInput(void)
{
    const BukClientPresentationSnapshot *snapshot = BukClientConfirmedPresentation();

    return snapshot == NULL ? "none" :
                              BukClientRequiredInputName(snapshot->required_input);
}

const char *BukClientPresentationCurrentTeam(void)
{
    const BukClientPresentationSnapshot *snapshot = BukClientConfirmedPresentation();

    return snapshot == NULL ? "" : BukClientTeamName(snapshot->current_team);
}

const char *BukClientPresentationRemainingMilliseconds(void)
{
    const BukClientPresentationSnapshot *snapshot = BukClientConfirmedPresentation();

    (void)snprintf(remaining_ms_text, sizeof(remaining_ms_text), "%" PRIu64,
                   snapshot == NULL ? 0U : snapshot->remaining_ms);
    return remaining_ms_text;
}

int BukClientSetRouteInteractionEnabled(int enabled)
{
    const BukClientPresentationSnapshot *snapshot = BukClientConfirmedPresentation();

    if (enabled != 0 && enabled != 1) return 0;
    if (enabled == 1 &&
        (snapshot == NULL || !snapshot->move_request_set ||
         snapshot->move_request_input != BUK_CLIENT_REQUIRED_SELECT_ROUTE ||
         !snapshot->normal_route_available ||
         !snapshot->shortcut_route_available)) {
        return 0;
    }
    route_interaction_enabled = enabled == 1;
    if (!route_interaction_enabled) {
        route_command_pending = false;
        route_intent_set = false;
    }
    return 1;
}

int BukClientCanSelectRoute(void)
{
    return route_interaction_enabled && !route_command_pending &&
                   BukClientCanSendStateCommands()
               ? 1
               : 0;
}

int BukClientRequestRouteSelection(const char *route)
{
    const BukClientPresentationSnapshot *snapshot = BukClientConfirmedPresentation();
    int intent;

    if (!BukClientCanSelectRoute() || route == NULL || snapshot == NULL) return 0;
    if (strcmp(route, "normal") == 0 && snapshot->normal_route_available) {
        intent = ROUTE_INTENT_NORMAL;
    } else if (strcmp(route, "shortcut") == 0 &&
               snapshot->shortcut_route_available) {
        intent = ROUTE_INTENT_SHORTCUT;
    } else {
        return 0;
    }
    route_intent = intent;
    route_intent_set = true;
    route_command_pending = true;
    return 1;
}

const char *BukClientConsumeRouteSelection(void)
{
    const char *route;

    if (!route_intent_set) return "";
    route = route_intent == ROUTE_INTENT_NORMAL ? "normal" : "shortcut";
    route_intent = ROUTE_INTENT_NONE;
    route_intent_set = false;
    return route;
}

int BukClientResolveRouteCommand(void)
{
    if (!route_command_pending) return 0;
    route_command_pending = false;
    route_intent_set = false;
    return 1;
}
