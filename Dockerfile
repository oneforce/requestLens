# syntax=docker/dockerfile:1

ARG GO_IMAGE=golang:1.23-bookworm
ARG RUNTIME_IMAGE=debian:bookworm-slim

FROM ${GO_IMAGE} AS builder
WORKDIR /src
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=${GOPROXY}

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/requestlens ./cmd/requestlens

FROM ${RUNTIME_IMAGE}
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

RUN useradd -r -u 10001 requestlens \
  && mkdir -p /data \
  && chown requestlens:requestlens /data

WORKDIR /app
COPY --from=builder /out/requestlens /app/requestlens

ENV REQUESTLENS_ADDR=:8080
ENV REQUESTLENS_DB_PATH=/data/requestlens.db
ENV REQUESTLENS_DEFAULT_MAX_BODY_SIZE=262144
ENV REQUESTLENS_LOG_RETENTION_DAYS=14

VOLUME ["/data"]
EXPOSE 8080
USER requestlens

ENTRYPOINT ["/app/requestlens"]
