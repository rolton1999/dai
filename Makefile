GO ?= go
GOLANGCI_LINT_VERSION := v2.15.0
GOLANGCI_LINT_PACKAGE := github.com/golangci/golangci-lint/v2/cmd/golangci-lint
LINT_FLAGS ?= ./...
TEST_FLAGS ?= -race -count=1

.PHONY: build clean lint test e2e e2e-single e2e-singe e2e-playlist test-small test-big

build:
	mkdir -p bin
	$(GO) build -o ./bin/dai ./cmd/dai

lint:
	$(GO) run $(GOLANGCI_LINT_PACKAGE)@$(GOLANGCI_LINT_VERSION) run --fix $(LINT_FLAGS)

test:
	$(GO) test $(TEST_FLAGS) ./...

e2e: e2e-single e2e-playlist

e2e-single: build
	bash ./e2e/run.sh single

e2e-singe: e2e-single

e2e-playlist: build
	bash ./e2e/run.sh playlist

clean:
	rm -rf *.mp4 *.json 

test-small: clean build
	./bin/dai https://rutube.ru/video/2e2efa63e94fe94ba8ef270a7260134c/

test-big: clean build
	./bin/dai https://rutube.ru/video/fc030f6915f11251816e47c3cfdaa59d/ 
