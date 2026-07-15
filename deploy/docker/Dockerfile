# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build
RUN apk add --no-cache git ca-certificates
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/outline-gate ./cmd/outline-gate

FROM alpine:3.21
RUN apk add --no-cache ca-certificates nftables iproute2 wget
COPY --from=build /out/outline-gate /usr/local/bin/outline-gate
COPY deploy/docker/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
EXPOSE 1080 8080
HEALTHCHECK --interval=15s --timeout=3s --start-period=20s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/readyz || exit 1
ENTRYPOINT ["/entrypoint.sh"]
CMD ["outline-gate"]
