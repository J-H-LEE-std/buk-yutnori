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
    static const char *const expected_results[] = {
        "yut/result_do.png",
        "yut/result_gae.png",
        "yut/result_geol.png",
        "yut/result_yut.png",
        "yut/result_mo.png",
        "yut/result_backdo.png",
        "yut/result_buk.png",
    };
    BukClientAssetRuntime runtime;
    char long_root[BUK_CLIENT_ASSET_PATH_MAX + 32];

    BukClientAssetRuntimeInit(&runtime, "assets", ExistsAll, NULL);
    assert(BukClientAssetRuntimeInitialized(&runtime));
    assert(BukClientAssetRuntimeLoadedCount(&runtime) == BUK_CLIENT_ASSET_COUNT);
    assert(BukClientAssetRuntimeFallbackCount(&runtime) == 0U);
    assert(strcmp(BukClientAssetRuntimePath(0U), "board/board_main.png") == 0);
    assert(BukClientAssetRuntimeAvailable(&runtime, 0U));
    assert(strcmp(BukClientAssetRuntimePath(13U), "piece/finished_crown.png") == 0);
    for (size_t index = 0U; index < sizeof(expected_results) / sizeof(expected_results[0]); index++) {
        assert(strcmp(BukClientAssetRuntimePath(BUK_CLIENT_ASSET_YUT_RESULT_DO + index),
                      expected_results[index]) == 0);
    }
    assert(strcmp(BukClientAssetRuntimePath(42U), "gui/common/menu_frame.png") == 0);
    assert(strcmp(BukClientAssetRuntimePath(43U), "gui/common/modal_frame.png") == 0);
    assert(!BukClientAssetRuntimeAvailable(&runtime, BUK_CLIENT_ASSET_COUNT));

    BukClientAssetRuntimeInit(&runtime, "assets", ExistsBoardOnly, NULL);
    assert(BukClientAssetRuntimeLoadedCount(&runtime) == 4U);
    assert(BukClientAssetRuntimeFallbackCount(&runtime) == BUK_CLIENT_ASSET_COUNT - 4U);
    assert(BukClientAssetRuntimeInitialized(&runtime));

    memset(long_root, 'x', sizeof(long_root) - 1U);
    long_root[sizeof(long_root) - 1U] = '\0';
    BukClientAssetRuntimeInit(&runtime, long_root, ExistsAll, NULL);
    assert(BukClientAssetRuntimeLoadedCount(&runtime) == 0U);
    assert(BukClientAssetRuntimeFallbackCount(&runtime) == BUK_CLIENT_ASSET_COUNT);

    BukClientAssetRuntimeInit(&runtime, NULL, ExistsAll, NULL);
    assert(!BukClientAssetRuntimeInitialized(&runtime));
    puts("asset runtime tests passed");
    return 0;
}
