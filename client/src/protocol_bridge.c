#include "buk_client/bridge.h"
#include "buk_client/protocol_state.h"

#include <inttypes.h>
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>

enum {
    SEQUENCE_TEXT_CAPACITY = 21,
};

static BukClientProtocolState protocol_state;
static char last_sequence_text[SEQUENCE_TEXT_CAPACITY];

static bool ParseSequence(const char *text, uint64_t *sequence)
{
    uint64_t value = 0U;
    size_t index = 0U;

    if ((text == NULL) || (sequence == NULL) || (text[0] == '\0')) return false;
    while (text[index] != '\0') {
        uint64_t digit;

        if ((text[index] < '0') || (text[index] > '9')) return false;
        digit = (uint64_t)(text[index] - '0');
        if (value > (UINT64_MAX - digit) / 10U) return false;
        value = value * 10U + digit;
        index++;
    }
    *sequence = value;
    return true;
}

void BukClientProtocolRuntimeInit(void)
{
    BukClientProtocolStateInit(&protocol_state);
}

int BukClientBeginSynchronization(void)
{
    BukClientProtocolBeginSynchronization(&protocol_state);
    return 1;
}

int BukClientApplySnapshotSequence(const char *sequence)
{
    uint64_t parsed;

    if (!ParseSequence(sequence, &parsed)) {
        BukClientProtocolBeginSynchronization(&protocol_state);
        (void)BukClientProtocolApplySnapshot(&protocol_state, 0U);
        return 0;
    }
    return BukClientProtocolApplySnapshot(&protocol_state, parsed) ? 1 : 0;
}

int BukClientApplyEventSequence(const char *sequence)
{
    uint64_t parsed;

    if (!ParseSequence(sequence, &parsed)) {
        (void)BukClientProtocolApplyEvent(&protocol_state, 0U);
        return 0;
    }
    return BukClientProtocolApplyEvent(&protocol_state, parsed) ? 1 : 0;
}

int BukClientCompleteSynchronization(void)
{
    return BukClientProtocolCompleteSynchronization(&protocol_state) ? 1 : 0;
}

int BukClientCanSendStateCommands(void)
{
    return BukClientProtocolCanSendCommands(&protocol_state) ? 1 : 0;
}

int BukClientRequiresResynchronization(void)
{
    return BukClientProtocolRequiresSynchronization(&protocol_state) ? 1 : 0;
}

const char *BukClientLastSequence(void)
{
    (void)snprintf(last_sequence_text, sizeof(last_sequence_text), "%" PRIu64,
                   BukClientProtocolLastSequence(&protocol_state));
    return last_sequence_text;
}
