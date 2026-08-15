#ifndef BUK_CLIENT_STATE_H
#define BUK_CLIENT_STATE_H

#include <stdbool.h>
#include <stddef.h>

#define BUK_CLIENT_INPUT_CAPACITY 1024U

typedef struct BukClientState {
    char input[BUK_CLIENT_INPUT_CAPACITY];
    size_t input_length;
} BukClientState;

void BukClientStateInit(BukClientState *state);
bool BukClientStateSetInput(BukClientState *state, const char *utf8_input);
const char *BukClientStateInput(const BukClientState *state);
size_t BukClientStateInputLength(const BukClientState *state);

#endif
