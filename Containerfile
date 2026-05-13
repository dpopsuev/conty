# Conty MCP Server
#
#   make deploy
#   podman run --rm \
#     -v ~/.config/conty:/root/.config/conty:ro,z \
#     localhost/conty:latest serve --addr :8082

FROM golang:1.25.8-bookworm AS build

WORKDIR /src
COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags="-s -w -X github.com/dpopsuev/conty/internal/adapter/driver/mcp.Version=${VERSION}" \
    -o /conty .

FROM gcr.io/distroless/static-debian12

COPY --from=build /conty /conty

HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
  CMD ["/conty", "version"]

ENTRYPOINT ["/conty"]
CMD ["serve"]
