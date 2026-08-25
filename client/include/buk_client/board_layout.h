#ifndef BUK_CLIENT_BOARD_LAYOUT_H
#define BUK_CLIENT_BOARD_LAYOUT_H

#include <stdbool.h>
#include <stddef.h>

#define BUK_CLIENT_LOGICAL_WIDTH 1280.0F
#define BUK_CLIENT_LOGICAL_HEIGHT 720.0F
#define BUK_CLIENT_BOARD_LOGICAL_X 40.0F
#define BUK_CLIENT_BOARD_LOGICAL_Y 40.0F
#define BUK_CLIENT_BOARD_LOGICAL_SIZE 640.0F

typedef enum BukClientBoardNodeId {
#define BUK_CLIENT_BOARD_NODE(symbol, spec_id, normalized_x, normalized_y) \
    BUK_CLIENT_BOARD_NODE_##symbol,
#include "buk_client/board_graph_data.def"
#undef BUK_CLIENT_BOARD_NODE
    BUK_CLIENT_BOARD_NODE_COUNT
} BukClientBoardNodeId;

typedef struct BukClientPoint {
    float x;
    float y;
} BukClientPoint;

typedef struct BukClientRect {
    float x;
    float y;
    float width;
    float height;
} BukClientRect;

typedef struct BukClientBoardNode {
    BukClientBoardNodeId id;
    const char *spec_id;
    float normalized_x;
    float normalized_y;
} BukClientBoardNode;

typedef struct BukClientBoardEdge {
    BukClientBoardNodeId from;
    BukClientBoardNodeId to;
} BukClientBoardEdge;

typedef struct BukClientGameLayout {
    BukClientRect content;
    BukClientRect board;
    float scale;
} BukClientGameLayout;

const BukClientBoardNode *BukClientBoardNodes(size_t *count);
const BukClientBoardEdge *BukClientBoardEdges(size_t *count);
bool BukClientBoardMapNode(BukClientRect viewport, BukClientBoardNodeId node_id,
                           BukClientPoint *point);
bool BukClientCalculateGameLayout(float screen_width, float screen_height,
                                  BukClientGameLayout *layout);

#endif
