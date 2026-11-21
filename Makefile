GO        ?= go
BINARY    ?= bin/guildbank
VERSION   ?= dev
COMMIT    ?= none
BUILD_DATE?= unknown
LDFLAGS   := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)

.PHONY: all build run test test-race vet fmt fmt-check lint docker-build compose-up compose-down clean tidy

all: fmt-check vet test build

build:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/guildbank

run: build
	GUILDBANK_SQLITE_PATH=./config/guildbank.db $(BINARY)

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -w cmd internal

fmt-check:
	@test -z "$$(gofmt -l cmd internal)" || (echo "gofmt required:"; gofmt -l cmd internal; exit 1)

lint: fmt-check vet

docker-build:
	docker build --build-arg VERSION=$(VERSION) -t guildbank:$(VERSION) .

compose-up:
	docker compose up --build -d

compose-down:
	docker compose down

tidy:
	$(GO) mod tidy

clean:
	rm -rf bin config tmp coverage.out