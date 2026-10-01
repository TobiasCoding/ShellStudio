GO ?= go
export TMPDIR := $(CURDIR)/.work/tmp
VERSION ?= 0.2.0
SOURCE_DATE_EPOCH ?= 0

.PHONY: all build test race integration check release clean
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
	python3 scripts/publication_check.py
release: | $(TMPDIR)
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o dist/shellstudio-linux-amd64 ./cmd/shellstudio
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o dist/shellstudio-linux-arm64 ./cmd/shellstudio
	cp LICENSE README.md THIRD_PARTY_NOTICES.md dist/
	cp -R third_party dist/
	tar --sort=name --mtime='@$(SOURCE_DATE_EPOCH)' --owner=0 --group=0 --numeric-owner --exclude='__pycache__' --exclude='*.pyc' -czf dist/shellstudio-$(VERSION)-source.tar.gz .github .gitignore Makefile go.mod go.sum LICENSE README.md CONTRIBUTING.md SECURITY.md THIRD_PARTY_NOTICES.md logo.png cmd internal tests scripts docs examples third_party
	cd dist && sha256sum shellstudio-linux-* shellstudio-$(VERSION)-source.tar.gz LICENSE THIRD_PARTY_NOTICES.md > SHA256SUMS
clean:
	rm -rf bin dist .work
