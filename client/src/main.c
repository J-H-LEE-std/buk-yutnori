#include "buk_client/bridge.h"
#include "buk_client/state.h"

#include "raylib.h"

#if defined(PLATFORM_WEB)
#include <emscripten/emscripten.h>
#endif

enum {
    SCREEN_WIDTH = 960,
    SCREEN_HEIGHT = 540,
};

static BukClientState client_state;

static void UpdateDrawFrame(void)
{
    BeginDrawing();
    ClearBackground((Color){ 245, 239, 225, 255 });

    DrawText("Buk Yutnori", 48, 48, 42, (Color){ 55, 42, 35, 255 });
    DrawText("raylib / C / WebAssembly", 50, 100, 22, (Color){ 126, 83, 57, 255 });
    DrawRectangleRounded((Rectangle){ 48.0F, 166.0F, 864.0F, 244.0F }, 0.06F, 8,
                         (Color){ 255, 252, 244, 255 });
    DrawText("The browser owns login, sessions, WebSocket transport, and IME input.",
             78, 208, 20, (Color){ 55, 42, 35, 255 });
    DrawText("C/WASM owns presentation state, rendering, and animation.",
             78, 248, 20, (Color){ 55, 42, 35, 255 });
    DrawText(TextFormat("UTF-8 bytes received from DOM: %i",
                        (int)BukClientStateInputLength(&client_state)),
             78, 324, 24, (Color){ 29, 111, 92, 255 });
    DrawText("Type Korean text below the canvas to verify the JS <-> C boundary.",
             48, 454, 20, (Color){ 92, 82, 75, 255 });

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

int main(void)
{
    BukClientStateInit(&client_state);
    BukClientProtocolRuntimeInit();
    SetConfigFlags(FLAG_WINDOW_RESIZABLE);
    InitWindow(SCREEN_WIDTH, SCREEN_HEIGHT, "Buk Yutnori WASM prototype");

#if defined(PLATFORM_WEB)
    emscripten_set_main_loop(UpdateDrawFrame, 0, 1);
#else
    SetTargetFPS(60);
    while (!WindowShouldClose()) UpdateDrawFrame();
#endif

    CloseWindow();
    return 0;
}
