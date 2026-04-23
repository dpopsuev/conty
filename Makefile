BINARY := conty
GOBIN  ?= $(shell go env GOBIN)
ifeq ($(GOBIN),)
GOBIN = $(shell go env GOPATH)/bin
endif

.PHONY: build install test test-integration lint preflight clean

build:
	go build -o bin/$(BINARY) .

install: build
	cp bin/$(BINARY) $(GOBIN)/$(BINARY)

test:
	go test ./...

test-integration:
	go test -tags=integration ./...

lint:
	golangci-lint run ./...

preflight: lint test
	@echo "preflight passed"

clean:
	rm -rf bin/
