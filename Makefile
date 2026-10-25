GO ?= go
GOLANGCI_LINT_VERSION := v2.15.0
GOLANGCI_LINT_PACKAGE := github.com/golangci/golangci-lint/v2/cmd/golangci-lint
LINT_FLAGS ?= --build-tags=integration ./...
TEST_FLAGS ?= -race -count=1

.PHONY: build clean lint test test-integration test-all e2e e2e-single e2e-playlist test-small test-big

build:
	mkdir -p bin
	$(GO) build -o ./bin/dai ./cmd/dai

lint:
	$(GO) run $(GOLANGCI_LINT_PACKAGE)@$(GOLANGCI_LINT_VERSION) run --fix $(LINT_FLAGS)

test:
	$(GO) test $(TEST_FLAGS) ./...

test-integration:
	$(GO) test $(TEST_FLAGS) -tags=integration -timeout=60s ./...

test-all:
	$(MAKE) test
	$(MAKE) test-integration
	$(MAKE) e2e

e2e: e2e-single e2e-playlist

e2e-single: build
	bash ./tests/e2e/run.sh single

e2e-playlist: build
	bash ./tests/e2e/run.sh playlist

clean:
	rm -f ./bin/dai

test-small: e2e-single

test-big: e2e-playlist
