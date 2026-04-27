VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X main.Version=$(VERSION) \
	-X main.Commit=$(COMMIT) \
	-X main.BuildDate=$(DATE)

.PHONY: build test test-integration test-e2e lint fmt vet check clean

build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o ./agent2shell ./cmd/agent2shell

test:
	go test ./... -race -count=1

test-integration:
	go test ./tests/integration/... -race -count=1

test-e2e:
	go test ./tests/e2e/... -race -count=1

lint:
	golangci-lint run ./...

fmt:
	gofmt -s -w .
	goimports -w .

vet:
	go vet ./...

check:
	@UNFORMATTED=$$(gofmt -s -l .); if [ -n "$$UNFORMATTED" ]; then echo "Files not formatted:"; echo "$$UNFORMATTED"; exit 1; fi
	go vet ./...
	go test ./... -race -count=1

clean:
	rm -f ./agent2shell
	find . -name '*.test' -delete
