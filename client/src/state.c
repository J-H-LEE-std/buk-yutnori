#include "buk_client/state.h"

#include <string.h>

void BukClientStateInit(BukClientState *state)
{
    if (state == NULL) return;

    state->input[0] = '\0';
    state->input_length = 0U;
}

bool BukClientStateSetInput(BukClientState *state, const char *utf8_input)
{
    size_t input_length;

    if ((state == NULL) || (utf8_input == NULL)) return false;

    input_length = strlen(utf8_input);
    if (input_length >= BUK_CLIENT_INPUT_CAPACITY) return false;

    memcpy(state->input, utf8_input, input_length + 1U);
    state->input_length = input_length;
    return true;
}

const char *BukClientStateInput(const BukClientState *state)
{
    if (state == NULL) return NULL;

    return state->input;
}

size_t BukClientStateInputLength(const BukClientState *state)
{
    if (state == NULL) return 0U;

    return state->input_length;
}
