APP_NAME := KBRD Agent.app
APP_DIR := dist/$(APP_NAME)
MACOS_DIR := $(APP_DIR)/Contents/MacOS
GO ?= $(shell command -v go 2>/dev/null || printf /usr/local/go/bin/go)
MACOS_ARCH ?= arm64
MACOS_HOST ?= mac
KBRD_API_URL ?= http://kbrd.local:8081
REMOTE_STAGE := Library/Caches/com.ubikyo.kbrd-agent

.PHONY: test build-macos build-macos-universal deploy deploy-macos clean

test:
	$(GO) test ./...

build-macos: clean
	mkdir -p "$(MACOS_DIR)"
	GOOS=darwin GOARCH=$(MACOS_ARCH) CGO_ENABLED=0 $(GO) build -o "$(MACOS_DIR)/kbrd-agent" ./src/cmd/kbrd-agent
	cp packaging/macos/Info.plist "$(APP_DIR)/Contents/Info.plist"

build-macos-universal: clean
	mkdir -p "$(MACOS_DIR)"
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 $(GO) build -o dist/kbrd-agent-arm64 ./src/cmd/kbrd-agent
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 $(GO) build -o dist/kbrd-agent-amd64 ./src/cmd/kbrd-agent
	lipo -create -output "$(MACOS_DIR)/kbrd-agent" dist/kbrd-agent-arm64 dist/kbrd-agent-amd64
	cp packaging/macos/Info.plist "$(APP_DIR)/Contents/Info.plist"
	rm dist/kbrd-agent-arm64 dist/kbrd-agent-amd64

deploy-macos: build-macos
	ssh $(MACOS_HOST) 'mkdir -p "$(REMOTE_STAGE)/$(APP_NAME)"'
	rsync -az --delete "$(APP_DIR)/" '$(MACOS_HOST):$(REMOTE_STAGE)/$(APP_NAME)/'
	rsync -az packaging/macos/com.ubikyo.kbrd-agent.plist scripts/install-macos.sh '$(MACOS_HOST):$(REMOTE_STAGE)/'
	ssh $(MACOS_HOST) 'sh "$(REMOTE_STAGE)/install-macos.sh" "$(REMOTE_STAGE)/$(APP_NAME)" "$(KBRD_API_URL)"'

deploy: deploy-macos

clean:
	rm -rf dist
