# Build stage
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache gcc musl-dev

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /gateway ./cmd/gateway

# Run stage
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /gateway /usr/local/bin/gateway
COPY voice-gateway/audio/fallback/ /etc/voxlane/audio/

EXPOSE 8080
USER nobody
ENTRYPOINT ["/usr/local/bin/gateway"]
