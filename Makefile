BINARY := peaceclaude
INSTALL_BIN := $(HOME)/.local/bin/$(BINARY)
ALIAS_BIN := $(HOME)/.local/bin/pclaude
VERSION := $(shell cat VERSION 2>/dev/null || echo 0.1.0)
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)
LDFLAGS := -s -w -X peaceclaude/internal/version.Version=$(VERSION) -X peaceclaude/internal/version.Commit=$(COMMIT)

.PHONY: all build test test-race install regression package dist-clean clean

all: build

build:
	@mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/peaceclaude

test:
	go test -v ./...

test-race:
	go test -race ./...

install: build
	@mkdir -p $(HOME)/.local/bin
	cp bin/$(BINARY) $(INSTALL_BIN)
	chmod 755 $(INSTALL_BIN)
	ln -sf $(BINARY) $(ALIAS_BIN)
	$(INSTALL_BIN) --version

regression: build
	./scripts/regression-test.sh

package:
	./scripts/package-release.sh

dist-clean:
	rm -rf dist

clean:
	rm -rf bin
