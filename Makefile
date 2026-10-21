VERSION ?= 0.1.0
GO ?= go
PNPM ?= pnpm
GOFMT ?= gofmt
BASELINE_SAMPLES ?= 30
BASELINE_OUTPUT ?= baseline/results/latest.json
LDFLAGS = -s -w -X main.version=$(VERSION)

.PHONY: web build release check test test-unit test-browser test-e2e test-browsers-install format format-check install baseline
web:
	$(PNPM) --dir web install --frozen-lockfile
	$(PNPM) --dir web build

build: web
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/webmux ./cmd/webmux

release: web
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/webmux-linux-amd64 ./cmd/webmux
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/webmux-linux-arm64 ./cmd/webmux
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/webmux-darwin-amd64 ./cmd/webmux
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/webmux-darwin-arm64 ./cmd/webmux

check: web format-check
	$(PNPM) --dir web lint
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
	$(GOFMT) -w cmd internal web/embed.go
	web/node_modules/.bin/oxfmt README.md CONTRIBUTING.md SECURITY.md docs .oxfmtrc.json web

format-check:
	@files="$$($(GOFMT) -l cmd internal web/embed.go)"; \
		if [ -n "$$files" ]; then printf 'Unformatted Go files:\n%s\n' "$$files"; exit 1; fi
	web/node_modules/.bin/oxfmt --check README.md CONTRIBUTING.md SECURITY.md docs .oxfmtrc.json web

install: build
	install -d $(HOME)/.local/bin
	install -m 755 bin/webmux $(HOME)/.local/bin/webmux
