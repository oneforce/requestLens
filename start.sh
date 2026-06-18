#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DATA_DIR="${REQUESTLENS_DATA_DIR:-$ROOT_DIR/data}"
PORT="${REQUESTLENS_PORT:-8080}"
DB_FILE="$DATA_DIR/requestlens.db"

compose() {
  if ! command -v docker >/dev/null 2>&1; then
    echo "未找到 docker，请先安装 Docker。"
    exit 1
  fi

  if docker compose version >/dev/null 2>&1; then
    docker compose "$@"
    return
  fi

  if command -v docker-compose >/dev/null 2>&1; then
    docker-compose "$@"
    return
  fi

  echo "未找到 docker compose 或 docker-compose，请先安装 Docker。"
  exit 1
}

copy_existing_sqlite() {
  if [ -f "$DB_FILE" ]; then
    return
  fi

  if ! command -v docker >/dev/null 2>&1; then
    return
  fi

  if ! docker ps -a --format '{{.Names}}' | grep -qx 'requestlens'; then
    return
  fi

  tmp_dir="$(mktemp -d)"
  trap 'rm -rf "$tmp_dir"' EXIT

  if ! docker cp requestlens:/data/. "$tmp_dir/" >/dev/null 2>&1; then
    return
  fi

  copied=0
  for name in requestlens.db requestlens.db-shm requestlens.db-wal; do
    if [ -f "$tmp_dir/$name" ]; then
      cp -n "$tmp_dir/$name" "$DATA_DIR/$name"
      copied=1
    fi
  done

  if [ "$copied" = "1" ]; then
    echo "已从现有 requestlens 容器复制 SQLite 数据到: $DATA_DIR"
  fi
}

cd "$ROOT_DIR"
mkdir -p "$DATA_DIR"
copy_existing_sqlite

export REQUESTLENS_PORT="$PORT"
export REQUESTLENS_DATA_DIR="$DATA_DIR"

echo "启动 RequestLens..."
echo "服务端口: $PORT"
echo "SQLite 数据库: $DB_FILE"

compose up -d --build

echo "RequestLens 已启动: http://localhost:$PORT/"
