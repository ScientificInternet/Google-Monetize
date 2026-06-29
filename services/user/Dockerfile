# Build stage
FROM golang:1.25-alpine as builder
WORKDIR /workspace
ENV GO111MODULE=on
# Disable go.work to prevent missing modules error in container
ENV GOWORK=off

# Copy minimal workspace - go.work is kept for reference but not activated
COPY go.work go.work.sum ./
COPY pkg ./pkg
COPY services/user ./services/user

WORKDIR /workspace/services/user
ARG COMMIT_SHA=local

RUN go mod tidy && \
    CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X main.buildVersion=${COMMIT_SHA} -X main.buildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o /user-service ./cmd/server

# Runtime stage
FROM gcr.io/distroless/base-debian12
WORKDIR /
COPY --from=builder /user-service /user-service
EXPOSE 8080
ENTRYPOINT ["/user-service"]