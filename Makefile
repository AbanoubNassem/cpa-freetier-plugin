.PHONY: all build-amd64 build-arm64 build-linux-amd64 build-linux-arm64 build-windows build-windows-amd64 test vet check-models verify clean

# Single source of truth for the version: the release tag (or the nearest tag).
# It is injected into the binary, so main.go/plugin.json/README no longer drift.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//' || echo dev)
LDFLAGS := -s -w -X main.pluginVersion=$(VERSION)

all: build-linux-amd64 build-linux-arm64 build-windows-amd64

build: build-linux-amd64

build-linux-amd64:
	mkdir -p dist
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 CC=gcc go build -buildmode=c-shared -trimpath -ldflags="$(LDFLAGS)" -o dist/cpa-freetier-plugin_linux_amd64.so main.go

build-amd64: build-linux-amd64

build-linux-arm64:
	mkdir -p dist
	CGO_ENABLED=1 GOOS=linux GOARCH=arm64 CC=aarch64-linux-gnu-gcc go build -buildmode=c-shared -trimpath -ldflags="$(LDFLAGS)" -o dist/cpa-freetier-plugin_linux_arm64.so main.go

build-arm64: build-linux-arm64

build-windows-amd64:
	mkdir -p dist
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc go build -buildmode=c-shared -trimpath -ldflags="$(LDFLAGS)" -o dist/cpa-freetier-plugin_windows_amd64.dll main.go

build-windows: build-windows-amd64

# Unit tests cover the fragile parts: the payload shape the free-tier gate
# accepts, SSE assembly, model health tracking and log bounding.
test:
	CGO_ENABLED=1 go test ./...

vet:
	CGO_ENABLED=1 go vet ./...

# Fails when the plugin advertises a model the upstream catalogue dropped.
check-models:
	./scripts/check-models.sh main.go

# Calls every advertised model for real. Catalogue presence is not liveness:
# an id can stay itemised while the provider answers 404. Needs residential
# egress for real verdicts — the free tier rejects datacenter ASNs.
#
#   make probe-models PROBE_PROXY=http://127.0.0.1:8127
probe-models:
	./scripts/probe-models.sh main.go

verify: vet test check-models

clean:
	rm -rf dist/
