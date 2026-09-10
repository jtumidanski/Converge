# syntax=docker/dockerfile:1

FROM node:22-alpine AS frontend
WORKDIR /src/apps/frontend
COPY apps/frontend/package.json apps/frontend/package-lock.json ./
RUN npm ci
COPY apps/frontend/ ./
# Vite writes to ../backend/internal/ui/dist, so the target must exist.
RUN mkdir -p /src/apps/backend/internal/ui/dist && npm run build

FROM golang:1.27-alpine AS backend
ARG VERSION=dev
ARG TARGETOS=linux
ARG TARGETARCH=amd64
WORKDIR /src/apps/backend
RUN apk add --no-cache git
COPY apps/backend/go.mod apps/backend/go.sum ./
RUN go mod download
COPY apps/backend/ ./
COPY --from=frontend /src/apps/backend/internal/ui/dist ./internal/ui/dist
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath \
      -ldflags "-s -w -X github.com/jtumidanski/converge/internal/buildinfo.Version=${VERSION}" \
      -o /out/converge ./cmd/converge && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath \
      -ldflags "-s -w -X github.com/jtumidanski/converge/internal/buildinfo.Version=${VERSION}" \
      -o /out/converge-cli ./cmd/converge-cli

FROM alpine:3.22
RUN apk add --no-cache git ca-certificates tini && \
    adduser -D -u 10001 converge && \
    mkdir -p /data/repositories /data/workspaces && \
    chown -R converge:converge /data
COPY --from=backend /out/converge /usr/local/bin/converge
COPY --from=backend /out/converge-cli /usr/local/bin/converge-cli
USER converge
WORKDIR /data
ENV APP_PORT=8080 \
    WORKSPACE_ROOT=/data/workspaces \
    REPOSITORY_CACHE_ROOT=/data/repositories
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -qO- "http://127.0.0.1:${APP_PORT}/healthz" >/dev/null || exit 1
ENTRYPOINT ["/sbin/tini", "--", "/usr/local/bin/converge"]
