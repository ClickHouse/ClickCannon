# Build on the native arch and cross-compile via Go (fast, no QEMU). TARGETOS/TARGETARCH
# default to the host platform for plain `docker build .`, so single-arch builds still work.
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

ARG TARGETOS
ARG TARGETARCH

RUN apk add --no-cache git

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -a -installsuffix cgo -o clickcannon .

FROM gcr.io/distroless/static-debian13

WORKDIR /root/

COPY --from=builder /app/clickcannon .

ENTRYPOINT ["./clickcannon"]
