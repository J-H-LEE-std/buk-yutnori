#include "buk_client/presentation_state.h"
#include "buk_client/bridge.h"
#include "buk_client/protocol_state.h"
#include "buk_client/state.h"

#include <assert.h>
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

enum {
    PRESENTATION_ITERATIONS = 2048,
    PROTOCOL_ITERATIONS = 4096,
    BRIDGE_ITERATIONS = 1024,
};

static size_t reallocations_before_failure = SIZE_MAX;
static bool realloc_failure_was_injected;

void *BukClientTestRealloc(void *pointer, size_t size);

void *BukClientTestRealloc(void *pointer, size_t size)
{
    if (reallocations_before_failure != SIZE_MAX) {
        if (reallocations_before_failure == 0U) {
            reallocations_before_failure = SIZE_MAX;
            realloc_failure_was_injected = true;
            return NULL;
        }
        reallocations_before_failure--;
    }
    return realloc(pointer, size);
}

static uint32_t Next(uint32_t *state)
{
    uint32_t value;

    *state += UINT32_C(0x9e3779b9);
    value = *state;
    value = (value ^ (value >> 16U)) * UINT32_C(0x85ebca6b);
    value = (value ^ (value >> 13U)) * UINT32_C(0xc2b2ae35);
    return value ^ (value >> 16U);
}

static size_t NextRange(uint32_t *state, size_t upper_bound)
{
    assert(upper_bound > 0U);
    return (size_t)(Next(state) % (uint32_t)upper_bound);
}

static BukClientPresentationPiece NextPiece(uint32_t *random)
{
    BukClientPieceState piece_state =
        (BukClientPieceState)NextRange(random, BUK_CLIENT_PIECE_STATE_COUNT);
    BukClientBoardNodeId node;
    bool stacked = false;
    size_t stack_size = 0U;

    if (piece_state == BUK_CLIENT_PIECE_WAITING ||
        piece_state == BUK_CLIENT_PIECE_FINISHED) {
        node = BUK_CLIENT_BOARD_NODE_COUNT;
    } else if (piece_state == BUK_CLIENT_PIECE_HOME_CHECKPOINT) {
        node = BUK_CLIENT_BOARD_NODE_CHAMMEOGI;
    } else {
        node = (BukClientBoardNodeId)NextRange(
            random, BUK_CLIENT_BOARD_NODE_COUNT);
    }
    if ((piece_state == BUK_CLIENT_PIECE_ON_BOARD ||
         piece_state == BUK_CLIENT_PIECE_HOME_CHECKPOINT) &&
        NextRange(random, 4U) == 0U) {
        stacked = true;
        stack_size = 2U + NextRange(random, 6U);
    }
    return (BukClientPresentationPiece){
        (NextRange(random, 2U) == 0U) ? BUK_CLIENT_TEAM_A : BUK_CLIENT_TEAM_B,
        piece_state,
        node,
        stacked,
        stack_size,
    };
}

static uint64_t SnapshotHash(const BukClientPresentationSnapshot *snapshot)
{
    uint64_t hash = UINT64_C(14695981039346656037);
    size_t index;

    if (snapshot == NULL) return 0U;
#define HASH_FIELD(field) do { hash ^= (uint64_t)(field); \
    hash *= UINT64_C(1099511628211); } while (0)
    HASH_FIELD(snapshot->status);
    HASH_FIELD(snapshot->phase);
    HASH_FIELD(snapshot->required_input);
    HASH_FIELD(snapshot->timer_phase);
    HASH_FIELD(snapshot->current_team);
    HASH_FIELD(snapshot->remaining_ms);
    HASH_FIELD(snapshot->move_request_set);
    HASH_FIELD(snapshot->move_request_input);
    HASH_FIELD(snapshot->normal_route_available);
    HASH_FIELD(snapshot->shortcut_route_available);
    HASH_FIELD(snapshot->route_origin);
    HASH_FIELD(snapshot->piece_count);
    HASH_FIELD(snapshot->result_count);
    for (index = 0U; index < snapshot->piece_count; index++) {
        HASH_FIELD(snapshot->pieces[index].team);
        HASH_FIELD(snapshot->pieces[index].state);
        HASH_FIELD(snapshot->pieces[index].node);
        HASH_FIELD(snapshot->pieces[index].stacked);
        HASH_FIELD(snapshot->pieces[index].stack_size);
    }
    for (index = 0U; index < snapshot->result_count; index++) {
        HASH_FIELD(snapshot->results[index]);
    }
#undef HASH_FIELD
    return hash;
}

static void ExercisePresentationSequences(void)
{
    BukClientPresentationState state;
    uint32_t random = UINT32_C(0x179180);
    size_t successful_commits = 0U;
    size_t successful_aborts = 0U;
    size_t successful_move_requests = 0U;
    size_t peak_piece_count = 0U;
    size_t peak_result_count = 0U;
    size_t iteration;

    BukClientPresentationStateInit(&state);
    for (iteration = 0U; iteration < PRESENTATION_ITERATIONS; iteration++) {
        BukClientPresentationPiece expected_pieces[
            BUK_CLIENT_MAX_PRESENTATION_PIECES];
        BukClientResult expected_results[BUK_CLIENT_MAX_PRESENTATION_RESULTS];
        size_t piece_target = (iteration % 4U == 0U)
                                  ? 9U + NextRange(&random, 8U)
                                  : NextRange(&random, 9U);
        size_t result_target = (iteration % 4U == 1U)
                                   ? 9U + NextRange(&random, 8U)
                                   : NextRange(&random, 9U);
        size_t pieces_staged = 0U;
        size_t results_staged = 0U;
        uint64_t previous_snapshot_hash;
        BukClientRequiredInput required_input;
        const BukClientPresentationSnapshot *confirmed;

        if (iteration > 0U && iteration % 97U == 0U) {
            BukClientPresentationStateDestroy(&state);
            BukClientPresentationStateInit(&state);
        }
        previous_snapshot_hash =
            SnapshotHash(BukClientPresentationConfirmed(&state));
        BukClientPresentationBeginSnapshot(&state);

        /* Mix collection staging before and after metadata in each transaction. */
        size_t pieces_before_metadata =
            NextRange(&random, piece_target + 1U);
        size_t results_before_metadata =
            NextRange(&random, result_target + 1U);
        while (pieces_staged < pieces_before_metadata) {
            expected_pieces[pieces_staged] = NextPiece(&random);
            assert(BukClientPresentationStagePiece(
                &state, expected_pieces[pieces_staged].team,
                expected_pieces[pieces_staged].state,
                expected_pieces[pieces_staged].node,
                expected_pieces[pieces_staged].stacked,
                expected_pieces[pieces_staged].stack_size));
            pieces_staged++;
        }
        while (results_staged < results_before_metadata) {
            expected_results[results_staged] = (BukClientResult)NextRange(
                &random, BUK_CLIENT_RESULT_COUNT);
            assert(BukClientPresentationStageResult(
                &state, expected_results[results_staged]));
            results_staged++;
        }

        required_input = (BukClientRequiredInput)NextRange(
            &random, BUK_CLIENT_REQUIRED_INPUT_COUNT);
        assert(BukClientPresentationStageMetadata(
            &state,
            (BukClientMatchStatus)NextRange(
                &random, BUK_CLIENT_MATCH_STATUS_COUNT),
            (BukClientTurnPhase)NextRange(&random, BUK_CLIENT_TURN_PHASE_COUNT),
            required_input,
            (BukClientTimerPhase)NextRange(&random, BUK_CLIENT_TIMER_PHASE_COUNT),
            (BukClientTeam)NextRange(&random, BUK_CLIENT_TEAM_COUNT),
            (uint64_t)Next(&random)));
        if (required_input == BUK_CLIENT_REQUIRED_SELECT_MOVE) {
            assert(BukClientPresentationStageMoveRequest(
                &state, required_input, false, false,
                BUK_CLIENT_BOARD_NODE_COUNT));
            successful_move_requests++;
        } else if (required_input == BUK_CLIENT_REQUIRED_SELECT_ROUTE) {
            assert(BukClientPresentationStageMoveRequest(
                &state, required_input, true, true, BUK_CLIENT_BOARD_NODE_MO));
            successful_move_requests++;
        }

        while ((pieces_staged < piece_target) ||
               (results_staged < result_target)) {
            bool stage_piece = pieces_staged < piece_target &&
                               (results_staged == result_target ||
                                NextRange(&random, 2U) == 0U);
            if (stage_piece) {
                expected_pieces[pieces_staged] = NextPiece(&random);
                assert(BukClientPresentationStagePiece(
                    &state, expected_pieces[pieces_staged].team,
                    expected_pieces[pieces_staged].state,
                    expected_pieces[pieces_staged].node,
                    expected_pieces[pieces_staged].stacked,
                    expected_pieces[pieces_staged].stack_size));
                pieces_staged++;
            } else {
                expected_results[results_staged] = (BukClientResult)NextRange(
                    &random, BUK_CLIENT_RESULT_COUNT);
                assert(BukClientPresentationStageResult(
                    &state, expected_results[results_staged]));
                results_staged++;
            }
        }

        if (iteration % 13U == 0U) {
            /* Duplicate metadata is malformed and must poison only this staging. */
            assert(!BukClientPresentationStageMetadata(
                &state, BUK_CLIENT_MATCH_ACTIVE, BUK_CLIENT_TURN_WAIT_THROW,
                BUK_CLIENT_REQUIRED_THROW, BUK_CLIENT_TIMER_THROW,
                BUK_CLIENT_TEAM_A, 0U));
            assert(!BukClientPresentationCanCommit(&state));
            BukClientPresentationAbortSnapshot(&state);
            successful_aborts++;
            assert(SnapshotHash(BukClientPresentationConfirmed(&state)) ==
                   previous_snapshot_hash);
            continue;
        }
        if (iteration % 7U == 0U) {
            BukClientPresentationAbortSnapshot(&state);
            successful_aborts++;
            assert(SnapshotHash(BukClientPresentationConfirmed(&state)) ==
                   previous_snapshot_hash);
            continue;
        }

        assert(BukClientPresentationCanCommit(&state));
        assert(BukClientPresentationCommitSnapshot(&state));
        successful_commits++;
        confirmed = BukClientPresentationConfirmed(&state);
        assert(confirmed != NULL);
        assert(confirmed->piece_count == pieces_staged);
        assert(confirmed->result_count == results_staged);
        assert(confirmed->piece_count <= BUK_CLIENT_MAX_PRESENTATION_PIECES);
        assert(confirmed->result_count <= BUK_CLIENT_MAX_PRESENTATION_RESULTS);
        if (pieces_staged > peak_piece_count) peak_piece_count = pieces_staged;
        if (results_staged > peak_result_count) peak_result_count = results_staged;
        for (size_t index = 0U; index < pieces_staged; index++) {
            assert(confirmed->pieces[index].team == expected_pieces[index].team);
            assert(confirmed->pieces[index].state == expected_pieces[index].state);
            assert(confirmed->pieces[index].node == expected_pieces[index].node);
            assert(confirmed->pieces[index].stacked == expected_pieces[index].stacked);
            assert(confirmed->pieces[index].stack_size ==
                   expected_pieces[index].stack_size);
        }
        for (size_t index = 0U; index < results_staged; index++) {
            assert(confirmed->results[index] == expected_results[index]);
        }
    }

    assert(successful_commits > 0U);
    assert(successful_aborts > 0U);
    assert(successful_move_requests > 0U);
    assert(peak_piece_count > 8U);
    assert(peak_result_count > 8U);
    BukClientPresentationStateDestroy(&state);
}

static void ExerciseBoundedStrings(void)
{
    char maximum_input[BUK_CLIENT_INPUT_CAPACITY];
    char unterminated_input[BUK_CLIENT_INPUT_CAPACITY];
    BukClientState state;
    size_t index;

    BukClientStateInit(&state);
    maximum_input[0] = 'x';
    for (index = 1U; index < BUK_CLIENT_INPUT_CAPACITY - 1U; index++) {
        maximum_input[index] = 'x';
    }
    maximum_input[BUK_CLIENT_INPUT_CAPACITY - 1U] = '\0';
    assert(BukClientStateSetInput(&state, maximum_input));
    assert(BukClientStateInputLength(&state) == BUK_CLIENT_INPUT_CAPACITY - 1U);

    for (index = 0U; index < sizeof(unterminated_input); index++) {
        unterminated_input[index] = 'x';
    }
    assert(!BukClientStateSetInput(&state, unterminated_input));
    assert(BukClientStateInputLength(&state) == BUK_CLIENT_INPUT_CAPACITY - 1U);
    assert(BukClientStateSetInput(&state, ""));
    assert(BukClientStateInputLength(&state) == 0U);
}

static void CommitSeedSnapshot(BukClientPresentationState *state)
{
    BukClientPresentationBeginSnapshot(state);
    assert(BukClientPresentationStageMetadata(
        state, BUK_CLIENT_MATCH_STARTING, BUK_CLIENT_TURN_WAIT_THROW,
        BUK_CLIENT_REQUIRED_THROW, BUK_CLIENT_TIMER_THROW,
        BUK_CLIENT_TEAM_A, 10U));
    assert(BukClientPresentationStagePiece(
        state, BUK_CLIENT_TEAM_A, BUK_CLIENT_PIECE_ON_BOARD,
        BUK_CLIENT_BOARD_NODE_DO, false, 0U));
    assert(BukClientPresentationStageResult(state, BUK_CLIENT_RESULT_GAE));
    assert(BukClientPresentationCommitSnapshot(state));
}

static void CommitRecoveredSnapshot(BukClientPresentationState *state)
{
    BukClientPresentationBeginSnapshot(state);
    assert(BukClientPresentationStageMetadata(
        state, BUK_CLIENT_MATCH_PAUSED, BUK_CLIENT_TURN_PAUSED,
        BUK_CLIENT_REQUIRED_NONE, BUK_CLIENT_TIMER_PAUSED,
        BUK_CLIENT_TEAM_B, 99U));
    assert(BukClientPresentationStagePiece(
        state, BUK_CLIENT_TEAM_B, BUK_CLIENT_PIECE_FINISHED,
        BUK_CLIENT_BOARD_NODE_COUNT, false, 0U));
    assert(BukClientPresentationStageResult(state, BUK_CLIENT_RESULT_DO));
    assert(BukClientPresentationCommitSnapshot(state));
}

static void ExercisePieceAllocationFailureRecovery(void)
{
    BukClientPresentationState state;
    uint64_t confirmed_hash;
    size_t index;

    BukClientPresentationStateInit(&state);
    CommitSeedSnapshot(&state);
    confirmed_hash = SnapshotHash(BukClientPresentationConfirmed(&state));
    BukClientPresentationBeginSnapshot(&state);
    assert(BukClientPresentationStageMetadata(
        &state, BUK_CLIENT_MATCH_ACTIVE, BUK_CLIENT_TURN_WAIT_THROW,
        BUK_CLIENT_REQUIRED_THROW, BUK_CLIENT_TIMER_THROW,
        BUK_CLIENT_TEAM_A, 20U));
    for (index = 0U; index < 8U; index++) {
        assert(BukClientPresentationStagePiece(
            &state, BUK_CLIENT_TEAM_A, BUK_CLIENT_PIECE_ON_BOARD,
            BUK_CLIENT_BOARD_NODE_DO, false, 0U));
    }

    realloc_failure_was_injected = false;
    reallocations_before_failure = 0U;
    assert(!BukClientPresentationStagePiece(
        &state, BUK_CLIENT_TEAM_B, BUK_CLIENT_PIECE_ON_BOARD,
        BUK_CLIENT_BOARD_NODE_GAE, false, 0U));
    assert(realloc_failure_was_injected);
    assert(reallocations_before_failure == SIZE_MAX);
    assert(!BukClientPresentationCanCommit(&state));
    assert(SnapshotHash(BukClientPresentationConfirmed(&state)) == confirmed_hash);

    BukClientPresentationAbortSnapshot(&state);
    assert(SnapshotHash(BukClientPresentationConfirmed(&state)) == confirmed_hash);
    CommitRecoveredSnapshot(&state);
    assert(BukClientPresentationConfirmed(&state)->status == BUK_CLIENT_MATCH_PAUSED);
    assert(BukClientPresentationConfirmed(&state)->piece_count == 1U);
    BukClientPresentationStateDestroy(&state);
}

static void ExerciseResultAllocationFailureRecovery(void)
{
    BukClientPresentationState state;
    uint64_t confirmed_hash;
    size_t index;

    BukClientPresentationStateInit(&state);
    CommitSeedSnapshot(&state);
    confirmed_hash = SnapshotHash(BukClientPresentationConfirmed(&state));
    BukClientPresentationBeginSnapshot(&state);
    assert(BukClientPresentationStageMetadata(
        &state, BUK_CLIENT_MATCH_ACTIVE, BUK_CLIENT_TURN_WAIT_THROW,
        BUK_CLIENT_REQUIRED_THROW, BUK_CLIENT_TIMER_THROW,
        BUK_CLIENT_TEAM_A, 20U));
    for (index = 0U; index < 8U; index++) {
        assert(BukClientPresentationStageResult(
            &state, (BukClientResult)(index % BUK_CLIENT_RESULT_COUNT)));
    }

    realloc_failure_was_injected = false;
    reallocations_before_failure = 0U;
    assert(!BukClientPresentationStageResult(&state, BUK_CLIENT_RESULT_DO));
    assert(realloc_failure_was_injected);
    assert(reallocations_before_failure == SIZE_MAX);
    assert(!BukClientPresentationCanCommit(&state));
    assert(SnapshotHash(BukClientPresentationConfirmed(&state)) == confirmed_hash);

    BukClientPresentationAbortSnapshot(&state);
    assert(SnapshotHash(BukClientPresentationConfirmed(&state)) == confirmed_hash);
    CommitRecoveredSnapshot(&state);
    assert(BukClientPresentationConfirmed(&state)->status == BUK_CLIENT_MATCH_PAUSED);
    assert(BukClientPresentationConfirmed(&state)->result_count == 1U);
    BukClientPresentationStateDestroy(&state);
}

static uint64_t NextSequence(uint32_t *random)
{
    uint64_t sequence = ((uint64_t)Next(random) << 32U) | Next(random);
    return sequence == 0U ? 1U : sequence;
}

static void ExerciseProtocolStateSequences(void)
{
    uint32_t random = UINT32_C(0x51a7e);
    size_t iteration;

    for (iteration = 0U; iteration < PROTOCOL_ITERATIONS; iteration++) {
        BukClientProtocolState state;
        uint64_t sequence = (iteration % 31U == 0U)
                                ? UINT64_MAX
                                : NextSequence(&random);
        size_t event_count = NextRange(&random, 5U);
        size_t event_index;
        bool complete = NextRange(&random, 5U) != 0U;

        BukClientProtocolStateInit(&state);
        if (NextRange(&random, 2U) == 0U) {
            BukClientProtocolBeginSynchronization(&state);
        }
        assert(BukClientProtocolApplySnapshot(&state, sequence));
        for (event_index = 0U;
             event_index < event_count && sequence != UINT64_MAX;
             event_index++) {
            sequence++;
            assert(BukClientProtocolApplyEvent(&state, sequence));
        }
        if (!complete) {
            assert(!BukClientProtocolCanSendCommands(&state));
            continue;
        }
        assert(BukClientProtocolCompleteSynchronization(&state));
        assert(BukClientProtocolCanSendCommands(&state));
        assert(BukClientProtocolLastSequence(&state) == sequence);

        if (NextRange(&random, 4U) == 0U) {
            uint64_t invalid_sequence =
                (sequence == UINT64_MAX) ? 1U : sequence + 2U;
            assert(!BukClientProtocolApplyEvent(&state, invalid_sequence));
            assert(BukClientProtocolRequiresSynchronization(&state));
            assert(!BukClientProtocolCanSendCommands(&state));
        }
    }

    {
        BukClientProtocolState state;
        BukClientProtocolStateInit(&state);
        assert(!BukClientProtocolApplySnapshot(&state, 0U));
        assert(BukClientProtocolRequiresSynchronization(&state));
        BukClientProtocolBeginSynchronization(&state);
        assert(BukClientProtocolApplySnapshot(&state, UINT64_MAX));
        assert(!BukClientProtocolApplyEvent(&state, 0U));
        assert(BukClientProtocolRequiresSynchronization(&state));
    }
}

static void ExerciseProtocolBridgeSequences(void)
{
    static const char *const teams[] = { "", "A", "B" };
    static const char *const invalid_sequences[] = {
        "", "+1", "-1", "1x", "18446744073709551616",
    };
    uint32_t random = UINT32_C(0x180180);
    size_t iteration;

    BukClientProtocolRuntimeInit();
    for (iteration = 0U; iteration < BRIDGE_ITERATIONS; iteration++) {
        char sequence_text[32];
        char next_sequence_text[32];
        uint64_t sequence = NextSequence(&random) & UINT64_C(0x0000ffffffffffff);
        size_t invalid_case = iteration % 11U;

        if (sequence == 0U) sequence = 1U;
        BukClientProtocolRuntimeInit();
        if (iteration % 5U == 0U) BukClientProtocolRuntimeInit();
        assert(BukClientBeginSynchronization());
        if (invalid_case == 0U) {
            const char *bad = invalid_sequences[
                NextRange(&random, sizeof(invalid_sequences) /
                                      sizeof(invalid_sequences[0]))];
            assert(!BukClientApplySnapshotSequence(bad));
            assert(BukClientRequiresResynchronization());
            assert(!BukClientCanSendStateCommands());
            continue;
        }

        assert(snprintf(sequence_text, sizeof(sequence_text), "%llu",
                        (unsigned long long)sequence) > 0);
        assert(BukClientApplySnapshotSequence(sequence_text));
        if (invalid_case == 1U) {
            assert(!BukClientStageSnapshotMetadata(
                "unknown", "wait_throw", "throw", "throw", "A", "12345"));
            assert(BukClientRequiresResynchronization());
            continue;
        }
        assert(BukClientStageSnapshotMetadata(
            "active", "wait_throw", "throw", "throw",
            teams[NextRange(&random, sizeof(teams) / sizeof(teams[0]))],
            "12345"));
        if (invalid_case == 2U) {
            assert(!BukClientStageSnapshotPiece("A", "unknown", "do", 0, 0));
            assert(BukClientRequiresResynchronization());
            continue;
        }
        assert(BukClientStageSnapshotPiece("A", "on_board", "do", 0, 0));
        assert(BukClientStageSnapshotPiece("B", "waiting", "", 0, 0));
        if (invalid_case == 3U) {
            assert(!BukClientStageSnapshotResult("unknown"));
            assert(BukClientRequiresResynchronization());
            continue;
        }
        assert(BukClientStageSnapshotResult((NextRange(&random, 2U) == 0U)
                                                ? "do" : "gae"));

        if (iteration % 3U == 0U) {
            assert(snprintf(next_sequence_text, sizeof(next_sequence_text),
                            "%llu", (unsigned long long)(sequence + 1U)) > 0);
            assert(BukClientApplyReducedEventSequence(next_sequence_text));
            sequence++;
        }
        assert(BukClientCompleteSynchronization());
        assert(BukClientCanSendStateCommands());
        assert(!BukClientRequiresResynchronization());
        assert(BukClientLastSequence() != NULL);
        if (iteration % 11U == 0U) {
            assert(!BukClientApplyEventSequence("99999999999999999999"));
            assert(BukClientRequiresResynchronization());
        }
    }
    BukClientProtocolRuntimeInit();
}

int main(void)
{
    ExercisePresentationSequences();
    ExerciseBoundedStrings();
    ExercisePieceAllocationFailureRecovery();
    ExerciseResultAllocationFailureRecovery();
    ExerciseProtocolStateSequences();
    ExerciseProtocolBridgeSequences();
    puts("client bounded security fuzz tests passed");
    return 0;
}
