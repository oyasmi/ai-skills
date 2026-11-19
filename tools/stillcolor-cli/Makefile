PREFIX ?= $(HOME)/.local
CFLAGS ?= -O2
BUILD_DIR ?= .build
WARNINGS = -std=gnu11 -Wall -Wextra
SOURCES = src/main.c
HEADERS = src/stillcolor.h
BINARY = $(BUILD_DIR)/stillcolor-cli
TEST_BINARY = $(BUILD_DIR)/stillcolor-cli-test

.PHONY: all install test clean

all: $(BINARY)

$(BUILD_DIR):
	mkdir -p "$@"

$(BINARY): $(SOURCES) src/macos.c $(HEADERS) Makefile | $(BUILD_DIR)
	xcrun --sdk macosx clang $(CPPFLAGS) $(WARNINGS) $(CFLAGS) \
		-arch arm64 -mmacosx-version-min=13.0 $(SOURCES) src/macos.c \
		-framework CoreFoundation -framework CoreGraphics -framework IOKit $(LDFLAGS) -o "$@"

install: $(BINARY)
	install -d "$(DESTDIR)$(PREFIX)/bin"
	install -m 755 "$(BINARY)" "$(DESTDIR)$(PREFIX)/bin/stillcolor-cli"

$(TEST_BINARY): $(SOURCES) tests/fake-macos.c $(HEADERS) Makefile | $(BUILD_DIR)
	$(CC) $(CPPFLAGS) $(WARNINGS) $(CFLAGS) -Isrc $(SOURCES) tests/fake-macos.c $(LDFLAGS) -o "$@"

test: $(TEST_BINARY)
	sh tests/cli.sh "$(TEST_BINARY)"

clean:
	$(RM) "$(BINARY)" "$(TEST_BINARY)"
