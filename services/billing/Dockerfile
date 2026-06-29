# Use the official Golang image to create a build artifact.
# Build triggered: 2025-10-15 backend test
# Full deployment trigger: 2025-10-15-full-deploy-test
FROM golang:1.25-alpine as builder
WORKDIR /workspace
ENV GO111MODULE=on
ENV GOWORK=off

# Install and configure git for private repositories
RUN apk add --no-cache git && \
    git config --global user.name "Cloud Build" && \
    git config --global user.email "builder@gcp.com" && \
    git config --global url."https://github.com/".insteadOf "git@github.com:" && \
    git config --global credential.helper ''
ENV GIT_TERMINAL_PROMPT=0

# Prime module cache (separate layer for better caching)
COPY go.work ./
RUN mkdir -p services/billing
COPY services/billing/go.mod services/billing/go.mod
COPY services/billing/go.sum services/billing/go.sum
RUN GOPROXY=https://proxy.golang.org,direct go mod download || true

# Copy minimal workspace and build
COPY pkg ./pkg
COPY services/billing ./services/billing

WORKDIR /workspace/services/billing
ARG COMMIT_SHA=local

RUN GOPROXY=https://proxy.golang.org,direct go mod tidy && \
    CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X main.buildVersion=${COMMIT_SHA}" \
    -o /billing-service ./cmd/server

# Runtime stage
FROM gcr.io/distroless/base-debian12

WORKDIR /

# Copy the binary from the builder stage
COPY --from=builder /billing-service /billing-service

# Copy migrations directory from the builder stage
COPY --from=builder /workspace/services/billing/internal/migrations /internal/migrations

# Expose port 8080 to the outside world
EXPOSE 8080

# Run the binary
ENTRYPOINT ["/billing-service"]
