#include "buk_client/asset_runtime.h"

#include <stdio.h>
#include <string.h>

static const char *const asset_paths[BUK_CLIENT_ASSET_COUNT] = {
    "board/board_main.png", "board/node_marker.png", "board/node_marker_last.png",
    "board/path_highlight.png", "piece/a_on_board.png", "piece/a_home_checkpoint.png",
    "piece/a_finished.png", "piece/a_waiting.png", "piece/b_on_board.png",
    "piece/b_home_checkpoint.png", "piece/b_finished.png", "piece/b_waiting.png",
    "piece/movable_outline.png", "yut/result_do.png", "yut/result_gae.png",
    "yut/result_geol.png", "yut/result_yut.png", "yut/result_mo.png",
    "yut/result_backdo.png", "yut/result_buk.png", "yut/toss_00.png",
    "yut/toss_01.png", "yut/toss_02.png", "yut/toss_03.png", "yut/toss_04.png",
    "yut/toss_05.png", "yut/toss_06.png", "yut/toss_07.png", "fx/capture_flash.png",
    "fx/stack_pop.png", "gui/common/panel.png", "gui/common/button_normal.png",
    "gui/common/button_hover.png", "gui/common/button_pressed.png",
    "gui/common/button_disabled.png", "gui/common/slot_frame.png",
    "gui/common/badge_ready.png", "gui/common/badge_watch.png",
    "gui/common/marker_team_a.png", "gui/common/marker_team_b.png",
    "gui/common/stack_count.png", "screen/game/hud_frame.png",
    "screen/game/result_queue_panel.png", "screen/game/turn_banner.png",
    "font/notosans_kr_regular.ttf", "font/notosans_kr_bold.ttf",
};

void BukClientAssetRuntimeInit(BukClientAssetRuntime *runtime,
                               const char *root,
                               BukClientAssetExistsFn exists,
                               void *userdata)
{
    size_t index;
    char path[BUK_CLIENT_ASSET_PATH_MAX];

    if (runtime == NULL) return;
    memset(runtime, 0, sizeof(*runtime));
    if (root == NULL || root[0] == '\0' || exists == NULL) return;
    for (index = 0U; index < BUK_CLIENT_ASSET_COUNT; index++) {
        int written = snprintf(path, sizeof(path), "%s/%s", root, asset_paths[index]);
        if (written < 0 || (size_t)written >= sizeof(path)) {
            runtime->fallback_count++;
            continue;
        }
        runtime->available[index] = exists(path, userdata) ? 1 : 0;
        if (runtime->available[index] != 0) runtime->loaded_count++;
        else runtime->fallback_count++;
    }
    runtime->initialized = 1;
}

int BukClientAssetRuntimeInitialized(const BukClientAssetRuntime *runtime)
{
    return runtime != NULL && runtime->initialized != 0;
}

unsigned int BukClientAssetRuntimeLoadedCount(const BukClientAssetRuntime *runtime)
{
    return runtime == NULL ? 0U : runtime->loaded_count;
}

unsigned int BukClientAssetRuntimeFallbackCount(const BukClientAssetRuntime *runtime)
{
    return runtime == NULL ? 0U : runtime->fallback_count;
}

int BukClientAssetRuntimeAvailable(const BukClientAssetRuntime *runtime, size_t index)
{
    return runtime != NULL && index < BUK_CLIENT_ASSET_COUNT && runtime->available[index] != 0;
}

const char *BukClientAssetRuntimePath(size_t index)
{
    return index < BUK_CLIENT_ASSET_COUNT ? asset_paths[index] : NULL;
}
