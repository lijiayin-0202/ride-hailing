FROM golang:1.24-alpine AS builder

WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=v0.0.1
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-X main.Version=${VERSION}" -o ./bin/ride-hailing ./cmd/ride-hailing

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime
WORKDIR /app
COPY --from=builder /workspace/bin/ride-hailing /app/ride-hailing
COPY --from=builder /workspace/configs /app/configs

EXPOSE 8000 9000
ENTRYPOINT ["/app/ride-hailing", "-conf", "/app/configs"]
