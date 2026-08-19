#include "buk_client/protocol_state.h"

#include <stdio.h>

static int failures;

#define CHECK(condition) Check((condition), #condition, __FILE__, __LINE__)

static void Check(bool condition, const char *expression, const char *file, int line)
{
    if (condition) return;

    fprintf(stderr, "%s:%d: check failed: %s\n", file, line, expression);
    failures++;
}

static void TestSynchronizationRequiresSnapshotAndContiguousEvents(void)
{
    BukClientProtocolState state;

    BukClientProtocolStateInit(&state);

    CHECK(!BukClientProtocolCanSendCommands(&state));
    CHECK(BukClientProtocolApplySnapshot(&state, 41U));
    CHECK(BukClientProtocolApplyEvent(&state, 42U));
    CHECK(BukClientProtocolApplyEvent(&state, 43U));
    CHECK(BukClientProtocolCompleteSynchronization(&state));
    CHECK(BukClientProtocolCanSendCommands(&state));
    CHECK(BukClientProtocolLastSequence(&state) == 43U);
}

static void TestGapAndDuplicateRequireFreshSynchronization(void)
{
    BukClientProtocolState state;

    BukClientProtocolStateInit(&state);
    CHECK(BukClientProtocolApplySnapshot(&state, 10U));
    CHECK(!BukClientProtocolApplyEvent(&state, 12U));
    CHECK(BukClientProtocolRequiresSynchronization(&state));
    CHECK(BukClientProtocolLastSequence(&state) == 0U);

    BukClientProtocolBeginSynchronization(&state);
    CHECK(BukClientProtocolApplySnapshot(&state, 12U));
    CHECK(!BukClientProtocolApplyEvent(&state, 12U));
    CHECK(BukClientProtocolRequiresSynchronization(&state));
    CHECK(BukClientProtocolLastSequence(&state) == 0U);
}

static void TestOlderSnapshotCannotRollbackConfirmedState(void)
{
    BukClientProtocolState state;

    BukClientProtocolStateInit(&state);
    CHECK(BukClientProtocolApplySnapshot(&state, 20U));
    CHECK(BukClientProtocolCompleteSynchronization(&state));
    CHECK(BukClientProtocolApplyEvent(&state, 21U));

    BukClientProtocolBeginSynchronization(&state);
    CHECK(!BukClientProtocolApplySnapshot(&state, 19U));
    CHECK(BukClientProtocolRequiresSynchronization(&state));
    CHECK(BukClientProtocolLastSequence(&state) == 21U);
}

static void TestFailedStagingPreservesPreviouslyConfirmedSequence(void)
{
    BukClientProtocolState state;

    BukClientProtocolStateInit(&state);
    CHECK(BukClientProtocolApplySnapshot(&state, 10U));
    CHECK(BukClientProtocolCompleteSynchronization(&state));
    CHECK(BukClientProtocolApplyEvent(&state, 11U));

    BukClientProtocolBeginSynchronization(&state);
    CHECK(BukClientProtocolApplySnapshot(&state, 20U));
    CHECK(!BukClientProtocolApplyEvent(&state, 22U));
    CHECK(BukClientProtocolRequiresSynchronization(&state));
    CHECK(BukClientProtocolLastSequence(&state) == 11U);
}

static void TestDuplicateSnapshotRequiresFreshSynchronization(void)
{
    BukClientProtocolState state;

    BukClientProtocolStateInit(&state);
    CHECK(BukClientProtocolApplySnapshot(&state, 10U));
    CHECK(!BukClientProtocolApplySnapshot(&state, 11U));
    CHECK(BukClientProtocolRequiresSynchronization(&state));
    CHECK(BukClientProtocolLastSequence(&state) == 0U);
}

static void TestRejectsZeroSnapshotSequence(void)
{
    BukClientProtocolState state;

    BukClientProtocolStateInit(&state);
    CHECK(!BukClientProtocolApplySnapshot(&state, 0U));
    CHECK(BukClientProtocolRequiresSynchronization(&state));
    CHECK(BukClientProtocolLastSequence(&state) == 0U);
}

static void TestRejectsEventAfterMaximumSequence(void)
{
    BukClientProtocolState state;

    BukClientProtocolStateInit(&state);
    CHECK(BukClientProtocolApplySnapshot(&state, UINT64_MAX));
    CHECK(BukClientProtocolCompleteSynchronization(&state));
    CHECK(!BukClientProtocolApplyEvent(&state, UINT64_MAX));
    CHECK(BukClientProtocolRequiresSynchronization(&state));
    CHECK(BukClientProtocolLastSequence(&state) == UINT64_MAX);
}

static void TestRejectsLiveGapDuplicateAndReversal(void)
{
    BukClientProtocolState state;

    BukClientProtocolStateInit(&state);
    CHECK(BukClientProtocolApplySnapshot(&state, 10U));
    CHECK(BukClientProtocolCompleteSynchronization(&state));
    CHECK(BukClientProtocolApplyEvent(&state, 11U));
    CHECK(!BukClientProtocolApplyEvent(&state, 13U));
    CHECK(BukClientProtocolLastSequence(&state) == 11U);

    BukClientProtocolBeginSynchronization(&state);
    CHECK(BukClientProtocolApplySnapshot(&state, 11U));
    CHECK(BukClientProtocolCompleteSynchronization(&state));
    CHECK(!BukClientProtocolApplyEvent(&state, 11U));
    CHECK(BukClientProtocolLastSequence(&state) == 11U);

    BukClientProtocolBeginSynchronization(&state);
    CHECK(BukClientProtocolApplySnapshot(&state, 11U));
    CHECK(BukClientProtocolCompleteSynchronization(&state));
    CHECK(!BukClientProtocolApplyEvent(&state, 10U));
    CHECK(BukClientProtocolLastSequence(&state) == 11U);
}

static void TestNullArgumentsAreSafe(void)
{
    BukClientProtocolState state;

    BukClientProtocolStateInit(NULL);
    BukClientProtocolBeginSynchronization(NULL);
    CHECK(!BukClientProtocolApplySnapshot(NULL, 1U));
    CHECK(!BukClientProtocolApplyEvent(NULL, 1U));
    CHECK(!BukClientProtocolCompleteSynchronization(NULL));
    CHECK(!BukClientProtocolCanSendCommands(NULL));
    CHECK(!BukClientProtocolRequiresSynchronization(NULL));
    CHECK(BukClientProtocolLastSequence(NULL) == 0U);

    BukClientProtocolStateInit(&state);
    CHECK(!BukClientProtocolCanSendCommands(&state));
}

int main(void)
{
    TestSynchronizationRequiresSnapshotAndContiguousEvents();
    TestGapAndDuplicateRequireFreshSynchronization();
    TestOlderSnapshotCannotRollbackConfirmedState();
    TestFailedStagingPreservesPreviouslyConfirmedSequence();
    TestDuplicateSnapshotRequiresFreshSynchronization();
    TestRejectsZeroSnapshotSequence();
    TestRejectsEventAfterMaximumSequence();
    TestRejectsLiveGapDuplicateAndReversal();
    TestNullArgumentsAreSafe();

    if (failures != 0) {
        fprintf(stderr, "%d protocol state test(s) failed\n", failures);
        return 1;
    }

    puts("client protocol state tests passed");
    return 0;
}
