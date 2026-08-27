#include "buk_client/bridge.h"

#include <stdio.h>
#include <string.h>

static int failures;

#define CHECK(condition) Check((condition), #condition, __FILE__, __LINE__)

static void Check(int condition, const char *expression, const char *file, int line)
{
    if (condition) return;

    fprintf(stderr, "%s:%d: check failed: %s\n", file, line, expression);
    failures++;
}

static void StageMinimalPresentation(const char *status, const char *team)
{
    CHECK(BukClientStageSnapshotMetadata(status, "wait_throw", "throw", "throw",
                                         team, "20000"));
    CHECK(BukClientStageSnapshotPiece("A", "on_board", "do", 0, 0));
    CHECK(BukClientStageSnapshotPiece("B", "waiting", "", 0, 0));
    CHECK(BukClientStageSnapshotResult("gae"));
}

static void TestAtomicallyCommitsSequenceAndPresentation(void)
{
    const BukClientPresentationSnapshot *snapshot;

    BukClientProtocolRuntimeInit();

    CHECK(!BukClientCanSendStateCommands());
    CHECK(strcmp(BukClientLastSequence(), "0") == 0);
    CHECK(BukClientBeginSynchronization());
    CHECK(BukClientApplySnapshotSequence("41"));
    StageMinimalPresentation("active", "A");
    CHECK(BukClientCompleteSynchronization());
    CHECK(BukClientCanSendStateCommands());
    CHECK(!BukClientRequiresResynchronization());
    CHECK(strcmp(BukClientLastSequence(), "41") == 0);
    CHECK(BukClientHasPresentationSnapshot());
    CHECK(BukClientPresentationPieceCount() == 2);
    CHECK(BukClientPresentationResultCount() == 1);
    CHECK(strcmp(BukClientPresentationStatus(), "active") == 0);
    CHECK(strcmp(BukClientPresentationTurnPhase(), "wait_throw") == 0);
    CHECK(strcmp(BukClientPresentationRequiredInput(), "throw") == 0);
    CHECK(strcmp(BukClientPresentationCurrentTeam(), "A") == 0);
    CHECK(strcmp(BukClientPresentationRemainingMilliseconds(), "20000") == 0);
    snapshot = BukClientConfirmedPresentation();
    CHECK(snapshot != NULL);
    CHECK(snapshot->pieces[0].node == BUK_CLIENT_BOARD_NODE_DO);
}

static void TestRejectsInvalidDecimalSequences(void)
{
    const char *invalid[] = {
        NULL,
        "",
        "+1",
        "-1",
        " 1",
        "1 ",
        "1.0",
        "18446744073709551616",
    };

    for (size_t index = 0U; index < sizeof(invalid) / sizeof(invalid[0]); index++) {
        BukClientProtocolRuntimeInit();
        CHECK(BukClientBeginSynchronization());
        CHECK(!BukClientApplySnapshotSequence(invalid[index]));
        CHECK(BukClientRequiresResynchronization());
        CHECK(strcmp(BukClientLastSequence(), "0") == 0);
    }
}

static void TestFailedBundlePreservesConfirmedSequence(void)
{
    BukClientProtocolRuntimeInit();
    CHECK(BukClientBeginSynchronization());
    CHECK(BukClientApplySnapshotSequence("10"));
    StageMinimalPresentation("starting", "");
    CHECK(BukClientCompleteSynchronization());

    CHECK(BukClientBeginSynchronization());
    CHECK(BukClientApplySnapshotSequence("20"));
    StageMinimalPresentation("active", "B");
    CHECK(!BukClientApplyEventSequence("22"));
    CHECK(BukClientRequiresResynchronization());
    CHECK(!BukClientCanSendStateCommands());
    CHECK(strcmp(BukClientLastSequence(), "10") == 0);
    CHECK(strcmp(BukClientPresentationStatus(), "starting") == 0);
}

static void TestInvalidSnapshotLocksGateWithoutExplicitBegin(void)
{
    BukClientProtocolRuntimeInit();
    CHECK(BukClientBeginSynchronization());
    CHECK(BukClientApplySnapshotSequence("10"));
    StageMinimalPresentation("active", "A");
    CHECK(BukClientCompleteSynchronization());
    CHECK(BukClientCanSendStateCommands());

    CHECK(!BukClientApplySnapshotSequence("invalid"));
    CHECK(BukClientRequiresResynchronization());
    CHECK(!BukClientCanSendStateCommands());
    CHECK(strcmp(BukClientLastSequence(), "10") == 0);
}

static void TestPresentationFailureLocksGateAndPreservesBothStates(void)
{
    BukClientProtocolRuntimeInit();
    CHECK(BukClientBeginSynchronization());
    CHECK(BukClientApplySnapshotSequence("10"));
    StageMinimalPresentation("starting", "");
    CHECK(BukClientCompleteSynchronization());

    CHECK(BukClientBeginSynchronization());
    CHECK(BukClientApplySnapshotSequence("11"));
    CHECK(BukClientStageSnapshotMetadata(
        "active", "wait_throw", "throw", "throw", "B", "19000"));
    CHECK(!BukClientStageSnapshotPiece("B", "home_checkpoint", "do", 0, 0));
    CHECK(!BukClientCompleteSynchronization());
    CHECK(BukClientRequiresResynchronization());
    CHECK(!BukClientCanSendStateCommands());
    CHECK(strcmp(BukClientLastSequence(), "10") == 0);
    CHECK(strcmp(BukClientPresentationStatus(), "starting") == 0);
}

static void TestRejectsValidatedEventTailWithoutPayloadReducer(void)
{
    BukClientProtocolRuntimeInit();
    CHECK(BukClientBeginSynchronization());
    CHECK(BukClientApplySnapshotSequence("10"));
    StageMinimalPresentation("active", "A");
    CHECK(BukClientApplyEventSequence("11"));
    CHECK(!BukClientCompleteSynchronization());
    CHECK(BukClientRequiresResynchronization());
    CHECK(!BukClientHasPresentationSnapshot());
    CHECK(strcmp(BukClientLastSequence(), "0") == 0);
}

static void TestCommitsReducedEventTailSequence(void)
{
    BukClientProtocolRuntimeInit();
    CHECK(BukClientBeginSynchronization());
    CHECK(BukClientApplySnapshotSequence("10"));
    StageMinimalPresentation("active", "A");
    CHECK(BukClientApplyReducedEventSequence("11"));
    CHECK(BukClientCompleteSynchronization());
    CHECK(BukClientCanSendStateCommands());
    CHECK(strcmp(BukClientLastSequence(), "11") == 0);
}

static void TestRejectsLiveEventSequenceWithoutPresentationReducer(void)
{
    BukClientProtocolRuntimeInit();
    CHECK(BukClientBeginSynchronization());
    CHECK(BukClientApplySnapshotSequence("10"));
    StageMinimalPresentation("active", "A");
    CHECK(BukClientCompleteSynchronization());

    CHECK(!BukClientApplyEventSequence("11"));
    CHECK(BukClientRequiresResynchronization());
    CHECK(!BukClientCanSendStateCommands());
    CHECK(strcmp(BukClientLastSequence(), "10") == 0);
    CHECK(strcmp(BukClientPresentationStatus(), "active") == 0);
}

static void TestEventCueAcceptsOnlyCanonicalKinds(void)
{
    BukClientProtocolRuntimeInit();
    CHECK(strcmp(BukClientEventCueName(), "none") == 0);
    CHECK(BukClientSetEventCue("backdo"));
    CHECK(strcmp(BukClientEventCueName(), "backdo") == 0);
    CHECK(BukClientSetEventCue("buk"));
    CHECK(strcmp(BukClientEventCueName(), "buk") == 0);
    CHECK(!BukClientSetEventCue("yut"));
    CHECK(strcmp(BukClientEventCueName(), "buk") == 0);
    CHECK(BukClientClearEventCue());
    CHECK(strcmp(BukClientEventCueName(), "none") == 0);
}

static void TestRouteIntentRequiresConfirmedInteractiveRequest(void)
{
    BukClientProtocolRuntimeInit();
    CHECK(BukClientBeginSynchronization());
    CHECK(BukClientApplySnapshotSequence("30"));
    CHECK(BukClientStageSnapshotMetadata(
        "active", "wait_route_selection", "select_route", "move", "A", "30000"));
    CHECK(BukClientStageSnapshotMoveRequest("select_route", 1, 1, "mo"));
    CHECK(BukClientCompleteSynchronization());

    CHECK(!BukClientCanSelectRoute());
    CHECK(BukClientSetRouteInteractionEnabled(1));
    CHECK(BukClientCanSelectRoute());
    CHECK(BukClientRequestRouteSelection("shortcut"));
    CHECK(!BukClientCanSelectRoute());
    CHECK(strcmp(BukClientConsumeRouteSelection(), "shortcut") == 0);
    CHECK(strcmp(BukClientConsumeRouteSelection(), "") == 0);
    CHECK(!BukClientRequestRouteSelection("normal"));
    CHECK(BukClientResolveRouteCommand());
    CHECK(BukClientCanSelectRoute());
    CHECK(!BukClientRequestRouteSelection("invalid"));

    CHECK(BukClientBeginSynchronization());
    CHECK(!BukClientCanSelectRoute());
}

int main(void)
{
    TestAtomicallyCommitsSequenceAndPresentation();
    TestRejectsInvalidDecimalSequences();
    TestFailedBundlePreservesConfirmedSequence();
    TestInvalidSnapshotLocksGateWithoutExplicitBegin();
    TestPresentationFailureLocksGateAndPreservesBothStates();
    TestRejectsValidatedEventTailWithoutPayloadReducer();
    TestCommitsReducedEventTailSequence();
    TestRejectsLiveEventSequenceWithoutPresentationReducer();
    TestEventCueAcceptsOnlyCanonicalKinds();
    TestRouteIntentRequiresConfirmedInteractiveRequest();

    if (failures != 0) {
        fprintf(stderr, "%d protocol bridge test(s) failed\n", failures);
        return 1;
    }

    puts("client protocol bridge tests passed");
    return 0;
}
