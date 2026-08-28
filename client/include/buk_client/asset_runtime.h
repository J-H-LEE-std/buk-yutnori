#ifndef BUK_CLIENT_ASSET_RUNTIME_H
#define BUK_CLIENT_ASSET_RUNTIME_H

#include <stddef.h>

#define BUK_CLIENT_ASSET_PATH_MAX 128
#define BUK_CLIENT_ASSET_COUNT 46

enum {
    BUK_CLIENT_ASSET_BOARD_MAIN = 0,
    BUK_CLIENT_ASSET_PIECE_A_ON_BOARD = 4,
    BUK_CLIENT_ASSET_PIECE_B_ON_BOARD = 8,
    BUK_CLIENT_ASSET_YUT_RESULT_DO = 13,
};

typedef int (*BukClientAssetExistsFn)(const char *path, void *userdata);

typedef struct {
    unsigned int loaded_count;
    unsigned int fallback_count;
    int initialized;
    int available[BUK_CLIENT_ASSET_COUNT];
} BukClientAssetRuntime;

void BukClientAssetRuntimeInit(BukClientAssetRuntime *runtime,
                               const char *root,
                               BukClientAssetExistsFn exists,
                               void *userdata);
int BukClientAssetRuntimeInitialized(const BukClientAssetRuntime *runtime);
unsigned int BukClientAssetRuntimeLoadedCount(const BukClientAssetRuntime *runtime);
unsigned int BukClientAssetRuntimeFallbackCount(const BukClientAssetRuntime *runtime);
int BukClientAssetRuntimeAvailable(const BukClientAssetRuntime *runtime, size_t index);
const char *BukClientAssetRuntimePath(size_t index);

#endif
