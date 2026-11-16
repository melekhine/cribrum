APP_NAME  = cribrum
PKG_PATH  = ./cmd/cribrum

GIT_HASH  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
GIT_TIME  := $(shell date "+%FT%T%Z")
GIT_TAG   := $(shell git tag --points-at HEAD 2>/dev/null)
ifeq ($(GIT_TAG),)
	GIT_TAG := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
endif

BUILD_FLAGS := -X cribrum.GitHash=$(GIT_HASH) -X cribrum.BuildTime=$(GIT_TIME) -X cribrum.Version=$(GIT_TAG)
LDFLAGS     := -s -w $(BUILD_FLAGS)

.PHONY: all test armv7 arm64 amd64 clean

all: test armv7 arm64 amd64

test:
	go test -v ./...

# Asus ZenWiFi AX (XT8) — ARMv7
armv7:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -ldflags="$(LDFLAGS)" -o build/$(APP_NAME)-armv7 $(PKG_PATH)

# MikroTik CCR2004 / ARM64 Linux — ARM64
arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o build/$(APP_NAME)-arm64 $(PKG_PATH)

# Стандартный сервер Linux — x86_64
amd64:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o build/$(APP_NAME)-amd64 $(PKG_PATH)

clean:
	rm -rf build/
