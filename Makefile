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

RH_CAS_DIR   ?= /etc/pki/ca-trust/source/anchors
RH_CAS_BUNDLE := $(shell mktemp /tmp/rh-cas-XXXXXX.pem 2>/dev/null)

image:
	cat $(RH_CAS_DIR)/2015-RH-IT-Root-CA.pem $(RH_CAS_DIR)/2022-IT-Root-CA.pem > $(RH_CAS_BUNDLE)
	$(CONTAINER_RT) build \
		--build-arg VERSION=$(VERSION) \
		--secret id=rh_cas,src=$(RH_CAS_BUNDLE) \
		-t $(IMAGE) .
	@rm -f $(RH_CAS_BUNDLE)

deploy: image
	@SERVICE=$${HOME}/.config/systemd/user/container-conty.service; \
	if [ -f "$$SERVICE" ]; then \
		sed -i "s|conty:v[^ ]*|$(IMAGE)|g" "$$SERVICE"; \
		systemctl --user daemon-reload; \
		systemctl --user restart container-conty.service; \
		echo "systemd service restarted with $(IMAGE)"; \
	else \
		$(CONTAINER_RT) stop conty 2>/dev/null || true; \
		$(CONTAINER_RT) rm conty 2>/dev/null || true; \
		$(CONTAINER_RT) run -d --name conty \
			-p 8082:8082 \
			-e JENKINS_CI_API_KEY=$${JENKINS_CI_API_KEY} \
			-e JENKINS_AUTO_API_KEY=$${JENKINS_AUTO_API_KEY} \
			-e GITHUB_TOKEN=$${GITHUB_TOKEN} \
			-v $(HOME)/.config/conty:/root/.config/conty:ro,z \
			$(IMAGE) serve --addr :8082; \
	fi

release:
	@test -n "$(V)" || (echo "usage: make release V=v0.8.0" && exit 1)
	@test ! -d certs || (echo "ERROR: remove certs/ before releasing — do not ship internal CAs to a public registry" && exit 1)
	git tag $(V)
	git push origin main --tags
	$(CONTAINER_RT) tag $(IMAGE) ghcr.io/dpopsuev/conty:$(V)
	$(CONTAINER_RT) push ghcr.io/dpopsuev/conty:$(V)
