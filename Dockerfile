# syntax=docker/dockerfile:1

# Build stage for Go API
FROM golang:1.23-alpine AS api-builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git make gcc musl-dev

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY cmd/ cmd/
COPY pkg/ pkg/

# Build arguments for version info
ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_DATE=unknown

# Build the binary
RUN CGO_ENABLED=1 GOOS=linux go build \
    -ldflags "-X main.Version=${VERSION} -X main.GitCommit=${GIT_COMMIT} -X main.BuildDate=${BUILD_DATE}" \
    -o /app/dispatchoor \
    ./cmd/dispatchoor

# Build stage for React UI
FROM node:22-alpine AS ui-builder

WORKDIR /app/ui

# Copy package files first for better caching
COPY ui/package.json ui/package-lock.json ./
RUN npm ci

# Copy UI source and build
COPY ui/ ./
RUN npm run build

# Final stage
FROM alpine:3.21

WORKDIR /app

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 dispatchoor && \
    adduser -u 1000 -G dispatchoor -s /bin/sh -D dispatchoor

# Copy binaries and assets
COPY --from=api-builder /app/dispatchoor /app/dispatchoor
COPY --from=ui-builder /app/ui/dist /app/ui/dist

# Copy example config
COPY config.example.yaml /app/config.example.yaml

# Set ownership
RUN chown -R dispatchoor:dispatchoor /app

USER dispatchoor

# Expose port
EXPOSE 9090

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:9090/health || exit 1

ENTRYPOINT ["/app/dispatchoor"]
CMD ["server", "--config", "/app/config.yaml"]
