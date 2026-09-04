.PHONY: build test vet lint install clean dev dev-setup check install-cron uninstall-cron

BIN := chv
CMD := ./cmd/chv
CRONCTL := go run ./cmd/cronctl
# Prefer GOPATH/bin after `make install`, else local build.
CHV_BIN ?= $(firstword $(shell command -v chv 2>/dev/null) $(abspath $(BIN)))

build:
	go build -o $(BIN) $(CMD)

test:
	go test ./...

vet:
	go vet ./...

lint: vet

install:
	go install $(CMD)

clean:
	rm -f $(BIN)

check: vet test

install-cron: build
	$(CRONCTL) install --chv $(CHV_BIN)

uninstall-cron:
	$(CRONCTL) uninstall

dev-setup:
	go install github.com/air-verse/air@latest

dev:
	air
