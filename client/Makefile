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
BRIDGE_HEADER := $(CURDIR)/include/buk_client/bridge.h
STATE_TEST := $(BUILD_DIR)/state_test
RAYLIB_WEB_LIB := $(RAYLIB_PATH)/src/libraylib.web.a
WEB_OUTPUT := $(BUILD_DIR)/web/index.html
WEB_SOURCES := $(CURDIR)/src/main.c $(STATE_SOURCE)
WEB_CFLAGS := -std=c11 -Os -Wall -Wextra -Wpedantic -Werror \
	-Werror=implicit-function-declaration -Wmissing-prototypes -Wstrict-prototypes \
	-DPLATFORM_WEB
WEB_LDFLAGS := -sUSE_GLFW=3 -sWASM=1 -sASSERTIONS=1 -sENVIRONMENT=web \
	-sEXPORTED_FUNCTIONS=_main,_BukClientSetInput,_BukClientGetInput \
	-sEXPORTED_RUNTIME_METHODS=ccall --shell-file $(CURDIR)/web/shell.html

.PHONY: all check-raylib test wasm

all: test

test: $(STATE_TEST)
	$(STATE_TEST)

wasm: check-raylib $(WEB_OUTPUT)

check-raylib:
	@test -f "$(RAYLIB_PATH)/src/raylib.h" || \
		{ echo "raylib source not found at $(RAYLIB_PATH)" >&2; exit 1; }

$(STATE_TEST): $(CURDIR)/tests/state_test.c $(STATE_SOURCE) $(STATE_HEADER)
	mkdir -p $(dir $@)
	$(NATIVE_CC) $(NATIVE_CFLAGS) $(INCLUDES) $(CURDIR)/tests/state_test.c $(STATE_SOURCE) -o $@

$(RAYLIB_WEB_LIB):
	$(MAKE) -C $(RAYLIB_PATH)/src PLATFORM=PLATFORM_WEB RAYLIB_BUILD_MODE=RELEASE \
		RAYLIB_MODULE_AUDIO=FALSE RAYLIB_MODULE_MODELS=FALSE

$(WEB_OUTPUT): $(WEB_SOURCES) $(STATE_HEADER) $(BRIDGE_HEADER) $(CURDIR)/web/shell.html $(RAYLIB_WEB_LIB)
	mkdir -p $(dir $@)
	$(EMCC) $(WEB_CFLAGS) $(INCLUDES) -I$(RAYLIB_PATH)/src $(WEB_SOURCES) \
		$(RAYLIB_WEB_LIB) $(WEB_LDFLAGS) -o $@
