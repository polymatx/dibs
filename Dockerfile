# Build
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/dibs ./cmd/dibs

# Run — git is a runtime dependency: dibs stores coordination state in the
# repository's .git common dir.
FROM alpine:3.22
# MCP Registry ownership verification + OCI metadata
LABEL io.modelcontextprotocol.server.name="io.github.polymatx/dibs" \
      org.opencontainers.image.source="https://github.com/polymatx/dibs" \
      org.opencontainers.image.description="Coordination for parallel coding agents: file claims with expiry, enforcement hooks, and git-native lessons." \
      org.opencontainers.image.licenses="MIT"
RUN apk add --no-cache git ca-certificates
COPY --from=build /out/dibs /usr/local/bin/dibs

# A fresh container has no repository, so initialize a workspace: this lets
# `dibs mcp` start and answer MCP introspection out of the box. For real
# use, mount your repository over /workspace:
#
#   docker run --rm -i -v "$PWD":/workspace dibs mcp
WORKDIR /workspace
RUN git init -q -b main

ENTRYPOINT ["dibs"]
CMD ["mcp"]
