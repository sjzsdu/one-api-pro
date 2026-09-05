SHELL := /bin/sh

APP_NAME ?= one-api-pro
PREFIX ?= $(HOME)/.local
DESTDIR ?=
BINDIR ?= $(PREFIX)/bin
BUILD_DIR ?= dist
GO ?= go

GOOS ?= $(shell $(GO) env GOOS)
GOARCH ?= $(shell $(GO) env GOARCH)
CGO_ENABLED ?= 1

VERSION ?= $(shell if [ -f VERSION ]; then tr -d '[:space:]' < VERSION; else git describe --tags --always 2>/dev/null || echo dev; fi)
LDFLAGS := -s -w -X github.com/modelbus/one-api-pro/common.Version=$(VERSION)
TARGET := $(BUILD_DIR)/$(APP_NAME)

.PHONY: all help build build-frontend build-backend check-install-permission install clean

all: build

help:
	@printf '%s\n' \
	  '可用目标:' \
	  '  make build          构建前端和当前平台后端，产物位于 dist/one-api-pro' \
	  '  make install        构建后安装到 $(DESTDIR)$(BINDIR)/one-api-pro' \
	  '  make clean          删除本地构建产物' \
	  '' \
	  '可覆盖变量:' \
	  '  PREFIX=/usr/local   安装前缀（默认 $(HOME)/.local）' \
	  '  DESTDIR=/tmp/stage  打包 staging 根目录' \
	  '  VERSION=v1.0.0      注入到后端的版本号' \
	  '  GOOS=linux GOARCH=amd64  交叉构建目标平台'

build: build-frontend build-backend

build-frontend:
	@command -v npm >/dev/null 2>&1 || { echo '错误: 未找到 npm，请先安装 Node.js/npm' >&2; exit 1; }
	@echo '==> 构建前端主题'
	@cd web && sh build.sh

build-backend:
	@command -v $(GO) >/dev/null 2>&1 || { echo '错误: 未找到 Go，请先安装 Go' >&2; exit 1; }
	@mkdir -p $(BUILD_DIR)
	@echo '==> 构建 $(TARGET) ($(GOOS)/$(GOARCH), version $(VERSION))'
	@CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(TARGET) .

check-install-permission:
	@target='$(DESTDIR)$(BINDIR)'; \
	probe="$$target"; \
	while [ ! -d "$$probe" ] && [ "$$probe" != '/' ]; do probe=$$(dirname "$$probe"); done; \
	if [ ! -d "$$probe" ] || [ ! -w "$$probe" ]; then \
		echo "错误: 没有权限写入 $(DESTDIR)$(BINDIR)" >&2; \
		echo "请使用 sudo make install，或指定用户可写目录，例如: make install PREFIX=\$$HOME/.local" >&2; \
		exit 1; \
	fi

install: check-install-permission build
	@mkdir -p '$(DESTDIR)$(BINDIR)'
	@install -m 0755 '$(TARGET)' '$(DESTDIR)$(BINDIR)/$(APP_NAME)'
	@echo '==> 已安装 $(DESTDIR)$(BINDIR)/$(APP_NAME)'

clean:
	@rm -rf '$(BUILD_DIR)'
	@rm -rf web/build
