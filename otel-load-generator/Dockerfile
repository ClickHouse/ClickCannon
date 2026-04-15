FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 go build -o /otel-load-generator .

FROM alpine:3.19
COPY --from=builder /otel-load-generator /otel-load-generator
ENTRYPOINT ["/otel-load-generator"]
