#include "buk_client/board_layout.h"
#include "buk_client/bridge.h"
#include "buk_client/state.h"

#include "raylib.h"

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
    DrawRectangleRounded(RaylibRectangle(layout->board), 0.04F, 12, board_background);
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

static void DrawPrototypeHud(const BukClientGameLayout *layout)
{
    const Color panel = { 255, 252, 244, 255 };
    const Color ink = { 55, 42, 35, 255 };
    const Color muted = { 126, 104, 88, 255 };
    const Color accent = { 29, 111, 92, 255 };
    const BukClientRect hud = LogicalRectangle(layout, 720.0F, 40.0F, 520.0F, 640.0F);
    const BukClientRect status = LogicalRectangle(layout, 752.0F, 150.0F, 456.0F, 154.0F);
    const BukClientRect queue = LogicalRectangle(layout, 752.0F, 328.0F, 456.0F, 154.0F);
    const BukClientRect command = LogicalRectangle(layout, 752.0F, 522.0F, 456.0F, 110.0F);
    int body_size = (int)(20.0F * layout->scale);
    int heading_size = (int)(30.0F * layout->scale);

    if (body_size < 8) body_size = 8;
    if (heading_size < 10) heading_size = 10;

    DrawRectangleRounded(RaylibRectangle(hud), 0.04F, 10, panel);
    DrawText("BUK YUTNORI", (int)(hud.x + (32.0F * layout->scale)),
             (int)(hud.y + (28.0F * layout->scale)), heading_size, ink);
    DrawText("canonical board presentation", (int)(hud.x + (32.0F * layout->scale)),
             (int)(hud.y + (70.0F * layout->scale)), body_size, muted);

    DrawRectangleRounded(RaylibRectangle(status), 0.06F, 8,
                         (Color){ 236, 244, 239, 255 });
    DrawText("BOARD", (int)(status.x + (24.0F * layout->scale)),
             (int)(status.y + (22.0F * layout->scale)), body_size, accent);
    DrawText("29 nodes / 32 forward edges", (int)(status.x + (24.0F * layout->scale)),
             (int)(status.y + (62.0F * layout->scale)), body_size, ink);
    DrawText("server state is display-only here", (int)(status.x + (24.0F * layout->scale)),
             (int)(status.y + (100.0F * layout->scale)), body_size, muted);

    DrawRectangleRounded(RaylibRectangle(queue), 0.06F, 8,
                         (Color){ 247, 239, 222, 255 });
    DrawText("RESULT QUEUE", (int)(queue.x + (24.0F * layout->scale)),
             (int)(queue.y + (22.0F * layout->scale)), body_size, muted);
    DrawText("Waiting for authoritative snapshot", (int)(queue.x + (24.0F * layout->scale)),
             (int)(queue.y + (66.0F * layout->scale)), body_size, ink);

    DrawRectangleRounded(RaylibRectangle(command), 0.06F, 8,
                         (Color){ 67, 50, 38, 255 });
    DrawText("PRESENTATION SKELETON", (int)(command.x + (24.0F * layout->scale)),
             (int)(command.y + (22.0F * layout->scale)), body_size,
             (Color){ 255, 249, 231, 255 });
    DrawText(TextFormat("DOM UTF-8 bytes: %i",
                        (int)BukClientStateInputLength(&client_state)),
             (int)(command.x + (24.0F * layout->scale)),
             (int)(command.y + (62.0F * layout->scale)), body_size,
             (Color){ 213, 232, 222, 255 });
}

static void UpdateDrawFrame(void)
{
    BukClientGameLayout layout;

    BeginDrawing();
    ClearBackground((Color){ 36, 29, 25, 255 });
    if (BukClientCalculateGameLayout((float)GetScreenWidth(), (float)GetScreenHeight(),
                                     &layout)) {
        DrawRectangleRec(RaylibRectangle(layout.content),
                         (Color){ 245, 239, 225, 255 });
        DrawCanonicalBoard(&layout);
        DrawPrototypeHud(&layout);
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

int main(void)
{
    BukClientStateInit(&client_state);
    BukClientProtocolRuntimeInit();
#if !defined(PLATFORM_WEB)
    SetConfigFlags(FLAG_WINDOW_RESIZABLE);
#endif
    InitWindow(SCREEN_WIDTH, SCREEN_HEIGHT, "Buk Yutnori board prototype");

#if defined(PLATFORM_WEB)
    emscripten_set_main_loop(UpdateDrawFrame, 0, 1);
#else
    SetTargetFPS(60);
    while (!WindowShouldClose()) UpdateDrawFrame();
#endif

    CloseWindow();
    return 0;
}
