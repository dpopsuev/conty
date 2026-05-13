BINARY       := conty
VERSION      ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS      := -ldflags "-X github.com/dpopsuev/conty/internal/adapter/driver/mcp.Version=$(VERSION)"
GOBIN        ?= $(shell go env GOBIN)
ifeq ($(GOBIN),)
GOBIN = $(shell go env GOPATH)/bin
endif

IMAGE        ?= conty:$(VERSION)
CONTAINER_RT ?= podman

.PHONY: build install test test-integration lint preflight clean image deploy release

build:
	go build $(LDFLAGS) -o bin/$(BINARY) .

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

image:
	$(CONTAINER_RT) build --build-arg VERSION=$(VERSION) -t $(IMAGE) -t conty:latest .

deploy: image
	-$(CONTAINER_RT) stop conty 2>/dev/null
	-$(CONTAINER_RT) rm conty 2>/dev/null
	$(CONTAINER_RT) run -d --name conty \
		-p 8082:8082 \
		-v $(HOME)/.config/conty:/root/.config/conty:ro,z \
		$(IMAGE) \
		serve --addr :8082

release:
	@test -n "$(V)" || (echo "usage: make release V=v0.8.0" && exit 1)
	git tag $(V)
	git push origin main --tags
	$(CONTAINER_RT) tag $(IMAGE) ghcr.io/dpopsuev/conty:$(V)
	$(CONTAINER_RT) push ghcr.io/dpopsuev/conty:$(V)
