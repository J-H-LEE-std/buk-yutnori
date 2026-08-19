#ifndef BUK_CLIENT_PROTOCOL_STATE_H
#define BUK_CLIENT_PROTOCOL_STATE_H

#include <stdbool.h>
#include <stdint.h>

typedef struct BukClientProtocolState {
    uint64_t last_sequence;
    uint64_t pending_sequence;
    bool sequence_set;
    bool pending_sequence_set;
    bool synchronizing;
    bool resynchronization_required;
} BukClientProtocolState;

void BukClientProtocolStateInit(BukClientProtocolState *state);
void BukClientProtocolBeginSynchronization(BukClientProtocolState *state);
bool BukClientProtocolApplySnapshot(BukClientProtocolState *state, uint64_t sequence);
bool BukClientProtocolApplyEvent(BukClientProtocolState *state, uint64_t sequence);
bool BukClientProtocolCompleteSynchronization(BukClientProtocolState *state);
bool BukClientProtocolCanSendCommands(const BukClientProtocolState *state);
bool BukClientProtocolRequiresSynchronization(const BukClientProtocolState *state);
uint64_t BukClientProtocolLastSequence(const BukClientProtocolState *state);

#endif
