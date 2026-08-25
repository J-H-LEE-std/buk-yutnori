ROOT_DIR := $(abspath ..)
BUILD_DIR ?= $(ROOT_DIR)/build/client
NATIVE_CC ?= cc
NATIVE_CFLAGS ?= -std=c11 -Wall -Wextra -Wpedantic -Werror \
	-Werror=implicit-function-declaration -Wmissing-prototypes -Wstrict-prototypes
EMCC ?= emcc
RAYLIB_PATH ?= $(ROOT_DIR)/external/raylib
INCLUDES := -I$(CURDIR)/include
STATE_SOURCE := $(CURDIR)/src/state.c
STATE_HEADER := $(CURDIR)/include/buk_client/state.h
PROTOCOL_STATE_SOURCE := $(CURDIR)/src/protocol_state.c
PROTOCOL_STATE_HEADER := $(CURDIR)/include/buk_client/protocol_state.h
PROTOCOL_BRIDGE_SOURCE := $(CURDIR)/src/protocol_bridge.c
BRIDGE_HEADER := $(CURDIR)/include/buk_client/bridge.h
BOARD_LAYOUT_SOURCE := $(CURDIR)/src/board_layout.c
BOARD_LAYOUT_HEADER := $(CURDIR)/include/buk_client/board_layout.h
BOARD_GRAPH_DATA := $(CURDIR)/include/buk_client/board_graph_data.def
PRESENTATION_STATE_SOURCE := $(CURDIR)/src/presentation_state.c
PRESENTATION_STATE_HEADER := $(CURDIR)/include/buk_client/presentation_state.h
STATE_TEST := $(BUILD_DIR)/state_test
PROTOCOL_STATE_TEST := $(BUILD_DIR)/protocol_state_test
PROTOCOL_BRIDGE_TEST := $(BUILD_DIR)/protocol_bridge_test
BOARD_LAYOUT_TEST := $(BUILD_DIR)/board_layout_test
PRESENTATION_STATE_TEST := $(BUILD_DIR)/presentation_state_test
RAYLIB_WEB_LIB := $(RAYLIB_PATH)/src/libraylib.web.a
WEB_OUTPUT := $(BUILD_DIR)/web/index.html
WEB_SOURCES := $(CURDIR)/src/main.c $(STATE_SOURCE) $(PROTOCOL_STATE_SOURCE) $(PROTOCOL_BRIDGE_SOURCE) $(BOARD_LAYOUT_SOURCE) $(PRESENTATION_STATE_SOURCE)
WEB_CFLAGS := -std=c11 -Os -Wall -Wextra -Wpedantic -Werror \
	-Werror=implicit-function-declaration -Wmissing-prototypes -Wstrict-prototypes \
	-DPLATFORM_WEB
WEB_LDFLAGS := -sUSE_GLFW=3 -sWASM=1 -sASSERTIONS=1 -sENVIRONMENT=web \
	-sEXPORTED_FUNCTIONS=_main,_BukClientSetInput,_BukClientGetInput,_BukClientRenderedBoardNodeCount,_BukClientRenderedBoardEdgeCount,_BukClientRenderedPieceCount,_BukClientProtocolRuntimeInit,_BukClientBeginSynchronization,_BukClientApplySnapshotSequence,_BukClientApplyEventSequence,_BukClientStageSnapshotMetadata,_BukClientStageSnapshotPiece,_BukClientStageSnapshotResult,_BukClientIsCanonicalBoardSpace,_BukClientFailSynchronization,_BukClientCompleteSynchronization,_BukClientCanSendStateCommands,_BukClientRequiresResynchronization,_BukClientLastSequence,_BukClientHasPresentationSnapshot,_BukClientPresentationPieceCount,_BukClientPresentationResultCount,_BukClientPresentationStatus,_BukClientPresentationTurnPhase,_BukClientPresentationRequiredInput,_BukClientPresentationCurrentTeam,_BukClientPresentationRemainingMilliseconds \
	-sEXPORTED_RUNTIME_METHODS=ccall --shell-file $(CURDIR)/web/shell.html

.PHONY: all check-raylib test wasm

all: test

test: $(STATE_TEST) $(PROTOCOL_STATE_TEST) $(PROTOCOL_BRIDGE_TEST) $(BOARD_LAYOUT_TEST) $(PRESENTATION_STATE_TEST)
	$(STATE_TEST)
	$(PROTOCOL_STATE_TEST)
	$(PROTOCOL_BRIDGE_TEST)
	$(BOARD_LAYOUT_TEST)
	$(PRESENTATION_STATE_TEST)

wasm: check-raylib $(WEB_OUTPUT)

check-raylib:
	@test -f "$(RAYLIB_PATH)/src/raylib.h" || \
		{ echo "raylib source not found at $(RAYLIB_PATH)" >&2; exit 1; }

$(STATE_TEST): $(CURDIR)/tests/state_test.c $(STATE_SOURCE) $(STATE_HEADER)
	mkdir -p $(dir $@)
	$(NATIVE_CC) $(NATIVE_CFLAGS) $(INCLUDES) $(CURDIR)/tests/state_test.c $(STATE_SOURCE) -o $@

$(PROTOCOL_STATE_TEST): $(CURDIR)/tests/protocol_state_test.c $(PROTOCOL_STATE_SOURCE) $(PROTOCOL_STATE_HEADER)
	mkdir -p $(dir $@)
	$(NATIVE_CC) $(NATIVE_CFLAGS) $(INCLUDES) $(CURDIR)/tests/protocol_state_test.c $(PROTOCOL_STATE_SOURCE) -o $@

$(PROTOCOL_BRIDGE_TEST): $(CURDIR)/tests/protocol_bridge_test.c $(PROTOCOL_BRIDGE_SOURCE) $(PROTOCOL_STATE_SOURCE) $(PRESENTATION_STATE_SOURCE) $(BOARD_LAYOUT_SOURCE) $(BRIDGE_HEADER) $(PROTOCOL_STATE_HEADER) $(PRESENTATION_STATE_HEADER) $(BOARD_LAYOUT_HEADER)
	mkdir -p $(dir $@)
	$(NATIVE_CC) $(NATIVE_CFLAGS) $(INCLUDES) $(CURDIR)/tests/protocol_bridge_test.c $(PROTOCOL_BRIDGE_SOURCE) $(PROTOCOL_STATE_SOURCE) $(PRESENTATION_STATE_SOURCE) $(BOARD_LAYOUT_SOURCE) -o $@

$(BOARD_LAYOUT_TEST): $(CURDIR)/tests/board_layout_test.c $(BOARD_LAYOUT_SOURCE) $(BOARD_LAYOUT_HEADER) $(BOARD_GRAPH_DATA)
	mkdir -p $(dir $@)
	$(NATIVE_CC) $(NATIVE_CFLAGS) $(INCLUDES) $(CURDIR)/tests/board_layout_test.c $(BOARD_LAYOUT_SOURCE) -o $@

$(PRESENTATION_STATE_TEST): $(CURDIR)/tests/presentation_state_test.c $(PRESENTATION_STATE_SOURCE) $(PRESENTATION_STATE_HEADER) $(BOARD_LAYOUT_HEADER)
	mkdir -p $(dir $@)
	$(NATIVE_CC) $(NATIVE_CFLAGS) $(INCLUDES) $(CURDIR)/tests/presentation_state_test.c $(PRESENTATION_STATE_SOURCE) -o $@

$(RAYLIB_WEB_LIB):
	$(MAKE) -C $(RAYLIB_PATH)/src PLATFORM=PLATFORM_WEB RAYLIB_BUILD_MODE=RELEASE \
		RAYLIB_MODULE_AUDIO=FALSE RAYLIB_MODULE_MODELS=FALSE

$(WEB_OUTPUT): $(WEB_SOURCES) $(STATE_HEADER) $(PROTOCOL_STATE_HEADER) $(BRIDGE_HEADER) $(BOARD_LAYOUT_HEADER) $(BOARD_GRAPH_DATA) $(PRESENTATION_STATE_HEADER) $(CURDIR)/web/shell.html $(RAYLIB_WEB_LIB)
	mkdir -p $(dir $@)
	$(EMCC) $(WEB_CFLAGS) $(INCLUDES) -I$(RAYLIB_PATH)/src $(WEB_SOURCES) \
		$(RAYLIB_WEB_LIB) $(WEB_LDFLAGS) -o $@
