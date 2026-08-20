# Termux NAS 构建入口
# 本机:  make build        (构建到 ../bin/,即部署根 bin/)
# Termux: make android     (android/arm64 静态交叉编译)
# 前端:  make web          (Vite 构建 → internal/webui/dist,go:embed 打包进 nasd)

VERSION ?= 0.1.0
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS := -s -w \
  -X github.com/termux-nas/nas/internal/version.Version=$(VERSION) \
  -X github.com/termux-nas/nas/internal/version.Commit=$(COMMIT) \
  -X github.com/termux-nas/nas/internal/version.BuildTime=$(BUILD_TIME)

# Windows 需要 .exe 后缀才能被 exec 启动
ifeq ($(OS),Windows_NT)
BINEXT := .exe
else
BINEXT :=
endif

.PHONY: all build web android test tidy clean

all: build

web:
	@echo "==> 构建前端(Vite → internal/webui/dist)"
	@npm --prefix web ci
	@npm --prefix web run build

build: web
	@mkdir -p ../bin
	@rm -f ../bin/nasm ../bin/nasd ../bin/nasm.exe ../bin/nasd.exe
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o ../bin/nasm$(BINEXT) ./cmd/nasm
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o ../bin/nasd$(BINEXT) ./cmd/nasd
	@echo "==> 构建完成:"
	@ls -lh ../bin/nasm$(BINEXT) ../bin/nasd$(BINEXT)

android: web
	@mkdir -p ../bin
	CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o ../bin/nasm ./cmd/nasm
	CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o ../bin/nasd ./cmd/nasd
	@echo "==> Termux(android/arm64)构建完成:"
	@ls -lh ../bin/nasm ../bin/nasd

test:
	go vet ./...
	go test ./...

tidy:
	go mod tidy

clean:
	rm -f ../bin/nasm ../bin/nasd
