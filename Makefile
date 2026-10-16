.PHONY: all build-amd64 build-arm64 build-linux-amd64 build-linux-arm64 build-windows build-windows-amd64 clean

all: build-linux-amd64 build-linux-arm64 build-windows-amd64

build-linux-amd64:
	mkdir -p dist
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 CC=gcc go build -buildmode=c-shared -ldflags="-s -w" -o dist/cpa-freetier-plugin_linux_amd64.so main.go

build-amd64: build-linux-amd64

build-linux-arm64:
	mkdir -p dist
	CGO_ENABLED=1 GOOS=linux GOARCH=arm64 CC=aarch64-linux-gnu-gcc go build -buildmode=c-shared -ldflags="-s -w" -o dist/cpa-freetier-plugin_linux_arm64.so main.go

build-arm64: build-linux-arm64

build-windows-amd64:
	mkdir -p dist
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc go build -buildmode=c-shared -ldflags="-s -w" -o dist/cpa-freetier-plugin_windows_amd64.dll main.go

build-windows: build-windows-amd64

clean:
	rm -rf dist/
