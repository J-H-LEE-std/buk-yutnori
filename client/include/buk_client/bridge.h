#ifndef BUK_CLIENT_BRIDGE_H
#define BUK_CLIENT_BRIDGE_H

#include "buk_client/presentation_state.h"

int BukClientSetInput(const char *utf8_input);
const char *BukClientGetInput(void);
int BukClientRenderedBoardNodeCount(void);
int BukClientRenderedBoardEdgeCount(void);
int BukClientRenderedPieceCount(void);
void BukClientProtocolRuntimeInit(void);
int BukClientBeginSynchronization(void);
int BukClientApplySnapshotSequence(const char *sequence);
int BukClientApplyEventSequence(const char *sequence);
int BukClientStageSnapshotMetadata(const char *status, const char *phase,
                                   const char *required_input,
                                   const char *timer_phase,
                                   const char *current_team,
                                   const char *remaining_ms);
int BukClientStageSnapshotPiece(const char *team, const char *piece_state,
                                const char *space_id);
int BukClientStageSnapshotResult(const char *result);
int BukClientIsCanonicalBoardSpace(const char *space_id);
int BukClientFailSynchronization(void);
int BukClientCompleteSynchronization(void);
int BukClientCanSendStateCommands(void);
int BukClientRequiresResynchronization(void);
const char *BukClientLastSequence(void);
const BukClientPresentationSnapshot *BukClientConfirmedPresentation(void);
int BukClientHasPresentationSnapshot(void);
int BukClientPresentationPieceCount(void);
int BukClientPresentationResultCount(void);
const char *BukClientPresentationStatus(void);
const char *BukClientPresentationTurnPhase(void);
const char *BukClientPresentationRequiredInput(void);
const char *BukClientPresentationCurrentTeam(void);
const char *BukClientPresentationRemainingMilliseconds(void);

#endif
