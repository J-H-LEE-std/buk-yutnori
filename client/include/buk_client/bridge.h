#ifndef BUK_CLIENT_BRIDGE_H
#define BUK_CLIENT_BRIDGE_H

int BukClientSetInput(const char *utf8_input);
const char *BukClientGetInput(void);
void BukClientProtocolRuntimeInit(void);
int BukClientBeginSynchronization(void);
int BukClientApplySnapshotSequence(const char *sequence);
int BukClientApplyEventSequence(const char *sequence);
int BukClientCompleteSynchronization(void);
int BukClientCanSendStateCommands(void);
int BukClientRequiresResynchronization(void);
const char *BukClientLastSequence(void);

#endif
