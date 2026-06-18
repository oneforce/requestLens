# RequestLens

RequestLens 是一个本地 HTTP/WebSocket 请求转发、拦截、查看与调试工具。它按路径前缀把请求转发到目标服务，同时记录请求和响应 metadata，以及受大小限制的小型 JSON/Text body。

当前版本：`v1.0.0`

## 运行

推荐使用启动脚本，它会把 SQLite 数据保存在项目的 `data/` 目录下：

```bash
./start.sh
```

打开：

```txt
http://localhost:8080/
```

如果本机 `8080` 已被占用，可以换一个宿主端口：

```bash
REQUESTLENS_PORT=18080 ./start.sh
```

SQLite 默认保存到：

```txt
data/requestlens.db
```

`data/requestlens.db`、`data/requestlens.db-shm`、`data/requestlens.db-wal` 已在 `.gitignore` 中排除，不会提交到 Git。

也可以指定数据目录：

```bash
REQUESTLENS_DATA_DIR=/path/to/requestlens-data ./start.sh
```

在 Linux 上，本地目录挂载常见权限问题。`start.sh` 会默认用当前宿主机用户的 UID/GID 运行容器，并在启动前检查 `data/` 是否可写。也可以手动指定：

```bash
REQUESTLENS_UID=$(id -u) REQUESTLENS_GID=$(id -g) ./start.sh
```

构建默认不再执行 `apt-get update`，运行镜像会直接复用 builder 中的 CA 证书，避免 Debian 包源卡住。

如果 Docker Hub 拉取很慢，可以指定镜像仓库前缀后的基础镜像：

```bash
REQUESTLENS_GO_IMAGE=docker.m.daocloud.io/library/golang:1.23-bookworm \
REQUESTLENS_RUNTIME_IMAGE=docker.m.daocloud.io/library/debian:bookworm-slim \
./start.sh
```

Go 模块默认使用：

```txt
https://goproxy.cn,direct
```

也可以直接使用 Docker Compose：

```bash
docker compose up --build
```

打开：

```txt
http://localhost:8080/
```

如果本机 `8080` 已被占用，可以换一个宿主端口：

```bash
REQUESTLENS_PORT=18080 docker compose up -d --build
```

## 示例

在 Rules 页面新增：

```txt
Prefix: /sja/
Target: https://api.deepseek.com/
```

访问：

```txt
http://localhost:8080/sja/v1/chat/completions
```

会转发到：

```txt
https://api.deepseek.com/v1/chat/completions
```

## API

- `GET /api/rules`
- `POST /api/rules`
- `GET /api/rules/{id}`
- `PUT /api/rules/{id}`
- `DELETE /api/rules/{id}`
- `POST /api/rules/{id}/enable`
- `POST /api/rules/{id}/disable`
- `POST /api/rules/{id}/test`
- `GET /api/logs`
- `GET /api/logs/{id}`
- `DELETE /api/logs/{id}`
- `DELETE /api/logs`
- `GET /api/logs/{id}/request-body`
- `GET /api/logs/{id}/response-body`

## 环境变量

| 名称 | 默认值 |
| --- | --- |
| `REQUESTLENS_ADDR` | `:8080` |
| `REQUESTLENS_DB_PATH` | `data/requestlens.db` |
| `REQUESTLENS_DEFAULT_MAX_BODY_SIZE` | `262144` |
| `REQUESTLENS_LOG_RETENTION_DAYS` | `14` |
| `REQUESTLENS_RESPONSE_HEADER_TIMEOUT_SECONDS` | `60` |

## 文档

- 设计方案：`doc/PROJECT_DESIGN.md`
- TODO / 路线图：`doc/TODO_ROADMAP.md`
- 对话需求记录：`doc/CONVERSATION_REQUIREMENTS.md`
- 变更记录：`doc/CHANGELOG.md`
