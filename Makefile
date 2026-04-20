GO ?= go
GOLANGCI_LINT ?= golangci-lint
GOVULNCHECK ?= govulncheck

COVER_PROFILE ?= coverage.out
COVER_THRESHOLD ?= 70

PKG := ./...

.PHONY: all
all: tidy lint test

.PHONY: tidy
tidy:
	$(GO) mod tidy

.PHONY: build
build:
	$(GO) build $(PKG)

.PHONY: vet
vet:
	$(GO) vet $(PKG)

.PHONY: lint
lint:
	$(GOLANGCI_LINT) run $(PKG)

.PHONY: fmt
fmt:
	$(GO) fmt $(PKG)

.PHONY: test
test:
	$(GO) test -short $(PKG)

.PHONY: test-race
test-race:
	$(GO) test -race -short $(PKG)

.PHONY: cover
cover:
	$(GO) test -race -covermode=atomic -coverprofile=$(COVER_PROFILE) $(PKG)
	$(GO) tool cover -func=$(COVER_PROFILE) | tail -n 1

.PHONY: bench
bench:
	$(GO) test -run=^$$ -bench=. -benchmem $(PKG)

.PHONY: vuln
vuln:
	$(GOVULNCHECK) $(PKG)

.PHONY: e2e
e2e:
	$(GO) test -tags=integration -count=1 ./test/integration/...

.PHONY: docs
docs:
	./scripts/gen-docs.sh

.PHONY: clean
clean:
	rm -f $(COVER_PROFILE) coverage.html p4ctl

.PHONY: help
help:
	@echo "Targets:"
	@echo "  tidy       - go mod tidy"
	@echo "  build      - go build ./..."
	@echo "  vet        - go vet ./..."
	@echo "  lint       - golangci-lint run"
	@echo "  test       - short unit tests"
	@echo "  test-race  - race detector"
	@echo "  cover      - coverage profile"
	@echo "  bench      - benchmarks"
	@echo "  vuln       - govulncheck"
	@echo "  e2e        - integration tests (requires BMv2)"
	@echo "  docs       - regenerate API docs"
