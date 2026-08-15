#include "buk_client/state.h"

#include <stdio.h>
#include <string.h>

static int failures;

#define CHECK(condition) Check((condition), #condition, __FILE__, __LINE__)

static void Check(bool condition, const char *expression, const char *file, int line)
{
    if (condition) return;

    fprintf(stderr, "%s:%d: check failed: %s\n", file, line, expression);
    failures++;
}

static void TestInitialStateIsEmpty(void)
{
    BukClientState state;

    BukClientStateInit(&state);

    CHECK(strcmp(BukClientStateInput(&state), "") == 0);
    CHECK(BukClientStateInputLength(&state) == 0U);
}

static void TestPreservesKoreanUtf8Input(void)
{
    const char *input = "안녕하세요, 윷놀이!";
    BukClientState state;

    BukClientStateInit(&state);

    CHECK(BukClientStateSetInput(&state, input));
    CHECK(strcmp(BukClientStateInput(&state), input) == 0);
    CHECK(BukClientStateInputLength(&state) == strlen(input));
}

static void TestRejectsOversizedInputWithoutMutation(void)
{
    char oversized[BUK_CLIENT_INPUT_CAPACITY + 1U];
    BukClientState state;

    memset(oversized, 'a', sizeof(oversized) - 1U);
    oversized[sizeof(oversized) - 1U] = '\0';
    BukClientStateInit(&state);

    CHECK(BukClientStateSetInput(&state, "previous"));
    CHECK(!BukClientStateSetInput(&state, oversized));
    CHECK(strcmp(BukClientStateInput(&state), "previous") == 0);
    CHECK(BukClientStateInputLength(&state) == strlen("previous"));
}

static void TestRejectsNullArguments(void)
{
    BukClientState state;

    BukClientStateInit(&state);

    CHECK(!BukClientStateSetInput(NULL, "input"));
    CHECK(!BukClientStateSetInput(&state, NULL));
    CHECK(BukClientStateInput(NULL) == NULL);
    CHECK(BukClientStateInputLength(NULL) == 0U);
}

int main(void)
{
    TestInitialStateIsEmpty();
    TestPreservesKoreanUtf8Input();
    TestRejectsOversizedInputWithoutMutation();
    TestRejectsNullArguments();

    if (failures != 0) {
        fprintf(stderr, "%d client state test(s) failed\n", failures);
        return 1;
    }

    puts("client state tests passed");
    return 0;
}
