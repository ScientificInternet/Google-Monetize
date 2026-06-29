FROM golang:1.25 as builder
WORKDIR /workspace
ENV GO111MODULE=on
ENV GOWORK=off
# Copy minimal workspace only
COPY go.work ./go.work
COPY pkg ./pkg
COPY services/batchopen ./services/batchopen
WORKDIR /workspace/services/batchopen
RUN go version && \
    go mod tidy && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags='-s -w' -o /batchopen-service .

FROM gcr.io/distroless/base-debian12
WORKDIR /
COPY --from=builder /batchopen-service /batchopen-service
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/batchopen-service"]
