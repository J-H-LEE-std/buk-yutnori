#include "buk_client/presentation_state.h"
#include "buk_client/state.h"

#include <assert.h>
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>

static bool fail_next_reallocation;

void *__real_realloc(void *pointer, size_t size);
void *__wrap_realloc(void *pointer, size_t size);

void *__wrap_realloc(void *pointer, size_t size)
{
    if (fail_next_reallocation) {
        fail_next_reallocation = false;
        return NULL;
    }
    return __real_realloc(pointer, size);
}

static uint32_t Next(uint32_t *state)
{
    *state = *state * UINT32_C(1664525) + UINT32_C(1013904223);
    return *state;
}

static void ExercisePresentationSequences(void)
{
    BukClientPresentationState state;
    uint32_t random = UINT32_C(0x179180);
    size_t iteration;

    BukClientPresentationStateInit(&state);
    for (iteration = 0U; iteration < 20000U; iteration++) {
        uint32_t value = Next(&random);
        switch (value % 8U) {
        case 0U:
            BukClientPresentationBeginSnapshot(&state);
            break;
        case 1U:
            (void)BukClientPresentationStageMetadata(
                &state, (BukClientMatchStatus)(value % (BUK_CLIENT_MATCH_STATUS_COUNT + 2U)),
                (BukClientTurnPhase)((value >> 3U) % (BUK_CLIENT_TURN_PHASE_COUNT + 2U)),
                (BukClientRequiredInput)((value >> 7U) % (BUK_CLIENT_REQUIRED_INPUT_COUNT + 2U)),
                (BukClientTimerPhase)((value >> 11U) % (BUK_CLIENT_TIMER_PHASE_COUNT + 2U)),
                (BukClientTeam)((value >> 15U) % (BUK_CLIENT_TEAM_COUNT + 2U)), value);
            break;
        case 2U:
            (void)BukClientPresentationStagePiece(
                &state, (BukClientTeam)(value % (BUK_CLIENT_TEAM_COUNT + 2U)),
                (BukClientPieceState)((value >> 4U) % (BUK_CLIENT_PIECE_STATE_COUNT + 2U)),
                (BukClientBoardNodeId)((value >> 8U) % (BUK_CLIENT_BOARD_NODE_COUNT + 2U)),
                ((value >> 16U) & 1U) != 0U,
                (size_t)((value >> 17U) % (BUK_CLIENT_MAX_PRESENTATION_PIECES + 3U)));
            break;
        case 3U:
            (void)BukClientPresentationStageResult(
                &state, (BukClientResult)(value % (BUK_CLIENT_RESULT_COUNT + 2U)));
            break;
        case 4U:
            (void)BukClientPresentationStageMoveRequest(
                &state,
                (BukClientRequiredInput)(value % (BUK_CLIENT_REQUIRED_INPUT_COUNT + 2U)),
                ((value >> 8U) & 1U) != 0U, ((value >> 9U) & 1U) != 0U,
                (BukClientBoardNodeId)((value >> 10U) % (BUK_CLIENT_BOARD_NODE_COUNT + 2U)));
            break;
        case 5U:
            (void)BukClientPresentationCommitSnapshot(&state);
            break;
        case 6U:
            BukClientPresentationAbortSnapshot(&state);
            break;
        default:
            BukClientPresentationStateDestroy(&state);
            BukClientPresentationStateInit(&state);
            break;
        }
        const BukClientPresentationSnapshot *confirmed =
            BukClientPresentationConfirmed(&state);
        if (confirmed != NULL) {
            assert(confirmed->piece_count <= BUK_CLIENT_MAX_PRESENTATION_PIECES);
            assert(confirmed->result_count <= BUK_CLIENT_MAX_PRESENTATION_RESULTS);
        }
    }
    BukClientPresentationStateDestroy(&state);
}

static void ExerciseBoundedStrings(void)
{
    char input[BUK_CLIENT_INPUT_CAPACITY + 1U];
    BukClientState state;
    size_t index;

    for (index = 0U; index < sizeof(input); index++) input[index] = 'x';
    BukClientStateInit(&state);
    for (index = 0U; index <= BUK_CLIENT_INPUT_CAPACITY; index++) {
        if (index < sizeof(input)) input[index] = '\0';
        (void)BukClientStateSetInput(&state, input);
        if (index < sizeof(input)) input[index] = 'x';
    }
}

static void ExerciseAllocationFailure(void)
{
    BukClientPresentationState state;

    BukClientPresentationStateInit(&state);
    BukClientPresentationBeginSnapshot(&state);
    assert(BukClientPresentationStageMetadata(
        &state, BUK_CLIENT_MATCH_ACTIVE, BUK_CLIENT_TURN_WAIT_THROW,
        BUK_CLIENT_REQUIRED_THROW, BUK_CLIENT_TIMER_THROW,
        BUK_CLIENT_TEAM_A, 1000U));
    assert(BukClientPresentationCommitSnapshot(&state));

    BukClientPresentationBeginSnapshot(&state);
    assert(BukClientPresentationStageMetadata(
        &state, BUK_CLIENT_MATCH_ACTIVE, BUK_CLIENT_TURN_WAIT_THROW,
        BUK_CLIENT_REQUIRED_THROW, BUK_CLIENT_TIMER_THROW,
        BUK_CLIENT_TEAM_A, 1000U));
    fail_next_reallocation = true;
    assert(!BukClientPresentationStagePiece(
        &state, BUK_CLIENT_TEAM_A, BUK_CLIENT_PIECE_WAITING,
        BUK_CLIENT_BOARD_NODE_COUNT, false, 0U));
    assert(!BukClientPresentationCanCommit(&state));
    assert(BukClientPresentationConfirmed(&state) != NULL);
    BukClientPresentationStateDestroy(&state);
}

int main(void)
{
    ExercisePresentationSequences();
    ExerciseBoundedStrings();
    ExerciseAllocationFailure();
    puts("client bounded security fuzz tests passed");
    return 0;
}
