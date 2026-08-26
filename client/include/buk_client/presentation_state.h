#ifndef BUK_CLIENT_PRESENTATION_STATE_H
#define BUK_CLIENT_PRESENTATION_STATE_H

#include "buk_client/board_layout.h"

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

typedef enum BukClientMatchStatus {
    BUK_CLIENT_MATCH_STARTING,
    BUK_CLIENT_MATCH_ACTIVE,
    BUK_CLIENT_MATCH_PAUSED,
    BUK_CLIENT_MATCH_FINISHED,
    BUK_CLIENT_MATCH_INVALID,
    BUK_CLIENT_MATCH_STATUS_COUNT
} BukClientMatchStatus;

typedef enum BukClientTeam {
    BUK_CLIENT_TEAM_NONE,
    BUK_CLIENT_TEAM_A,
    BUK_CLIENT_TEAM_B,
    BUK_CLIENT_TEAM_COUNT
} BukClientTeam;

typedef enum BukClientTurnPhase {
    BUK_CLIENT_TURN_TURN_START,
    BUK_CLIENT_TURN_WAIT_THROW,
    BUK_CLIENT_TURN_THROWING_CHAIN,
    BUK_CLIENT_TURN_RESOLVE_QUEUE,
    BUK_CLIENT_TURN_WAIT_RESULT_SELECTION,
    BUK_CLIENT_TURN_WAIT_PIECE_SELECTION,
    BUK_CLIENT_TURN_WAIT_ROUTE_SELECTION,
    BUK_CLIENT_TURN_APPLY_MOVE,
    BUK_CLIENT_TURN_RESOLVE_STACK_CAPTURE_FINISH,
    BUK_CLIENT_TURN_RESOLVE_BUK,
    BUK_CLIENT_TURN_CPU_CONTROL,
    BUK_CLIENT_TURN_PAUSED,
    BUK_CLIENT_TURN_TURN_END,
    BUK_CLIENT_TURN_MATCH_END,
    BUK_CLIENT_TURN_PHASE_COUNT
} BukClientTurnPhase;

typedef enum BukClientRequiredInput {
    BUK_CLIENT_REQUIRED_NONE,
    BUK_CLIENT_REQUIRED_THROW,
    BUK_CLIENT_REQUIRED_SELECT_RESULT,
    BUK_CLIENT_REQUIRED_SELECT_PIECE,
    BUK_CLIENT_REQUIRED_SELECT_ROUTE,
    BUK_CLIENT_REQUIRED_INPUT_COUNT
} BukClientRequiredInput;

typedef enum BukClientTimerPhase {
    BUK_CLIENT_TIMER_THROW,
    BUK_CLIENT_TIMER_MOVE,
    BUK_CLIENT_TIMER_PAUSED,
    BUK_CLIENT_TIMER_NONE,
    BUK_CLIENT_TIMER_PHASE_COUNT
} BukClientTimerPhase;

typedef enum BukClientPieceState {
    BUK_CLIENT_PIECE_WAITING,
    BUK_CLIENT_PIECE_ON_BOARD,
    BUK_CLIENT_PIECE_HOME_CHECKPOINT,
    BUK_CLIENT_PIECE_FINISHED,
    BUK_CLIENT_PIECE_STATE_COUNT
} BukClientPieceState;

typedef enum BukClientResult {
    BUK_CLIENT_RESULT_DO,
    BUK_CLIENT_RESULT_GAE,
    BUK_CLIENT_RESULT_GEOL,
    BUK_CLIENT_RESULT_YUT,
    BUK_CLIENT_RESULT_MO,
    BUK_CLIENT_RESULT_BACKDO,
    BUK_CLIENT_RESULT_BUK,
    BUK_CLIENT_RESULT_COUNT
} BukClientResult;

typedef struct BukClientPresentationPiece {
    BukClientTeam team;
    BukClientPieceState state;
    BukClientBoardNodeId node;
    bool stacked;
    size_t stack_size;
} BukClientPresentationPiece;

typedef struct BukClientPresentationSnapshot {
    BukClientMatchStatus status;
    BukClientTurnPhase phase;
    BukClientRequiredInput required_input;
    BukClientTimerPhase timer_phase;
    BukClientTeam current_team;
    uint64_t remaining_ms;
    bool move_request_set;
    BukClientRequiredInput move_request_input;
    bool normal_route_available;
    bool shortcut_route_available;
    BukClientBoardNodeId route_origin;
    BukClientPresentationPiece *pieces;
    size_t piece_count;
    size_t piece_capacity;
    BukClientResult *results;
    size_t result_count;
    size_t result_capacity;
} BukClientPresentationSnapshot;

typedef struct BukClientPresentationState {
    BukClientPresentationSnapshot confirmed;
    BukClientPresentationSnapshot pending;
    bool confirmed_set;
    bool staging;
    bool pending_metadata_set;
    bool pending_failed;
} BukClientPresentationState;

void BukClientPresentationStateInit(BukClientPresentationState *state);
void BukClientPresentationStateDestroy(BukClientPresentationState *state);
void BukClientPresentationBeginSnapshot(BukClientPresentationState *state);
void BukClientPresentationAbortSnapshot(BukClientPresentationState *state);
bool BukClientPresentationStageMetadata(BukClientPresentationState *state,
                                        BukClientMatchStatus status,
                                        BukClientTurnPhase phase,
                                        BukClientRequiredInput required_input,
                                        BukClientTimerPhase timer_phase,
                                        BukClientTeam current_team,
                                        uint64_t remaining_ms);
bool BukClientPresentationStagePiece(BukClientPresentationState *state,
                                     BukClientTeam team,
                                     BukClientPieceState piece_state,
                                     BukClientBoardNodeId node,
                                     bool stacked,
                                     size_t stack_size);
bool BukClientPresentationStageResult(BukClientPresentationState *state,
                                      BukClientResult result);
bool BukClientPresentationStageMoveRequest(
    BukClientPresentationState *state, BukClientRequiredInput required_input,
    bool normal_route_available, bool shortcut_route_available,
    BukClientBoardNodeId route_origin);
bool BukClientPresentationCanCommit(const BukClientPresentationState *state);
bool BukClientPresentationCommitSnapshot(BukClientPresentationState *state);
const BukClientPresentationSnapshot *BukClientPresentationConfirmed(
    const BukClientPresentationState *state);

bool BukClientParseMatchStatus(const char *text, BukClientMatchStatus *status);
bool BukClientParseTeam(const char *text, BukClientTeam *team);
bool BukClientParseTurnPhase(const char *text, BukClientTurnPhase *phase);
bool BukClientParseRequiredInput(const char *text,
                                 BukClientRequiredInput *required_input);
bool BukClientParseTimerPhase(const char *text, BukClientTimerPhase *phase);
bool BukClientParsePieceState(const char *text, BukClientPieceState *piece_state);
bool BukClientParseResult(const char *text, BukClientResult *result);
const char *BukClientMatchStatusName(BukClientMatchStatus status);
const char *BukClientTeamName(BukClientTeam team);
const char *BukClientTurnPhaseName(BukClientTurnPhase phase);
const char *BukClientRequiredInputName(BukClientRequiredInput required_input);
const char *BukClientTimerPhaseName(BukClientTimerPhase phase);
const char *BukClientResultName(BukClientResult result);

#endif
