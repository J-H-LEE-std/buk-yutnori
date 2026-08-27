#include "buk_client/asset_runtime.h"

#include <assert.h>
#include <stdio.h>
#include <string.h>

static int ExistsAll(const char *path, void *userdata)
{
    (void)userdata;
    return strstr(path, "not-found") == NULL;
}

static int ExistsBoardOnly(const char *path, void *userdata)
{
    (void)userdata;
    return strstr(path, "/board/") != NULL;
}

int main(void)
{
    BukClientAssetRuntime runtime;

    BukClientAssetRuntimeInit(&runtime, "assets", ExistsAll, NULL);
    assert(BukClientAssetRuntimeInitialized(&runtime));
    assert(BukClientAssetRuntimeLoadedCount(&runtime) == BUK_CLIENT_ASSET_COUNT);
    assert(BukClientAssetRuntimeFallbackCount(&runtime) == 0U);
    assert(strcmp(BukClientAssetRuntimePath(0U), "board/board_main.png") == 0);
    assert(BukClientAssetRuntimeAvailable(&runtime, 0U));
    assert(!BukClientAssetRuntimeAvailable(&runtime, BUK_CLIENT_ASSET_COUNT));

    BukClientAssetRuntimeInit(&runtime, "assets", ExistsBoardOnly, NULL);
    assert(BukClientAssetRuntimeLoadedCount(&runtime) == 4U);
    assert(BukClientAssetRuntimeFallbackCount(&runtime) == BUK_CLIENT_ASSET_COUNT - 4U);
    assert(BukClientAssetRuntimeInitialized(&runtime));

    BukClientAssetRuntimeInit(&runtime, NULL, ExistsAll, NULL);
    assert(!BukClientAssetRuntimeInitialized(&runtime));
    puts("asset runtime tests passed");
    return 0;
}
