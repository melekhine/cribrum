APP_NAME  = cribrum
MODULE    = github.com/melekhine/cribrum
PKG_PATH  = ./cmd/cribrum

GIT_HASH  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
GIT_TIME  := $(shell date -u "+%FT%TZ")
GIT_TAG   := $(shell git tag --points-at HEAD 2>/dev/null)
ifeq ($(GIT_TAG),)
	GIT_TAG := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
endif

# -X needs the full import path of the package, not just its name.
BUILD_FLAGS := -X $(MODULE).GitHash=$(GIT_HASH) -X $(MODULE).BuildTime=$(GIT_TIME) -X $(MODULE).Version=$(GIT_TAG)
LDFLAGS     := -s -w $(BUILD_FLAGS)
GOBUILD     := CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="$(LDFLAGS)"

.PHONY: all test test-netlink vet armv7 arm64 amd64 clean

all: vet test armv7 arm64 amd64

vet:
	go vet ./...
	GOOS=linux go vet ./...

test:
	go test -race ./...

# Kernel integration tests (Linux, root): run inside a throwaway netns.
test-netlink:
	go test -c -race -o build/$(APP_NAME).test .
	sudo build/$(APP_NAME).test -test.run 'Netlink' -test.v

# Asus ZenWiFi AX (XT8) — ARMv7
armv7:
	GOARCH=arm GOARM=7 $(GOBUILD) -o build/$(APP_NAME)-armv7 $(PKG_PATH)

# MikroTik CCR2004 / ARM64 Linux — ARM64
arm64:
	GOARCH=arm64 $(GOBUILD) -o build/$(APP_NAME)-arm64 $(PKG_PATH)

# Generic Linux server — x86_64
amd64:
	GOARCH=amd64 $(GOBUILD) -o build/$(APP_NAME)-amd64 $(PKG_PATH)

clean:
	rm -rf build/
