# Conty MCP Server
#
#   make deploy
#   podman run --rm \
#     -v ~/.config/conty:/root/.config/conty:ro,z \
#     localhost/conty:latest serve --addr :8082

FROM golang:1.25.8-bookworm AS build

WORKDIR /src
COPY . .

# Inject Red Hat IT root CAs at build time using a secret mount.
# The cert files are NEVER written into any image layer — only the merged
# ca-certificates.crt output is copied to the runtime stage below.
# Build with: make deploy  (passes --secret automatically)
RUN --mount=type=secret,id=rh_cas,target=/tmp/rh-cas.pem \
    cp /tmp/rh-cas.pem /usr/local/share/ca-certificates/rh-cas.crt && \
    update-ca-certificates

ARG VERSION=dev
RUN CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags="-s -w -X github.com/dpopsuev/conty/internal/adapter/driver/mcp.Version=${VERSION}" \
    -o /conty .

FROM gcr.io/distroless/static-debian12

COPY --from=build /conty /conty
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
  CMD ["/conty", "version"]

ENTRYPOINT ["/conty"]
CMD ["serve"]
