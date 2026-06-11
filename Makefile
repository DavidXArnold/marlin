BINARY        := marlin
CMD           := ./cmd/marlin
COVERAGE      := coverage.out
COVERAGE_HTML := coverage.html
# 85% gate — cmd and internal/ui are TTY/root-bound and cannot reach 90%
MIN_COVERAGE  := 85.0

.PHONY: all build build-check test coverage lint clean install check \
        test-race test-verbose test-short coverage-html \
        integration e2e

all: build-check lint coverage integration build

build:
	go build -o bin/$(BINARY) $(CMD)

# build-check compiles all packages including integration tests.
# Catches signature mismatches in test/integration/ that plain go build misses.
build-check:
	go build -tags integration ./...

test:
	go test ./... -race -count=1

test-race:
	go test ./... -race -count=1 -timeout=120s

test-verbose:
	go test ./... -race -count=1 -v

test-short:
	go test ./... -short -count=1

coverage:
	go test ./... -race -count=1 -coverprofile=$(COVERAGE) -covermode=atomic
	@COVERAGE=$$(go tool cover -func=$(COVERAGE) | grep '^total:' | awk '{print $$3}' | tr -d '%'); \
	echo "Total coverage: $${COVERAGE}%"; \
	awk -v c="$$COVERAGE" -v min=$(MIN_COVERAGE) \
	  'BEGIN { if (c+0 < min+0) { print "FAIL: coverage " c "% < " min "%"; exit 1 } \
	           else { print "PASS: coverage " c "% >= " min "%" } }'

coverage-html: coverage
	go tool cover -html=$(COVERAGE) -o $(COVERAGE_HTML)
	@echo "Coverage report: $(COVERAGE_HTML)"

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/ $(COVERAGE) $(COVERAGE_HTML)

install:
	go install $(CMD)

# check: full pre-push gate identical to CI (build-check + lint + coverage + integration)
check: build-check lint coverage integration

tidy:
	go mod tidy

# integration: filesystem + API tests using an embedded mock vLLM server.
# Set MARLIN_TEST_HOST to run against a real inference server instead.
integration: build-check
	go test -tags integration -v -count=1 -timeout=120s ./test/integration/

# e2e: binary smoke tests against a real server.
# Requires: MARLIN_TEST_HOST=<server-ip>
e2e:
	@echo "Running e2e tests against MARLIN_TEST_HOST=${MARLIN_TEST_HOST}..."
	go test -tags integration -v -run TestE2E -count=1 -timeout=300s ./test/integration/
