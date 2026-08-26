#ifndef BUK_CLIENT_BRIDGE_H
#define BUK_CLIENT_BRIDGE_H

#include "buk_client/presentation_state.h"

int BukClientSetInput(const char *utf8_input);
const char *BukClientGetInput(void);
int BukClientRenderedBoardNodeCount(void);
int BukClientRenderedBoardEdgeCount(void);
int BukClientRenderedPieceCount(void);
int BukClientRenderedStackBadgeCount(void);
int BukClientRenderedRouteOptionCount(void);
int BukClientHighlightedRouteEdgeCount(void);
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
                                const char *space_id, int stacked,
                                int stack_size);
int BukClientStageSnapshotResult(const char *result);
int BukClientStageSnapshotMoveRequest(const char *required_input,
                                      int normal_route_available,
                                      int shortcut_route_available,
                                      const char *route_origin_space_id);
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
int BukClientSetRouteInteractionEnabled(int enabled);
int BukClientCanSelectRoute(void);
int BukClientRequestRouteSelection(const char *route);
const char *BukClientConsumeRouteSelection(void);
int BukClientResolveRouteCommand(void);

#endif
