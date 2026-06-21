
MODULES=$(shell go list -m)
MODULE_DIRS=$(shell go list -m -f '{{.Dir}}')

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X main.version=$(VERSION) \
           -X main.commit=$(COMMIT) \
           -X main.date=$(DATE)

build:
	go build -ldflags "$(LDFLAGS)" -o godig ./cmd/godig
install:
	go install -ldflags "$(LDFLAGS)" ./cmd/godig
run:
	go run -ldflags "$(LDFLAGS)" ./cmd/godig

# Runs unit tests and the integration suite under ./tests (which builds the
# binary and drives it against a fake pkg.go.dev server).
test:
	go test -race -v ${MODULES} ./...
watch-test:
	reflex -t 50ms -s -- sh -c 'gotest -race -v ${MODULES} ./...'

# Integration tests only: the end-to-end suite under ./tests.
integration:
	go test -race -count=1 -v ./tests/...

bench:
	go test -benchmem -count 3 -bench ${MODULES} ./...
watch-bench:
	reflex -t 50ms -s -- sh -c 'go test -benchmem -count 3 -bench ${MODULES} ./...'

coverage:
	go test -v -coverprofile=cover.out -covermode=atomic ${MODULES} ./...
	go tool cover -html=cover.out -o cover.html

tools:
	go install github.com/cespare/reflex@latest
	go install github.com/rakyll/gotest@latest
	go install github.com/psampaz/go-mod-outdated@latest
	go install github.com/jondot/goweight@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go get -t -u golang.org/x/tools/cmd/cover
	go install github.com/sonatype-nexus-community/nancy@latest
	go install golang.org/x/perf/cmd/benchstat@latest
	go install github.com/cespare/prettybench@latest
	go mod tidy

lint:
	golangci-lint run --timeout 60s --max-same-issues 50 ${MODULE_DIRS}
lint-fix:
	golangci-lint run --timeout 60s --max-same-issues 50 --fix ${MODULE_DIRS}

audit:
	go list -json -m all | nancy sleuth

outdated:
	go list -u -m -json all | go-mod-outdated -update -direct

weight:
	goweight
