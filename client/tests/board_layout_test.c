#include "buk_client/board_layout.h"

#include <assert.h>
#include <math.h>
#include <stdbool.h>
#include <stddef.h>
#include <stdio.h>
#include <string.h>

static bool NearlyEqual(float left, float right)
{
    return fabsf(left - right) < 0.001F;
}

static void TestCanonicalGraphShape(void)
{
    const BukClientBoardNode *nodes;
    const BukClientBoardEdge *edges;
    size_t node_count;
    size_t edge_count;
    size_t node_index;
    bool seen[BUK_CLIENT_BOARD_NODE_COUNT] = { false };

    nodes = BukClientBoardNodes(&node_count);
    edges = BukClientBoardEdges(&edge_count);

    assert(nodes != NULL);
    assert(edges != NULL);
    assert(node_count == 29U);
    assert(edge_count == 32U);

    for (node_index = 0U; node_index < node_count; node_index++) {
        const BukClientBoardNode node = nodes[node_index];

        assert(node.id >= 0);
        assert(node.id < BUK_CLIENT_BOARD_NODE_COUNT);
        assert(!seen[node.id]);
        assert(node.spec_id != NULL);
        assert(node.spec_id[0] != '\0');
        assert(node.normalized_x >= 0.0F);
        assert(node.normalized_x <= 1.0F);
        assert(node.normalized_y >= 0.0F);
        assert(node.normalized_y <= 1.0F);
        seen[node.id] = true;
    }

    for (node_index = 0U; node_index < BUK_CLIENT_BOARD_NODE_COUNT; node_index++) {
        assert(seen[node_index]);
    }
}

static void TestCoordinateMapping(void)
{
    const BukClientRect viewport = { 100.0F, 50.0F, 600.0F, 600.0F };
    BukClientPoint point = { 0.0F, 0.0F };

    assert(BukClientBoardMapNode(viewport, BUK_CLIENT_BOARD_NODE_CHAMMEOGI, &point));
    assert(NearlyEqual(point.x, 400.0F));
    assert(NearlyEqual(point.y, 98.0F));

    assert(BukClientBoardMapNode(viewport, BUK_CLIENT_BOARD_NODE_BANG, &point));
    assert(NearlyEqual(point.x, 400.0F));
    assert(NearlyEqual(point.y, 362.0F));

    assert(!BukClientBoardMapNode(viewport, BUK_CLIENT_BOARD_NODE_COUNT, &point));
    assert(!BukClientBoardMapNode(viewport, BUK_CLIENT_BOARD_NODE_DO, NULL));
}

static void TestCanonicalSpecIDLookup(void)
{
    BukClientBoardNodeId node_id = BUK_CLIENT_BOARD_NODE_COUNT;

    assert(BukClientBoardFindNode("chammeogi", &node_id));
    assert(node_id == BUK_CLIENT_BOARD_NODE_CHAMMEOGI);
    assert(BukClientBoardFindNode("bang", &node_id));
    assert(node_id == BUK_CLIENT_BOARD_NODE_BANG);
    assert(!BukClientBoardFindNode("unknown-space", &node_id));
    assert(!BukClientBoardFindNode("", &node_id));
    assert(!BukClientBoardFindNode(NULL, &node_id));
    assert(!BukClientBoardFindNode("do", NULL));
}

static void TestCanonicalRouteChoiceTargets(void)
{
    BukClientBoardNodeId normal = BUK_CLIENT_BOARD_NODE_COUNT;
    BukClientBoardNodeId shortcut = BUK_CLIENT_BOARD_NODE_COUNT;

    assert(BukClientBoardRouteTargets(BUK_CLIENT_BOARD_NODE_MO, &normal, &shortcut));
    assert(normal == BUK_CLIENT_BOARD_NODE_BACK_DO);
    assert(shortcut == BUK_CLIENT_BOARD_NODE_MO_DO);
    assert(BukClientBoardRouteTargets(BUK_CLIENT_BOARD_NODE_BACK_MO, &normal,
                                      &shortcut));
    assert(normal == BUK_CLIENT_BOARD_NODE_JJI_DO);
    assert(shortcut == BUK_CLIENT_BOARD_NODE_BACK_MO_DO);
    assert(BukClientBoardRouteTargets(BUK_CLIENT_BOARD_NODE_BANG, &normal, &shortcut));
    assert(normal == BUK_CLIENT_BOARD_NODE_SOK_YUT);
    assert(shortcut == BUK_CLIENT_BOARD_NODE_BANGSUGI);
    assert(!BukClientBoardRouteTargets(BUK_CLIENT_BOARD_NODE_DO, &normal, &shortcut));
    assert(!BukClientBoardRouteTargets(BUK_CLIENT_BOARD_NODE_MO, NULL, &shortcut));
}

static void TestLogicalLayout(void)
{
    BukClientGameLayout layout;

    assert(BukClientCalculateGameLayout(1280.0F, 720.0F, &layout));
    assert(NearlyEqual(layout.scale, 1.0F));
    assert(NearlyEqual(layout.content.x, 0.0F));
    assert(NearlyEqual(layout.content.y, 0.0F));
    assert(NearlyEqual(layout.content.width, 1280.0F));
    assert(NearlyEqual(layout.content.height, 720.0F));
    assert(NearlyEqual(layout.board.x, 40.0F));
    assert(NearlyEqual(layout.board.y, 40.0F));
    assert(NearlyEqual(layout.board.width, 640.0F));
    assert(NearlyEqual(layout.board.height, 640.0F));

    assert(BukClientCalculateGameLayout(1920.0F, 1080.0F, &layout));
    assert(NearlyEqual(layout.scale, 1.5F));
    assert(NearlyEqual(layout.board.x, 60.0F));
    assert(NearlyEqual(layout.board.y, 60.0F));
    assert(NearlyEqual(layout.board.width, 960.0F));

    assert(BukClientCalculateGameLayout(360.0F, 640.0F, &layout));
    assert(NearlyEqual(layout.scale, 0.28125F));
    assert(NearlyEqual(layout.content.x, 0.0F));
    assert(NearlyEqual(layout.content.y, 218.75F));
    assert(layout.board.x >= 0.0F);
    assert(layout.board.y >= 0.0F);
    assert(layout.board.x + layout.board.width <= 360.0F);
    assert(layout.board.y + layout.board.height <= 640.0F);
    assert(NearlyEqual(layout.board.width, layout.board.height));
}

static void TestInvalidLayoutInputs(void)
{
    BukClientGameLayout layout;

    memset(&layout, 0x7f, sizeof(layout));
    assert(!BukClientCalculateGameLayout(0.0F, 720.0F, &layout));
    assert(NearlyEqual(layout.scale, 0.0F));
    assert(!BukClientCalculateGameLayout(1280.0F, -1.0F, &layout));
    assert(!BukClientCalculateGameLayout(1280.0F, 720.0F, NULL));
}

int main(void)
{
    TestCanonicalGraphShape();
    TestCoordinateMapping();
    TestCanonicalSpecIDLookup();
    TestCanonicalRouteChoiceTargets();
    TestLogicalLayout();
    TestInvalidLayoutInputs();
    puts("board_layout_test: ok");
    return 0;
}
