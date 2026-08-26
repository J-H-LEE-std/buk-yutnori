#include "buk_client/presentation_state.h"

#include <assert.h>
#include <stdint.h>
#include <stdio.h>

static void StageMetadata(BukClientPresentationState *state,
                          BukClientMatchStatus status,
                          BukClientTurnPhase phase,
                          BukClientRequiredInput required_input,
                          BukClientTimerPhase timer_phase,
                          BukClientTeam current_team,
                          uint64_t remaining_ms)
{
    assert(BukClientPresentationStageMetadata(state, status, phase, required_input,
                                               timer_phase, current_team, remaining_ms));
}

static void TestCommitsAuthoritativeSnapshot(void)
{
    BukClientPresentationState state;
    const BukClientPresentationSnapshot *snapshot;

    BukClientPresentationStateInit(&state);
    BukClientPresentationBeginSnapshot(&state);
    StageMetadata(&state, BUK_CLIENT_MATCH_ACTIVE,
                  BUK_CLIENT_TURN_WAIT_PIECE_SELECTION,
                  BUK_CLIENT_REQUIRED_SELECT_PIECE, BUK_CLIENT_TIMER_MOVE,
                  BUK_CLIENT_TEAM_A, 52000U);
    assert(BukClientPresentationStageMoveRequest(
        &state, BUK_CLIENT_REQUIRED_SELECT_PIECE, false, false,
        BUK_CLIENT_BOARD_NODE_COUNT));
    assert(BukClientPresentationStagePiece(
        &state, BUK_CLIENT_TEAM_A, BUK_CLIENT_PIECE_ON_BOARD,
        BUK_CLIENT_BOARD_NODE_BANG, false, 0U));
    assert(BukClientPresentationStagePiece(
        &state, BUK_CLIENT_TEAM_B, BUK_CLIENT_PIECE_WAITING,
        BUK_CLIENT_BOARD_NODE_COUNT, false, 0U));
    assert(BukClientPresentationStageResult(&state, BUK_CLIENT_RESULT_GAE));
    assert(BukClientPresentationStageResult(&state, BUK_CLIENT_RESULT_BUK));
    assert(BukClientPresentationCanCommit(&state));
    assert(BukClientPresentationCommitSnapshot(&state));

    snapshot = BukClientPresentationConfirmed(&state);
    assert(snapshot != NULL);
    assert(snapshot->status == BUK_CLIENT_MATCH_ACTIVE);
    assert(snapshot->phase == BUK_CLIENT_TURN_WAIT_PIECE_SELECTION);
    assert(snapshot->required_input == BUK_CLIENT_REQUIRED_SELECT_PIECE);
    assert(snapshot->timer_phase == BUK_CLIENT_TIMER_MOVE);
    assert(snapshot->current_team == BUK_CLIENT_TEAM_A);
    assert(snapshot->remaining_ms == 52000U);
    assert(snapshot->piece_count == 2U);
    assert(snapshot->pieces[0].team == BUK_CLIENT_TEAM_A);
    assert(snapshot->pieces[0].node == BUK_CLIENT_BOARD_NODE_BANG);
    assert(snapshot->pieces[1].state == BUK_CLIENT_PIECE_WAITING);
    assert(snapshot->result_count == 2U);
    assert(snapshot->results[0] == BUK_CLIENT_RESULT_GAE);
    assert(snapshot->results[1] == BUK_CLIENT_RESULT_BUK);
    BukClientPresentationStateDestroy(&state);
}

static void TestFailedSnapshotPreservesConfirmedState(void)
{
    BukClientPresentationState state;
    const BukClientPresentationSnapshot *snapshot;

    BukClientPresentationStateInit(&state);
    BukClientPresentationBeginSnapshot(&state);
    StageMetadata(&state, BUK_CLIENT_MATCH_STARTING, BUK_CLIENT_TURN_TURN_START,
                  BUK_CLIENT_REQUIRED_NONE, BUK_CLIENT_TIMER_NONE,
                  BUK_CLIENT_TEAM_NONE, 0U);
    assert(BukClientPresentationStagePiece(
        &state, BUK_CLIENT_TEAM_A, BUK_CLIENT_PIECE_WAITING,
        BUK_CLIENT_BOARD_NODE_COUNT, false, 0U));
    assert(BukClientPresentationCommitSnapshot(&state));

    BukClientPresentationBeginSnapshot(&state);
    StageMetadata(&state, BUK_CLIENT_MATCH_ACTIVE, BUK_CLIENT_TURN_WAIT_THROW,
                  BUK_CLIENT_REQUIRED_THROW, BUK_CLIENT_TIMER_THROW,
                  BUK_CLIENT_TEAM_B, 20000U);
    assert(!BukClientPresentationStagePiece(
        &state, BUK_CLIENT_TEAM_B, BUK_CLIENT_PIECE_ON_BOARD,
        BUK_CLIENT_BOARD_NODE_COUNT, false, 0U));
    assert(!BukClientPresentationCanCommit(&state));
    assert(!BukClientPresentationCommitSnapshot(&state));

    snapshot = BukClientPresentationConfirmed(&state);
    assert(snapshot != NULL);
    assert(snapshot->status == BUK_CLIENT_MATCH_STARTING);
    assert(snapshot->piece_count == 1U);
    assert(snapshot->pieces[0].team == BUK_CLIENT_TEAM_A);
    BukClientPresentationStateDestroy(&state);
}

static void TestRejectsInvalidPieceStateCombinations(void)
{
    BukClientPresentationState state;

    BukClientPresentationStateInit(&state);
    BukClientPresentationBeginSnapshot(&state);
    StageMetadata(&state, BUK_CLIENT_MATCH_ACTIVE, BUK_CLIENT_TURN_WAIT_THROW,
                  BUK_CLIENT_REQUIRED_THROW, BUK_CLIENT_TIMER_THROW,
                  BUK_CLIENT_TEAM_A, 1000U);
    assert(!BukClientPresentationStagePiece(
        &state, BUK_CLIENT_TEAM_A, BUK_CLIENT_PIECE_WAITING,
        BUK_CLIENT_BOARD_NODE_DO, false, 0U));
    assert(!BukClientPresentationCommitSnapshot(&state));

    BukClientPresentationBeginSnapshot(&state);
    StageMetadata(&state, BUK_CLIENT_MATCH_ACTIVE, BUK_CLIENT_TURN_WAIT_THROW,
                  BUK_CLIENT_REQUIRED_THROW, BUK_CLIENT_TIMER_THROW,
                  BUK_CLIENT_TEAM_A, 1000U);
    assert(!BukClientPresentationStagePiece(
        &state, BUK_CLIENT_TEAM_B, BUK_CLIENT_PIECE_HOME_CHECKPOINT,
        BUK_CLIENT_BOARD_NODE_DO, false, 0U));
    assert(!BukClientPresentationCommitSnapshot(&state));
    BukClientPresentationStateDestroy(&state);
}

static void TestCarriesAuthoritativeStackMembership(void)
{
    BukClientPresentationState state;
    const BukClientPresentationSnapshot *snapshot;

    BukClientPresentationStateInit(&state);
    BukClientPresentationBeginSnapshot(&state);
    StageMetadata(&state, BUK_CLIENT_MATCH_ACTIVE, BUK_CLIENT_TURN_WAIT_THROW,
                  BUK_CLIENT_REQUIRED_THROW, BUK_CLIENT_TIMER_THROW,
                  BUK_CLIENT_TEAM_A, 1000U);
    assert(BukClientPresentationStagePiece(
        &state, BUK_CLIENT_TEAM_A, BUK_CLIENT_PIECE_ON_BOARD,
        BUK_CLIENT_BOARD_NODE_DO, true, 2U));
    assert(!BukClientPresentationStagePiece(
        &state, BUK_CLIENT_TEAM_A, BUK_CLIENT_PIECE_ON_BOARD,
        BUK_CLIENT_BOARD_NODE_DO, true, 1U));
    assert(!BukClientPresentationCanCommit(&state));

    BukClientPresentationBeginSnapshot(&state);
    StageMetadata(&state, BUK_CLIENT_MATCH_ACTIVE, BUK_CLIENT_TURN_WAIT_THROW,
                  BUK_CLIENT_REQUIRED_THROW, BUK_CLIENT_TIMER_THROW,
                  BUK_CLIENT_TEAM_A, 1000U);
    assert(BukClientPresentationStagePiece(
        &state, BUK_CLIENT_TEAM_A, BUK_CLIENT_PIECE_ON_BOARD,
        BUK_CLIENT_BOARD_NODE_DO, true, 2U));
    assert(BukClientPresentationStagePiece(
        &state, BUK_CLIENT_TEAM_A, BUK_CLIENT_PIECE_ON_BOARD,
        BUK_CLIENT_BOARD_NODE_DO, true, 2U));
    assert(BukClientPresentationCommitSnapshot(&state));
    snapshot = BukClientPresentationConfirmed(&state);
    assert(snapshot != NULL && snapshot->pieces[0].stacked &&
           snapshot->pieces[0].stack_size == 2U);
    BukClientPresentationStateDestroy(&state);
}

static void TestRequiresExactlyOneMetadataRecord(void)
{
    BukClientPresentationState state;

    BukClientPresentationStateInit(&state);
    BukClientPresentationBeginSnapshot(&state);
    assert(!BukClientPresentationCanCommit(&state));
    assert(!BukClientPresentationCommitSnapshot(&state));

    BukClientPresentationBeginSnapshot(&state);
    StageMetadata(&state, BUK_CLIENT_MATCH_ACTIVE, BUK_CLIENT_TURN_WAIT_THROW,
                  BUK_CLIENT_REQUIRED_THROW, BUK_CLIENT_TIMER_THROW,
                  BUK_CLIENT_TEAM_A, 1000U);
    assert(!BukClientPresentationStageMetadata(
        &state, BUK_CLIENT_MATCH_ACTIVE, BUK_CLIENT_TURN_WAIT_THROW,
        BUK_CLIENT_REQUIRED_THROW, BUK_CLIENT_TIMER_THROW,
        BUK_CLIENT_TEAM_A, 1000U));
    assert(!BukClientPresentationCommitSnapshot(&state));
    BukClientPresentationStateDestroy(&state);
}

static void TestParsesOnlyCanonicalTokens(void)
{
    BukClientMatchStatus status;
    BukClientTurnPhase phase;
    BukClientRequiredInput required_input;
    BukClientTimerPhase timer_phase;
    BukClientTeam team;
    BukClientPieceState piece_state;
    BukClientResult result;

    assert(BukClientParseMatchStatus("invalid", &status));
    assert(status == BUK_CLIENT_MATCH_INVALID);
    assert(!BukClientParseMatchStatus("ACTIVE", &status));
    assert(BukClientParseTurnPhase("resolve_buk", &phase));
    assert(phase == BUK_CLIENT_TURN_RESOLVE_BUK);
    assert(!BukClientParseTurnPhase("resolve-buk", &phase));
    assert(BukClientParseRequiredInput("select_route", &required_input));
    assert(required_input == BUK_CLIENT_REQUIRED_SELECT_ROUTE);
    assert(BukClientParseTimerPhase("paused", &timer_phase));
    assert(timer_phase == BUK_CLIENT_TIMER_PAUSED);
    assert(BukClientParseTeam("A", &team));
    assert(team == BUK_CLIENT_TEAM_A);
    assert(BukClientParseTeam("", &team));
    assert(team == BUK_CLIENT_TEAM_NONE);
    assert(!BukClientParseTeam("C", &team));
    assert(BukClientParsePieceState("finished", &piece_state));
    assert(piece_state == BUK_CLIENT_PIECE_FINISHED);
    assert(BukClientParseResult("backdo", &result));
    assert(result == BUK_CLIENT_RESULT_BACKDO);
    assert(!BukClientParseResult("back_do", &result));
}

static void TestGrowsSnapshotStorageWithoutProtocolCaps(void)
{
    BukClientPresentationState state;
    const BukClientPresentationSnapshot *snapshot;
    size_t index;

    BukClientPresentationStateInit(&state);
    BukClientPresentationBeginSnapshot(&state);
    StageMetadata(&state, BUK_CLIENT_MATCH_ACTIVE, BUK_CLIENT_TURN_RESOLVE_QUEUE,
                  BUK_CLIENT_REQUIRED_SELECT_RESULT, BUK_CLIENT_TIMER_MOVE,
                  BUK_CLIENT_TEAM_A, 5000U);
    assert(BukClientPresentationStageMoveRequest(
        &state, BUK_CLIENT_REQUIRED_SELECT_RESULT, false, false,
        BUK_CLIENT_BOARD_NODE_COUNT));
    for (index = 0U; index < 40U; index++) {
        assert(BukClientPresentationStagePiece(
            &state, index % 2U == 0U ? BUK_CLIENT_TEAM_A : BUK_CLIENT_TEAM_B,
            BUK_CLIENT_PIECE_WAITING, BUK_CLIENT_BOARD_NODE_COUNT, false, 0U));
        assert(BukClientPresentationStageResult(&state, BUK_CLIENT_RESULT_YUT));
    }
    assert(BukClientPresentationCommitSnapshot(&state));
    snapshot = BukClientPresentationConfirmed(&state);
    assert(snapshot != NULL);
    assert(snapshot->piece_count == 40U);
    assert(snapshot->result_count == 40U);
    BukClientPresentationStateDestroy(&state);
}

static void TestCommitsOnlyConsistentAuthoritativeRouteRequest(void)
{
    BukClientPresentationState state;
    const BukClientPresentationSnapshot *snapshot;

    BukClientPresentationStateInit(&state);
    BukClientPresentationBeginSnapshot(&state);
    StageMetadata(&state, BUK_CLIENT_MATCH_ACTIVE,
                  BUK_CLIENT_TURN_WAIT_ROUTE_SELECTION,
                  BUK_CLIENT_REQUIRED_SELECT_ROUTE, BUK_CLIENT_TIMER_MOVE,
                  BUK_CLIENT_TEAM_A, 30000U);
    assert(BukClientPresentationStageMoveRequest(
        &state, BUK_CLIENT_REQUIRED_SELECT_ROUTE, true, true,
        BUK_CLIENT_BOARD_NODE_MO));
    assert(BukClientPresentationCommitSnapshot(&state));
    snapshot = BukClientPresentationConfirmed(&state);
    assert(snapshot != NULL);
    assert(snapshot->move_request_set);
    assert(snapshot->normal_route_available);
    assert(snapshot->shortcut_route_available);
    assert(snapshot->route_origin == BUK_CLIENT_BOARD_NODE_MO);

    BukClientPresentationBeginSnapshot(&state);
    StageMetadata(&state, BUK_CLIENT_MATCH_ACTIVE,
                  BUK_CLIENT_TURN_WAIT_ROUTE_SELECTION,
                  BUK_CLIENT_REQUIRED_SELECT_ROUTE, BUK_CLIENT_TIMER_MOVE,
                  BUK_CLIENT_TEAM_A, 29000U);
    assert(!BukClientPresentationStageMoveRequest(
        &state, BUK_CLIENT_REQUIRED_SELECT_ROUTE, true, false,
        BUK_CLIENT_BOARD_NODE_MO));
    assert(!BukClientPresentationCommitSnapshot(&state));
    snapshot = BukClientPresentationConfirmed(&state);
    assert(snapshot != NULL);
    assert(snapshot->remaining_ms == 30000U);
    BukClientPresentationStateDestroy(&state);
}

int main(void)
{
    TestCommitsAuthoritativeSnapshot();
    TestFailedSnapshotPreservesConfirmedState();
    TestRejectsInvalidPieceStateCombinations();
    TestCarriesAuthoritativeStackMembership();
    TestRequiresExactlyOneMetadataRecord();
    TestParsesOnlyCanonicalTokens();
    TestGrowsSnapshotStorageWithoutProtocolCaps();
    TestCommitsOnlyConsistentAuthoritativeRouteRequest();
    puts("presentation_state_test: ok");
    return 0;
}
