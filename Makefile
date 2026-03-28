MODULE   := github.com/etch-tool/etch
MAIN     := ./cmd/etch
BINARY   := etch
DIST     := dist

VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS  := -s -w -X main.version=$(VERSION)

PLATFORMS := \
	linux/amd64 \
	linux/arm64 \
	darwin/amd64 \
	darwin/arm64 \
	windows/amd64

.PHONY: build build-all test clean

## build: Build for the current platform
build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(MAIN)

## build-all: Build for all supported platforms
build-all: clean
	@for platform in $(PLATFORMS); do \
		OS=$${platform%/*}; \
		ARCH=$${platform#*/}; \
		EXT=""; \
		if [ "$$OS" = "windows" ]; then EXT=".exe"; fi; \
		OUT=$(DIST)/$(BINARY)-$${OS}-$${ARCH}$${EXT}; \
		echo "Building $$OUT"; \
		CGO_ENABLED=0 GOOS=$$OS GOARCH=$$ARCH \
			go build -ldflags "$(LDFLAGS)" -o $$OUT $(MAIN); \
	done

## test: Run all tests
test:
	go test ./...

## clean: Remove build artifacts
clean:
	rm -rf $(DIST)
	rm -f $(BINARY)
