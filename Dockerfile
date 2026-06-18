# syntax=docker/dockerfile:1

FROM golang:1.23-bookworm AS builder
WORKDIR /src
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=${GOPROXY}

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/requestlens ./cmd/requestlens

FROM debian:bookworm-slim
RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates \
  && rm -rf /var/lib/apt/lists/*

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
