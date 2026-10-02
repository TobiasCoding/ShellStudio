GO ?= go
export TMPDIR := $(CURDIR)/.work/tmp
VERSION ?= $(shell cat VERSION)
SOURCE_DATE_EPOCH ?= 0

.PHONY: all build test race integration check release preview publish clean
all: build
$(TMPDIR):
	mkdir -p $@
build: | $(TMPDIR)
	mkdir -p bin
	$(GO) build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/shellstudio ./cmd/shellstudio
test: build
	$(GO) test ./...
race: build
	$(GO) test -race ./...
integration: build
	SHELLSTUDIO_INTEGRATION=1 $(GO) test ./internal/extensions -run TestOffline -count=1
	python3 tests/terminal_integration.py
check: test race integration
	$(GO) vet ./...
	python3 tests/installer_integration.py
	python3 tests/release_integration.py
	python3 scripts/publication_check.py
release: | $(TMPDIR)
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o dist/shellstudio-linux-amd64 ./cmd/shellstudio
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o dist/shellstudio-linux-arm64 ./cmd/shellstudio
	python3 scripts/package.py --version "$(VERSION)" --epoch "$(SOURCE_DATE_EPOCH)"
preview:
	python3 scripts/release.py preview
publish:
	python3 scripts/release.py publish
clean:
	rm -rf bin dist .work
