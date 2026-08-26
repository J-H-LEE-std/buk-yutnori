#include "buk_client/board_layout.h"

#include <math.h>
#include <string.h>

static const BukClientBoardNode board_nodes[] = {
#define BUK_CLIENT_BOARD_NODE(symbol, spec_id_value, x, y) \
    { BUK_CLIENT_BOARD_NODE_##symbol, spec_id_value, x, y },
#include "buk_client/board_graph_data.def"
#undef BUK_CLIENT_BOARD_NODE
};

static const BukClientBoardEdge board_edges[] = {
#define BUK_CLIENT_BOARD_EDGE(from_symbol, to_symbol) \
    { BUK_CLIENT_BOARD_NODE_##from_symbol, BUK_CLIENT_BOARD_NODE_##to_symbol },
#include "buk_client/board_graph_data.def"
#undef BUK_CLIENT_BOARD_EDGE
};

static const BukClientBoardRouteChoice board_route_choices[] = {
#define BUK_CLIENT_BOARD_ROUTE_CHOICE(origin_symbol, normal_symbol, shortcut_symbol) \
    { BUK_CLIENT_BOARD_NODE_##origin_symbol, BUK_CLIENT_BOARD_NODE_##normal_symbol, \
      BUK_CLIENT_BOARD_NODE_##shortcut_symbol },
#include "buk_client/board_graph_data.def"
#undef BUK_CLIENT_BOARD_ROUTE_CHOICE
};

_Static_assert(
    (sizeof(board_nodes) / sizeof(board_nodes[0])) == BUK_CLIENT_BOARD_NODE_COUNT,
    "board node table must contain every node enum");

const BukClientBoardNode *BukClientBoardNodes(size_t *count)
{
    if (count != NULL) *count = sizeof(board_nodes) / sizeof(board_nodes[0]);
    return board_nodes;
}

const BukClientBoardEdge *BukClientBoardEdges(size_t *count)
{
    if (count != NULL) *count = sizeof(board_edges) / sizeof(board_edges[0]);
    return board_edges;
}

const BukClientBoardRouteChoice *BukClientBoardRouteChoices(size_t *count)
{
    if (count != NULL) {
        *count = sizeof(board_route_choices) / sizeof(board_route_choices[0]);
    }
    return board_route_choices;
}

bool BukClientBoardFindNode(const char *spec_id, BukClientBoardNodeId *node_id)
{
    size_t index;

    if ((spec_id == NULL) || (node_id == NULL) || (spec_id[0] == '\0')) return false;
    for (index = 0U; index < sizeof(board_nodes) / sizeof(board_nodes[0]); index++) {
        if (strcmp(spec_id, board_nodes[index].spec_id) == 0) {
            *node_id = board_nodes[index].id;
            return true;
        }
    }
    return false;
}

bool BukClientBoardRouteTargets(BukClientBoardNodeId origin,
                                BukClientBoardNodeId *normal,
                                BukClientBoardNodeId *shortcut)
{
    size_t index;

    if ((normal == NULL) || (shortcut == NULL)) return false;
    for (index = 0U;
         index < sizeof(board_route_choices) / sizeof(board_route_choices[0]);
         index++) {
        if (board_route_choices[index].origin == origin) {
            *normal = board_route_choices[index].normal;
            *shortcut = board_route_choices[index].shortcut;
            return true;
        }
    }
    return false;
}

bool BukClientBoardMapNode(BukClientRect viewport, BukClientBoardNodeId node_id,
                           BukClientPoint *point)
{
    const BukClientBoardNode *node;

    if ((point == NULL) || ((int)node_id < 0) ||
        (node_id >= BUK_CLIENT_BOARD_NODE_COUNT)) {
        return false;
    }

    node = &board_nodes[node_id];
    point->x = viewport.x + (node->normalized_x * viewport.width);
    point->y = viewport.y + (node->normalized_y * viewport.height);
    return true;
}

bool BukClientCalculateGameLayout(float screen_width, float screen_height,
                                  BukClientGameLayout *layout)
{
    float height_scale;
    float scale;

    if (layout == NULL) return false;
    *layout = (BukClientGameLayout){ 0 };

    if (!isfinite(screen_width) || !isfinite(screen_height) ||
        !(screen_width > 0.0F) || !(screen_height > 0.0F)) {
        return false;
    }

    scale = screen_width / BUK_CLIENT_LOGICAL_WIDTH;
    height_scale = screen_height / BUK_CLIENT_LOGICAL_HEIGHT;
    if (height_scale < scale) scale = height_scale;
    layout->scale = scale;
    layout->content.width = BUK_CLIENT_LOGICAL_WIDTH * scale;
    layout->content.height = BUK_CLIENT_LOGICAL_HEIGHT * scale;
    layout->content.x = (screen_width - layout->content.width) * 0.5F;
    layout->content.y = (screen_height - layout->content.height) * 0.5F;
    layout->board.x = layout->content.x + (BUK_CLIENT_BOARD_LOGICAL_X * scale);
    layout->board.y = layout->content.y + (BUK_CLIENT_BOARD_LOGICAL_Y * scale);
    layout->board.width = BUK_CLIENT_BOARD_LOGICAL_SIZE * scale;
    layout->board.height = BUK_CLIENT_BOARD_LOGICAL_SIZE * scale;
    return true;
}
