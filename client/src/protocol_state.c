#include "buk_client/protocol_state.h"

#include <stddef.h>
#include <stdint.h>

void BukClientProtocolStateInit(BukClientProtocolState *state)
{
    if (state == NULL) return;

    state->last_sequence = 0U;
    state->pending_sequence = 0U;
    state->sequence_set = false;
    state->pending_sequence_set = false;
    state->synchronizing = true;
    state->resynchronization_required = false;
}

void BukClientProtocolBeginSynchronization(BukClientProtocolState *state)
{
    if (state == NULL) return;

    state->synchronizing = true;
    state->pending_sequence = 0U;
    state->pending_sequence_set = false;
    state->resynchronization_required = false;
}

bool BukClientProtocolApplySnapshot(BukClientProtocolState *state, uint64_t sequence)
{
    if ((state == NULL) || !state->synchronizing) return false;
    if (sequence == 0U) {
        state->resynchronization_required = true;
        return false;
    }
    if (state->pending_sequence_set) {
        state->resynchronization_required = true;
        return false;
    }
    if (state->sequence_set && (sequence < state->last_sequence)) {
        state->resynchronization_required = true;
        return false;
    }

    state->pending_sequence = sequence;
    state->pending_sequence_set = true;
    return true;
}

bool BukClientProtocolApplyEvent(BukClientProtocolState *state, uint64_t sequence)
{
    uint64_t current_sequence;
    bool current_sequence_set;

    if (state == NULL) return false;

    current_sequence = state->synchronizing ? state->pending_sequence : state->last_sequence;
    current_sequence_set = state->synchronizing ? state->pending_sequence_set : state->sequence_set;
    if (!current_sequence_set || state->resynchronization_required
        || (current_sequence == UINT64_MAX) || (sequence != current_sequence + 1U)) {
        if (state != NULL) state->resynchronization_required = true;
        return false;
    }

    if (state->synchronizing) state->pending_sequence = sequence;
    else state->last_sequence = sequence;
    return true;
}

bool BukClientProtocolCompleteSynchronization(BukClientProtocolState *state)
{
    if ((state == NULL) || !state->synchronizing || !state->pending_sequence_set
        || state->resynchronization_required) {
        return false;
    }

    state->last_sequence = state->pending_sequence;
    state->sequence_set = true;
    state->pending_sequence = 0U;
    state->pending_sequence_set = false;
    state->synchronizing = false;
    return true;
}

bool BukClientProtocolCanSendCommands(const BukClientProtocolState *state)
{
    return (state != NULL) && !state->synchronizing && !state->resynchronization_required;
}

bool BukClientProtocolRequiresSynchronization(const BukClientProtocolState *state)
{
    return (state != NULL) && state->resynchronization_required;
}

uint64_t BukClientProtocolLastSequence(const BukClientProtocolState *state)
{
    if ((state == NULL) || !state->sequence_set) return 0U;

    return state->last_sequence;
}
