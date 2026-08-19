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

static void TestStagesSequencesThroughDecimalStringBoundary(void)
{
    BukClientProtocolRuntimeInit();

    CHECK(!BukClientCanSendStateCommands());
    CHECK(strcmp(BukClientLastSequence(), "0") == 0);
    CHECK(BukClientBeginSynchronization());
    CHECK(BukClientApplySnapshotSequence("41"));
    CHECK(BukClientApplyEventSequence("42"));
    CHECK(BukClientCompleteSynchronization());
    CHECK(BukClientCanSendStateCommands());
    CHECK(!BukClientRequiresResynchronization());
    CHECK(strcmp(BukClientLastSequence(), "42") == 0);
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
    CHECK(BukClientCompleteSynchronization());
    CHECK(BukClientApplyEventSequence("11"));

    CHECK(BukClientBeginSynchronization());
    CHECK(BukClientApplySnapshotSequence("20"));
    CHECK(!BukClientApplyEventSequence("22"));
    CHECK(BukClientRequiresResynchronization());
    CHECK(!BukClientCanSendStateCommands());
    CHECK(strcmp(BukClientLastSequence(), "11") == 0);
}

static void TestInvalidSnapshotLocksGateWithoutExplicitBegin(void)
{
    BukClientProtocolRuntimeInit();
    CHECK(BukClientBeginSynchronization());
    CHECK(BukClientApplySnapshotSequence("10"));
    CHECK(BukClientCompleteSynchronization());
    CHECK(BukClientCanSendStateCommands());

    CHECK(!BukClientApplySnapshotSequence("invalid"));
    CHECK(BukClientRequiresResynchronization());
    CHECK(!BukClientCanSendStateCommands());
    CHECK(strcmp(BukClientLastSequence(), "10") == 0);
}

int main(void)
{
    TestStagesSequencesThroughDecimalStringBoundary();
    TestRejectsInvalidDecimalSequences();
    TestFailedBundlePreservesConfirmedSequence();
    TestInvalidSnapshotLocksGateWithoutExplicitBegin();

    if (failures != 0) {
        fprintf(stderr, "%d protocol bridge test(s) failed\n", failures);
        return 1;
    }

    puts("client protocol bridge tests passed");
    return 0;
}
