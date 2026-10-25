VERSION ?= 0.1.0
GO ?= go
PNPM ?= pnpm
GOFMT ?= gofmt
BASELINE_SAMPLES ?= 30
BASELINE_OUTPUT ?= baseline/results/latest.json
LDFLAGS = -s -w -X main.version=$(VERSION)
TRAY_PKG = github.com/rainchan/webmux/internal/tray
# Set by release: the Linux server digests the tray may install.
TRAY_LDFLAGS ?=
RELEASE_FILES = webmux-linux-amd64 webmux-linux-arm64 webmux-darwin-amd64 webmux-darwin-arm64 \
	webmux-windows-amd64.exe webmux-windows-arm64.exe webmux-tray-windows-amd64.exe webmux-tray-windows-arm64.exe

.PHONY: web build tray release check test test-unit test-browser test-e2e test-browsers-install format format-check install baseline
web:
	$(PNPM) --dir web install --frozen-lockfile
	$(PNPM) --dir web build

build: web
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/webmux ./cmd/webmux

# The Windows tray needs no frontend build; its icon and version resources
# come from the committed cmd/webmux-tray/rsrc_windows_*.syso files. A plain
# `make tray` carries no server digests and therefore cannot install webmux.
tray:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS) $(TRAY_LDFLAGS) -H=windowsgui" -o bin/webmux-tray-windows-amd64.exe ./cmd/webmux-tray
	CGO_ENABLED=0 GOOS=windows GOARCH=arm64 $(GO) build -trimpath -ldflags "$(LDFLAGS) $(TRAY_LDFLAGS) -H=windowsgui" -o bin/webmux-tray-windows-arm64.exe ./cmd/webmux-tray

# Linux servers first: the tray embeds their digests, and SHA256SUMS lists
# every artifact. The last step proves both trays carry both digests.
release: web
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/webmux-linux-amd64 ./cmd/webmux
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/webmux-linux-arm64 ./cmd/webmux
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/webmux-darwin-amd64 ./cmd/webmux
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/webmux-darwin-arm64 ./cmd/webmux
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/webmux-windows-amd64.exe ./cmd/webmux-windows
	CGO_ENABLED=0 GOOS=windows GOARCH=arm64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/webmux-windows-arm64.exe ./cmd/webmux-windows
	amd64=$$(sha256sum bin/webmux-linux-amd64 | cut -d' ' -f1) && \
	arm64=$$(sha256sum bin/webmux-linux-arm64 | cut -d' ' -f1) && \
	$(MAKE) tray TRAY_LDFLAGS="-X $(TRAY_PKG).linuxAMD64SHA256=$$amd64 -X $(TRAY_PKG).linuxARM64SHA256=$$arm64" && \
	for exe in bin/webmux-tray-windows-amd64.exe bin/webmux-tray-windows-arm64.exe; do \
		grep -qaF "$$amd64" $$exe && grep -qaF "$$arm64" $$exe || { echo "$$exe lacks the server digests" >&2; exit 1; }; \
	done
	cd bin && sha256sum $(RELEASE_FILES) > SHA256SUMS

check: web format-check
	$(PNPM) --dir web lint
	$(PNPM) --dir web check:arch
	$(GO) vet ./...
	golangci-lint run ./...

test:
	$(GO) test -race -parallel=4 ./...
	$(PNPM) --dir web test

baseline: web
	$(GO) run ./cmd/baseline -samples $(BASELINE_SAMPLES) -output $(BASELINE_OUTPUT)

test-unit:
	$(PNPM) --dir web test:unit

test-browser:
	$(PNPM) --dir web test:browser

test-e2e:
	$(PNPM) --dir web test:e2e

test-browsers-install:
	$(PNPM) --dir web exec playwright install --with-deps chromium

format:
	$(GOFMT) -w cmd internal web/embed.go deploy/embed.go
	web/node_modules/.bin/oxfmt README.md CONTRIBUTING.md SECURITY.md docs .oxfmtrc.json web

format-check:
	@files="$$($(GOFMT) -l cmd internal web/embed.go deploy/embed.go)"; \
		if [ -n "$$files" ]; then printf 'Unformatted Go files:\n%s\n' "$$files"; exit 1; fi
	web/node_modules/.bin/oxfmt --check README.md CONTRIBUTING.md SECURITY.md docs .oxfmtrc.json web

install: build
	install -d $(HOME)/.local/bin
	install -m 755 bin/webmux $(HOME)/.local/bin/webmux
