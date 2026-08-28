#include "buk_client/board_layout.h"
#include "buk_client/asset_runtime.h"
#include "buk_client/bridge.h"
#include "buk_client/state.h"

#include "raylib.h"

#include <stdio.h>
#include <string.h>

#if defined(PLATFORM_WEB)
#include <emscripten/emscripten.h>
#endif

enum {
    SCREEN_WIDTH = 1280,
    SCREEN_HEIGHT = 720,
};

static BukClientState client_state;
static int rendered_board_node_count;
static int rendered_board_edge_count;
static int rendered_piece_count;
static int rendered_stack_badge_count;
static int rendered_route_option_count;
static int highlighted_route_edge_count;
static BukClientAssetRuntime asset_runtime;
static Texture2D board_texture;
static Texture2D piece_texture_a;
static Texture2D piece_texture_b;
static Texture2D result_textures[BUK_CLIENT_RESULT_COUNT];

static Texture2D LoadAssetTexture(size_t index)
{
    const char *relative = BukClientAssetRuntimePath(index);
    char path[BUK_CLIENT_ASSET_PATH_MAX];

    if (relative == NULL || !BukClientAssetRuntimeAvailable(&asset_runtime, index)) {
        return (Texture2D){ 0 };
    }
    (void)snprintf(path, sizeof(path), "assets/%s", relative);
    return LoadTexture(path);
}

static int AssetFileExists(const char *path, void *userdata)
{
    (void)userdata;
    return FileExists(path) ? 1 : 0;
}

static Rectangle RaylibRectangle(BukClientRect rectangle)
{
    return (Rectangle){ rectangle.x, rectangle.y, rectangle.width, rectangle.height };
}

static BukClientRect LogicalRectangle(const BukClientGameLayout *layout, float x, float y,
                                      float width, float height)
{
    return (BukClientRect){
        layout->content.x + (x * layout->scale),
        layout->content.y + (y * layout->scale),
        width * layout->scale,
        height * layout->scale,
    };
}

static bool IsMajorNode(BukClientBoardNodeId node_id)
{
    return (node_id == BUK_CLIENT_BOARD_NODE_CHAMMEOGI) ||
           (node_id == BUK_CLIENT_BOARD_NODE_MO) ||
           (node_id == BUK_CLIENT_BOARD_NODE_BACK_MO) ||
           (node_id == BUK_CLIENT_BOARD_NODE_JJI_MO) ||
           (node_id == BUK_CLIENT_BOARD_NODE_BANG);
}

static void DrawCanonicalBoard(const BukClientGameLayout *layout)
{
    const Color board_background = { 236, 218, 176, 255 };
    const Color board_ink = { 67, 50, 38, 255 };
    const Color node_fill = { 255, 249, 231, 255 };
    const Color start_fill = { 200, 84, 65, 255 };
    const BukClientBoardEdge *edges;
    const BukClientBoardNode *nodes;
    size_t edge_count;
    size_t edge_index;
    size_t node_count;
    size_t node_index;

    rendered_board_node_count = 0;
    rendered_board_edge_count = 0;
    if (board_texture.id != 0U) {
        DrawTexturePro(board_texture,
                       (Rectangle){ 0.0F, 0.0F, (float)board_texture.width,
                                   (float)board_texture.height },
                       RaylibRectangle(layout->board), (Vector2){ 0.0F, 0.0F }, 0.0F,
                       WHITE);
    } else {
        DrawRectangleRounded(RaylibRectangle(layout->board), 0.04F, 12, board_background);
    }
    edges = BukClientBoardEdges(&edge_count);
    for (edge_index = 0U; edge_index < edge_count; edge_index++) {
        BukClientPoint from;
        BukClientPoint to;

        if (!BukClientBoardMapNode(layout->board, edges[edge_index].from, &from) ||
            !BukClientBoardMapNode(layout->board, edges[edge_index].to, &to)) {
            continue;
        }
        DrawLineEx((Vector2){ from.x, from.y }, (Vector2){ to.x, to.y },
                   4.0F * layout->scale, board_ink);
        rendered_board_edge_count++;
    }

    nodes = BukClientBoardNodes(&node_count);
    for (node_index = 0U; node_index < node_count; node_index++) {
        BukClientPoint point;
        float radius;
        Color fill;

        if (!BukClientBoardMapNode(layout->board, nodes[node_index].id, &point)) continue;
        radius = (IsMajorNode(nodes[node_index].id) ? 18.0F : 11.0F) * layout->scale;
        fill = nodes[node_index].id == BUK_CLIENT_BOARD_NODE_CHAMMEOGI
                   ? start_fill
                   : node_fill;
        DrawCircleV((Vector2){ point.x, point.y }, radius + (2.0F * layout->scale),
                    board_ink);
        DrawCircleV((Vector2){ point.x, point.y }, radius, fill);
        rendered_board_node_count++;
    }
}

static void DrawAuthoritativeRouteHighlights(const BukClientGameLayout *layout)
{
    const BukClientPresentationSnapshot *snapshot = BukClientConfirmedPresentation();
    BukClientBoardNodeId normal;
    BukClientBoardNodeId shortcut;
    BukClientPoint origin_point;
    BukClientPoint normal_point;
    BukClientPoint shortcut_point;
    float width = 10.0F * layout->scale;

    highlighted_route_edge_count = 0;
    if (snapshot == NULL || !snapshot->move_request_set ||
        snapshot->move_request_input != BUK_CLIENT_REQUIRED_SELECT_ROUTE ||
        !BukClientBoardRouteTargets(snapshot->route_origin, &normal, &shortcut) ||
        !BukClientBoardMapNode(layout->board, snapshot->route_origin, &origin_point) ||
        !BukClientBoardMapNode(layout->board, normal, &normal_point) ||
        !BukClientBoardMapNode(layout->board, shortcut, &shortcut_point)) {
        return;
    }
    if (snapshot->normal_route_available) {
        DrawLineEx((Vector2){ origin_point.x, origin_point.y },
                   (Vector2){ normal_point.x, normal_point.y }, width,
                   (Color){ 221, 156, 48, 210 });
        highlighted_route_edge_count++;
    }
    if (snapshot->shortcut_route_available) {
        DrawLineEx((Vector2){ origin_point.x, origin_point.y },
                   (Vector2){ shortcut_point.x, shortcut_point.y }, width,
                   (Color){ 29, 154, 119, 220 });
        highlighted_route_edge_count++;
    }
}

static size_t CountPieces(const BukClientPresentationSnapshot *snapshot,
                          BukClientTeam team, BukClientPieceState state)
{
    size_t count = 0U;
    size_t index;

    if (snapshot == NULL) return 0U;
    for (index = 0U; index < snapshot->piece_count; index++) {
        if (snapshot->pieces[index].team == team &&
            snapshot->pieces[index].state == state) {
            count++;
        }
    }
    return count;
}

static void DrawAuthoritativePieces(const BukClientGameLayout *layout)
{
    static const float offset_x[] = { 0.0F, -15.0F, 15.0F, 0.0F, 0.0F,
                                      -15.0F, 15.0F, -15.0F, 15.0F };
    static const float offset_y[] = { 0.0F, 0.0F, 0.0F, -15.0F, 15.0F,
                                      -15.0F, -15.0F, 15.0F, 15.0F };
    const Color outline = { 45, 35, 32, 255 };
    const Color team_a = { 198, 69, 58, 255 };
    const Color team_b = { 51, 102, 174, 255 };
    const Color label = { 255, 250, 240, 255 };
    const BukClientPresentationSnapshot *snapshot = BukClientConfirmedPresentation();
    size_t piece_index;

    rendered_piece_count = 0;
    rendered_stack_badge_count = 0;
    if (snapshot == NULL) return;
    for (piece_index = 0U; piece_index < snapshot->piece_count; piece_index++) {
        const BukClientPresentationPiece piece = snapshot->pieces[piece_index];
        BukClientPoint point;
        size_t previous_at_node = 0U;
        size_t previous_stack_at_node = 0U;
        size_t previous_index;
        size_t offset_index;
        float ring;
        float radius = 10.0F * layout->scale;
        int label_size = (int)(12.0F * layout->scale);
        Color fill;

        if ((piece.state != BUK_CLIENT_PIECE_ON_BOARD &&
             piece.state != BUK_CLIENT_PIECE_HOME_CHECKPOINT) ||
            !BukClientBoardMapNode(layout->board, piece.node, &point)) {
            continue;
        }
        for (previous_index = 0U; previous_index < piece_index; previous_index++) {
            const BukClientPresentationPiece previous = snapshot->pieces[previous_index];

            if ((previous.state == BUK_CLIENT_PIECE_ON_BOARD ||
                 previous.state == BUK_CLIENT_PIECE_HOME_CHECKPOINT) &&
                previous.node == piece.node && previous.team == piece.team &&
                previous.state == piece.state) {
                previous_at_node++;
            }
        }
        for (previous_index = 0U; previous_index < piece_index; previous_index++) {
            const BukClientPresentationPiece grouped = snapshot->pieces[previous_index];

            if (grouped.stacked && grouped.node == piece.node &&
                grouped.team == piece.team && grouped.state == piece.state) {
                previous_stack_at_node++;
            }
        }
        offset_index = previous_at_node % (sizeof(offset_x) / sizeof(offset_x[0]));
        ring = 1.0F + (float)(previous_at_node /
                             (sizeof(offset_x) / sizeof(offset_x[0])));
        point.x += offset_x[offset_index] * ring * layout->scale;
        point.y += offset_y[offset_index] * ring * layout->scale;
        fill = piece.team == BUK_CLIENT_TEAM_A ? team_a : team_b;
        Texture2D texture = piece.team == BUK_CLIENT_TEAM_A ? piece_texture_a : piece_texture_b;
        if (texture.id != 0U) {
            float diameter = radius * 2.0F;
            DrawTexturePro(texture,
                           (Rectangle){ 0.0F, 0.0F, (float)texture.width,
                                       (float)texture.height },
                           (Rectangle){ point.x - radius, point.y - radius, diameter, diameter },
                           (Vector2){ 0.0F, 0.0F }, 0.0F, WHITE);
        } else {
            DrawCircleV((Vector2){ point.x, point.y }, radius + (2.0F * layout->scale), outline);
            DrawCircleV((Vector2){ point.x, point.y }, radius, fill);
            if (label_size < 7) label_size = 7;
            DrawText(TextFormat("%i", (int)piece_index + 1),
                     (int)(point.x - (4.0F * layout->scale)),
                     (int)(point.y - (6.0F * layout->scale)), label_size, label);
        }
        if (piece.stacked && piece.stack_size >= 2U &&
            previous_stack_at_node + 1U == piece.stack_size) {
            const float badge_radius = 9.0F * layout->scale;
            DrawCircleV((Vector2){ point.x + (radius * 0.8F),
                                   point.y - (radius * 0.8F) },
                        badge_radius, (Color){ 45, 35, 32, 255 });
            DrawText(TextFormat("%i", (int)piece.stack_size),
                     (int)(point.x + (radius * 0.8F) - (4.0F * layout->scale)),
                     (int)(point.y - (radius * 0.8F) - (6.0F * layout->scale)),
                     label_size, label);
            rendered_stack_badge_count++;
        }
        rendered_piece_count++;
    }
}

static void DrawGameHud(const BukClientGameLayout *layout)
{
    const Color panel = { 255, 252, 244, 255 };
    const Color ink = { 55, 42, 35, 255 };
    const Color muted = { 126, 104, 88, 255 };
    const Color accent = { 29, 111, 92, 255 };
    const BukClientRect hud = LogicalRectangle(layout, 720.0F, 40.0F, 520.0F, 640.0F);
    const BukClientRect status = LogicalRectangle(layout, 752.0F, 150.0F, 456.0F, 154.0F);
    const BukClientRect queue = LogicalRectangle(layout, 752.0F, 328.0F, 456.0F, 154.0F);
    const BukClientRect command = LogicalRectangle(layout, 752.0F, 522.0F, 456.0F, 110.0F);
    const BukClientPresentationSnapshot *snapshot = BukClientConfirmedPresentation();
    int body_size = (int)(20.0F * layout->scale);
    int heading_size = (int)(30.0F * layout->scale);

    rendered_route_option_count = 0;

    if (body_size < 8) body_size = 8;
    if (heading_size < 10) heading_size = 10;

    DrawRectangleRounded(RaylibRectangle(hud), 0.04F, 10, panel);
    DrawText("BUK YUTNORI", (int)(hud.x + (32.0F * layout->scale)),
             (int)(hud.y + (28.0F * layout->scale)), heading_size, ink);
    DrawText("authoritative snapshot presentation",
             (int)(hud.x + (32.0F * layout->scale)),
             (int)(hud.y + (70.0F * layout->scale)), body_size, muted);
    if (strcmp(BukClientEventCueName(), "none") != 0) {
        const BukClientRect cue = LogicalRectangle(layout, 752.0F, 106.0F,
                                                    456.0F, 32.0F);
        const char *label = strcmp(BukClientEventCueName(), "backdo") == 0
                                ? "BACKDO CONFIRMED" : "BUK RESOLVED";
        DrawRectangleRounded(RaylibRectangle(cue), 0.18F, 8,
                             (Color){ 221, 156, 48, 255 });
        DrawText(label, (int)(cue.x + (18.0F * layout->scale)),
                 (int)(cue.y + (6.0F * layout->scale)), body_size,
                 (Color){ 67, 50, 38, 255 });
    }

    DrawRectangleRounded(RaylibRectangle(status), 0.06F, 8,
                         (Color){ 236, 244, 239, 255 });
    DrawText("MATCH", (int)(status.x + (24.0F * layout->scale)),
             (int)(status.y + (22.0F * layout->scale)), body_size, accent);
    if (snapshot == NULL) {
        DrawText("Waiting for authoritative snapshot",
                 (int)(status.x + (24.0F * layout->scale)),
                 (int)(status.y + (66.0F * layout->scale)), body_size, ink);
    } else {
        DrawText(TextFormat("%s | TURN %s", BukClientMatchStatusName(snapshot->status),
                            BukClientTeamName(snapshot->current_team)),
                 (int)(status.x + (24.0F * layout->scale)),
                 (int)(status.y + (58.0F * layout->scale)), body_size, ink);
        DrawText(BukClientTurnPhaseName(snapshot->phase),
                 (int)(status.x + (24.0F * layout->scale)),
                 (int)(status.y + (88.0F * layout->scale)), body_size, ink);
        DrawText(TextFormat("%s | %.1fs",
                            BukClientRequiredInputName(snapshot->required_input),
                            (double)snapshot->remaining_ms / 1000.0),
                 (int)(status.x + (24.0F * layout->scale)),
                 (int)(status.y + (116.0F * layout->scale)), body_size, muted);
    }

    DrawRectangleRounded(RaylibRectangle(queue), 0.06F, 8,
                         (Color){ 247, 239, 222, 255 });
    DrawText("RESULT QUEUE", (int)(queue.x + (24.0F * layout->scale)),
             (int)(queue.y + (22.0F * layout->scale)), body_size, muted);
    if (snapshot == NULL || snapshot->result_count == 0U) {
        DrawText(snapshot == NULL ? "Waiting for authoritative snapshot" : "empty",
                 (int)(queue.x + (24.0F * layout->scale)),
                 (int)(queue.y + (66.0F * layout->scale)), body_size, ink);
    } else {
        size_t result_index;
        size_t visible = snapshot->result_count < 6U ? snapshot->result_count : 6U;

        for (result_index = 0U; result_index < visible; result_index++) {
            BukClientRect token = LogicalRectangle(
                layout, 776.0F + ((float)result_index * 66.0F), 386.0F, 56.0F, 52.0F);

            Texture2D result_texture = result_textures[snapshot->results[result_index]];
            if (result_texture.id != 0U) {
                DrawTexturePro(result_texture,
                               (Rectangle){ 0.0F, 0.0F, (float)result_texture.width,
                                           (float)result_texture.height },
                               RaylibRectangle(token), (Vector2){ 0.0F, 0.0F }, 0.0F, WHITE);
            } else {
                DrawRectangleRounded(RaylibRectangle(token), 0.14F, 6,
                                     (Color){ 255, 250, 240, 255 });
                DrawText(BukClientResultName(snapshot->results[result_index]),
                         (int)(token.x + (7.0F * layout->scale)),
                         (int)(token.y + (17.0F * layout->scale)), body_size, ink);
            }
        }
        if (snapshot->result_count > visible) {
            DrawText(TextFormat("+%i", (int)(snapshot->result_count - visible)),
                     (int)(queue.x + (408.0F * layout->scale)),
                     (int)(queue.y + (78.0F * layout->scale)), body_size, muted);
        }
    }

    DrawRectangleRounded(RaylibRectangle(command), 0.06F, 8,
                         (Color){ 67, 50, 38, 255 });
    if (snapshot != NULL && snapshot->move_request_set &&
        snapshot->move_request_input == BUK_CLIENT_REQUIRED_SELECT_ROUTE) {
        const BukClientRect normal = LogicalRectangle(layout, 768.0F, 542.0F,
                                                       200.0F, 70.0F);
        const BukClientRect shortcut = LogicalRectangle(layout, 992.0F, 542.0F,
                                                         200.0F, 70.0F);
        const bool enabled = BukClientCanSelectRoute() == 1;
        const Color disabled = { 116, 108, 99, 255 };
        int button_text_size = (int)(22.0F * layout->scale);

        if (button_text_size < 8) button_text_size = 8;
        if (snapshot->normal_route_available) {
            DrawRectangleRounded(RaylibRectangle(normal), 0.16F, 8,
                                 enabled ? (Color){ 221, 156, 48, 255 } : disabled);
            DrawText("NORMAL", (int)(normal.x + (48.0F * layout->scale)),
                     (int)(normal.y + (23.0F * layout->scale)), button_text_size,
                     (Color){ 255, 249, 231, 255 });
            rendered_route_option_count++;
        }
        if (snapshot->shortcut_route_available) {
            DrawRectangleRounded(RaylibRectangle(shortcut), 0.16F, 8,
                                 enabled ? (Color){ 29, 154, 119, 255 } : disabled);
            DrawText("SHORTCUT", (int)(shortcut.x + (32.0F * layout->scale)),
                     (int)(shortcut.y + (23.0F * layout->scale)), button_text_size,
                     (Color){ 255, 249, 231, 255 });
            rendered_route_option_count++;
        }
    } else {
        DrawText("PIECES", (int)(command.x + (24.0F * layout->scale)),
                 (int)(command.y + (22.0F * layout->scale)), body_size,
                 (Color){ 255, 249, 231, 255 });
        if (snapshot == NULL) {
            DrawText("A --  |  B --", (int)(command.x + (24.0F * layout->scale)),
                     (int)(command.y + (62.0F * layout->scale)), body_size,
                     (Color){ 213, 232, 222, 255 });
        } else {
            DrawText(TextFormat("A wait %i / finish %i | B wait %i / finish %i",
                                (int)CountPieces(snapshot, BUK_CLIENT_TEAM_A,
                                                 BUK_CLIENT_PIECE_WAITING),
                                (int)CountPieces(snapshot, BUK_CLIENT_TEAM_A,
                                                 BUK_CLIENT_PIECE_FINISHED),
                                (int)CountPieces(snapshot, BUK_CLIENT_TEAM_B,
                                                 BUK_CLIENT_PIECE_WAITING),
                                (int)CountPieces(snapshot, BUK_CLIENT_TEAM_B,
                                                 BUK_CLIENT_PIECE_FINISHED)),
                     (int)(command.x + (24.0F * layout->scale)),
                     (int)(command.y + (62.0F * layout->scale)), body_size,
                     (Color){ 213, 232, 222, 255 });
        }
    }
}

static void UpdateRouteSelectionInput(const BukClientGameLayout *layout)
{
    const BukClientPresentationSnapshot *snapshot = BukClientConfirmedPresentation();
    const BukClientRect normal = LogicalRectangle(layout, 768.0F, 542.0F,
                                                   200.0F, 70.0F);
    const BukClientRect shortcut = LogicalRectangle(layout, 992.0F, 542.0F,
                                                     200.0F, 70.0F);
    Vector2 mouse;

    if (snapshot == NULL || !snapshot->move_request_set ||
        snapshot->move_request_input != BUK_CLIENT_REQUIRED_SELECT_ROUTE ||
        BukClientCanSelectRoute() != 1 || !IsMouseButtonPressed(MOUSE_BUTTON_LEFT)) {
        return;
    }
    mouse = GetMousePosition();
    if (snapshot->normal_route_available &&
        CheckCollisionPointRec(mouse, RaylibRectangle(normal))) {
        (void)BukClientRequestRouteSelection("normal");
    } else if (snapshot->shortcut_route_available &&
               CheckCollisionPointRec(mouse, RaylibRectangle(shortcut))) {
        (void)BukClientRequestRouteSelection("shortcut");
    }
}

static void UpdateDrawFrame(void)
{
    BukClientGameLayout layout;

    BeginDrawing();
    ClearBackground((Color){ 36, 29, 25, 255 });
    if (BukClientCalculateGameLayout((float)GetScreenWidth(), (float)GetScreenHeight(),
                                     &layout)) {
        UpdateRouteSelectionInput(&layout);
        DrawRectangleRec(RaylibRectangle(layout.content),
                         (Color){ 245, 239, 225, 255 });
        DrawCanonicalBoard(&layout);
        DrawAuthoritativeRouteHighlights(&layout);
        DrawAuthoritativePieces(&layout);
        DrawGameHud(&layout);
    }

    EndDrawing();
}

#if defined(PLATFORM_WEB)
EMSCRIPTEN_KEEPALIVE
#endif
int BukClientSetInput(const char *utf8_input)
{
    return BukClientStateSetInput(&client_state, utf8_input) ? 1 : 0;
}

#if defined(PLATFORM_WEB)
EMSCRIPTEN_KEEPALIVE
#endif
const char *BukClientGetInput(void)
{
    return BukClientStateInput(&client_state);
}

#if defined(PLATFORM_WEB)
EMSCRIPTEN_KEEPALIVE
#endif
int BukClientRenderedBoardNodeCount(void)
{
    return rendered_board_node_count;
}

#if defined(PLATFORM_WEB)
EMSCRIPTEN_KEEPALIVE
#endif
int BukClientRenderedBoardEdgeCount(void)
{
    return rendered_board_edge_count;
}

#if defined(PLATFORM_WEB)
EMSCRIPTEN_KEEPALIVE
#endif
int BukClientRenderedPieceCount(void)
{
    return rendered_piece_count;
}

#if defined(PLATFORM_WEB)
EMSCRIPTEN_KEEPALIVE
#endif
int BukClientRenderedStackBadgeCount(void)
{
    return rendered_stack_badge_count;
}

#if defined(PLATFORM_WEB)
EMSCRIPTEN_KEEPALIVE
#endif
int BukClientRenderedRouteOptionCount(void)
{
    return rendered_route_option_count;
}

#if defined(PLATFORM_WEB)
EMSCRIPTEN_KEEPALIVE
#endif
int BukClientHighlightedRouteEdgeCount(void)
{
    return highlighted_route_edge_count;
}

#if defined(PLATFORM_WEB)
EMSCRIPTEN_KEEPALIVE
#endif
int BukClientAssetsInitialized(void)
{
    return BukClientAssetRuntimeInitialized(&asset_runtime);
}

#if defined(PLATFORM_WEB)
EMSCRIPTEN_KEEPALIVE
#endif
int BukClientAssetsLoadedCount(void)
{
    return (int)BukClientAssetRuntimeLoadedCount(&asset_runtime);
}

#if defined(PLATFORM_WEB)
EMSCRIPTEN_KEEPALIVE
#endif
int BukClientAssetsFallbackCount(void)
{
    return (int)BukClientAssetRuntimeFallbackCount(&asset_runtime);
}

int main(void)
{
    BukClientStateInit(&client_state);
    BukClientProtocolRuntimeInit();
#if !defined(PLATFORM_WEB)
    SetConfigFlags(FLAG_WINDOW_RESIZABLE);
#endif
    InitWindow(SCREEN_WIDTH, SCREEN_HEIGHT, "Buk Yutnori board prototype");
    BukClientAssetRuntimeInit(&asset_runtime, "assets", AssetFileExists, NULL);
    board_texture = LoadAssetTexture(BUK_CLIENT_ASSET_BOARD_MAIN);
    piece_texture_a = LoadAssetTexture(BUK_CLIENT_ASSET_PIECE_A_ON_BOARD);
    piece_texture_b = LoadAssetTexture(BUK_CLIENT_ASSET_PIECE_B_ON_BOARD);
    for (size_t result_index = 0U; result_index < BUK_CLIENT_RESULT_COUNT; result_index++) {
        result_textures[result_index] = LoadAssetTexture(
            BUK_CLIENT_ASSET_YUT_RESULT_DO + result_index);
    }

#if defined(PLATFORM_WEB)
    emscripten_set_main_loop(UpdateDrawFrame, 0, 1);
#else
    SetTargetFPS(60);
    while (!WindowShouldClose()) UpdateDrawFrame();
#endif

    if (board_texture.id != 0U) UnloadTexture(board_texture);
    if (piece_texture_a.id != 0U) UnloadTexture(piece_texture_a);
    if (piece_texture_b.id != 0U) UnloadTexture(piece_texture_b);
    for (size_t result_index = 0U; result_index < BUK_CLIENT_RESULT_COUNT; result_index++) {
        if (result_textures[result_index].id != 0U) UnloadTexture(result_textures[result_index]);
    }
    CloseWindow();
    return 0;
}
