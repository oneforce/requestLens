# RequestLens 项目设计方案

## 一、项目一句话概括

RequestLens 是一个面向个人本地调试的轻量级 HTTP/WebSocket 反向代理与请求查看器：浏览器请求先进入本地 Go 服务，服务根据路径前缀转发到真实目标地址，同时以安全、限量、不阻塞流式传输的方式记录请求和响应，供 Web UI 检索、查看与分析。

## 二、需求整理

### 2.1 核心功能

1. 路由转发配置
   - 支持多个代理规则。
   - 每条规则由本地路径前缀匹配，例如 `/sja/`、`/openai/`。
   - 转发到目标基础地址，例如 `https://api.deepseek.com/`。
   - 规则支持名称、分类、启用状态、记录开关、body 捕获开关和最大 body 记录大小。

2. HTTP 请求转发
   - 支持常见 HTTP 方法：`GET`、`POST`、`PUT`、`PATCH`、`DELETE`、`OPTIONS`、`HEAD`。
   - 保留原请求的 method、query、headers、cookie、authorization、content-type 和 body。
   - 只改目标 URL、目标 host，以及必要的代理头。
   - 支持普通请求、文件上传、文件下载、Range、chunked、压缩响应、SSE 和 WebSocket Upgrade。

3. 请求和响应记录
   - 记录请求元信息、转发目标、耗时、状态码、headers、content-type、大小、错误信息。
   - 小 JSON/Text body 可以记录到 SQLite。
   - 大 body、二进制、文件、流式内容默认只记录 metadata 或有限 preview。
   - 超过限制时截断并标记 `truncated`。

4. Web UI
   - 代理规则管理：列表、新增、编辑、删除、启用/禁用、分类查看、目标测试。
   - 请求日志列表：时间、method、URL、状态码、耗时、类型、大小、所属规则、错误状态。
   - 请求详情：headers 表格、请求/响应 body、JSON 格式化、复制、搜索、截断提示。
   - 对二进制、文件、视频、音频、流式内容默认显示概要，用户确认后再查看有限内容。

5. API
   - 提供规则管理 API。
   - 提供日志查询、详情、删除、body 单独读取 API。
   - 非 `/api/*` 和 Web UI 静态资源路径进入代理匹配流程。

6. Docker 部署
   - 不要求本机安装 Go。
   - 使用 Docker 构建与运行。
   - SQLite 数据挂载到 volume。
   - 默认暴露 `8080`。

### 2.2 非目标

1. 不做多用户、权限系统、团队协作。
2. 不做企业级高并发代理网关。
3. 不默认解密 HTTPS 浏览器代理流量。这里是路径前缀反向代理，不是系统级 MITM 代理。
4. 不默认完整保存大文件、视频、音频、WebSocket 全量消息和无限流式内容。

## 三、补全后的请求类型支持清单

### 3.1 HTTP 方法

| 方法 | 支持策略 |
| --- | --- |
| `GET` | 直接转发，记录 query、headers、响应 |
| `POST` | 流式转发 body，可限量捕获 preview |
| `PUT` | 同 `POST`，适合文件或对象上传 |
| `PATCH` | 同 `POST` |
| `DELETE` | 直接转发，可带 body |
| `OPTIONS` | 默认转发到目标；可开启本地开发 CORS 模式 |
| `HEAD` | 只记录 headers 和状态，不尝试读取响应 body |

### 3.2 浏览器请求来源

| 来源 | 支持策略 |
| --- | --- |
| 地址栏请求 | 作为普通 `GET` 转发 |
| `fetch` | 完整保留 method、headers、body |
| `XMLHttpRequest` | 与 `fetch` 一致 |
| HTML form | 支持 `application/x-www-form-urlencoded` 和 `multipart/form-data` |
| 文件上传 | request body 流式转发，只截取前 N 字节 preview |
| JSON 请求 | 小 body 记录并在 UI 格式化 |
| text/plain | 小 body 直接记录 |
| octet-stream | 默认只记录 metadata，可选 preview |
| 静态资源 | 支持图片、字体、CSS、JS、音频、视频等 |

### 3.3 响应类型

| 类型 | 支持策略 |
| --- | --- |
| JSON | 小响应存储并格式化展示 |
| HTML | 小响应按文本展示，可提示可能包含脚本 |
| Text | 直接展示 |
| Blob/Binary | 默认 metadata，必要时有限 preview |
| 文件下载 | 保留 `Content-Disposition`、`Content-Length`、Range 相关 headers |
| 图片 | 默认 metadata，可提供小图预览但不强制 |
| 音频/视频 | 默认 metadata，避免大文件进入 SQLite |
| gzip/deflate/br | 默认保留目标行为；日志 preview 根据能力有限解码 |
| chunked | 流式转发，限量捕获 |
| Range | 保留 Range 请求和 `206 Partial Content` 响应 |

### 3.4 流式与长连接

| 类型 | 支持策略 |
| --- | --- |
| SSE `text/event-stream` | 自定义转发器即时 flush，默认限量保存文本流内容，记录 metadata 和 preview |
| HTTP chunked streaming | 不聚合完整 body，边读边转发边计数 |
| LLM 流式响应 | 按 SSE/chunked 处理，避免等待流结束才返回 |
| 大文件下载 | 不完整入库，只记录大小、类型、耗时、前 N 字节可选 |
| 大文件上传 | request body 直通 upstream，捕获器只做限量 tee |
| WebSocket `ws://` | HTTP Upgrade 直通，MVP 记录连接 metadata |
| WebSocket Secure `wss://` | upstream 走 TLS，MVP 记录连接 metadata |

## 四、推荐技术架构

### 4.1 总体选择

| 层 | 推荐方案 | 原因 |
| --- | --- | --- |
| 后端 | Go `net/http` + 自定义转发器 | 转发、响应复制和 body 捕获完全可控，更适合请求查看器 |
| 路由 | Go 1.22+ `http.ServeMux` 或 `chi` | MVP 可用标准库；若 API 路由复杂再引入 `chi` |
| 数据库 | SQLite + `database/sql` | 单机个人工具足够简单，方便 Docker volume 持久化 |
| SQLite driver | `modernc.org/sqlite` | 纯 Go，Docker 构建更简单，避免 CGO 依赖 |
| Web UI | Go 静态文件服务 + 原生 HTML/CSS/JS | 无前端构建链，部署简单 |
| JSON 查看 | 前端轻量 JSON tree 组件或自写递归渲染 | 避免引入重型 SPA |
| 部署 | Docker multi-stage build + docker compose | 本机无需 Go 环境 |

### 4.2 后端模块

1. `config`
   - 读取端口、数据库路径、默认 body 限制、日志保留天数、敏感 header 策略。

2. `store`
   - SQLite 初始化、migration、规则 CRUD、日志写入和查询。

3. `rules`
   - 规则校验、前缀规范化、最长前缀匹配。

4. `proxy`
   - 构造目标 URL。
   - 显式构造 upstream request。
   - 包装 request/response body 捕获器。
   - 管理自定义转发、错误处理、流式 flush、WebSocket Upgrade。

5. `api`
   - JSON API。
   - 参数解析、分页过滤、错误响应。

6. `web`
   - 静态 UI。
   - 可使用 `embed` 将 `web/` 打包进二进制。

### 4.3 Header 推荐策略

| 项 | 推荐默认值 | 说明 |
| --- | --- | --- |
| Host | 改为目标服务 host | 兼容 TLS SNI、虚拟主机和多数 API 服务 |
| 原始 Host | 写入 `X-Forwarded-Host` | 便于上游识别原始入口 |
| 客户端 IP | 追加 `X-Forwarded-For` | 保留链路信息 |
| 原始协议 | 写入 `X-Forwarded-Proto` | 本地通常是 `http`，Docker 后也清晰 |
| 代理标识 | 可写入 `Via: RequestLens` | 方便排查，必要时可关闭 |
| Hop-by-hop headers | 删除 | 包括 `Connection`、`Proxy-Connection`、`Keep-Alive`、`TE`、`Trailer`、`Upgrade` 等，WebSocket Upgrade 走专门逻辑 |
| Authorization/Cookie | 默认转发 | 代理调试工具应尽量保持真实请求 |
| Authorization/Cookie 记录 | 默认脱敏或关闭记录 | 避免本地日志暴露敏感信息 |

### 4.4 CORS、Redirect、Cookie 策略

1. CORS
   - 默认：pass-through，`OPTIONS` 和 CORS headers 都交给目标服务。
   - 可选：每条规则开启 `local_dev_cors`，由 RequestLens 对预检请求返回允许 headers，并在响应中追加 `Access-Control-Allow-*`。
   - 原因：默认不改变上游语义；本地前端调试时再显式放宽。

2. Redirect Location
   - 推荐默认开启安全重写：只重写以 `target_base_url` 开头的绝对 `Location`。
   - 示例：`https://api.deepseek.com/v1/a` 重写为 `http://localhost:8080/sja/v1/a`。
   - 不重写第三方跳转，避免破坏 OAuth 或外部下载地址。

3. Cookie Domain/Path
   - API 调试场景默认不重写。
   - 如果代理的是网页应用，可按规则开启 Cookie 重写：
     - `Domain=api.example.com` 移除或改为 `localhost`。
     - `Path=/` 可改为本地前缀 `/sja/`。
   - 默认不重写的原因是 Cookie 规则很容易影响登录态和安全属性。

## 五、核心转发流程

1. 请求进入 Go HTTP Server。
2. 判断路径：
   - `/api/*` 进入 JSON API。
   - `/assets/*`、`/ui/*` 或 `/` 进入 Web UI。
   - 其他路径进入代理匹配。
3. 读取启用的 proxy rules，按 prefix 长度倒序做最长前缀匹配。
4. 未匹配到规则：
   - API 路径返回 API 404。
   - 普通路径返回代理 404，并提示没有匹配规则。
5. 匹配到规则后构造目标 URL：
   - 去掉本地 prefix。
   - 与 `target_base_url` 的 path 安全拼接。
   - 保留原 query。
   - 示例：`/sja/v1/chat?x=1` 到 `https://api.deepseek.com/v1/chat?x=1`。
6. 创建 log context：
   - 生成 `request_id`。
   - 记录开始时间、method、original_url、proxied_url、client_ip、rule_id。
7. 包装 request body：
   - 不预先完整读取 body。
   - 使用 `io.Reader` 包装器在 upstream 读取时顺便计数和限量复制。
   - 超过 `max_body_size` 后继续转发但停止写入 capture buffer。
8. 调用 `httputil.ReverseProxy`：
   - `Rewrite` 或 `Director` 修改目标 scheme、host、path、query。
   - 设置 Host 为目标 host。
   - 追加 `X-Forwarded-*`。
   - 使用自定义 `Transport` 设置超时、TLS 和连接池。
9. 在 `ModifyResponse` 捕获响应 metadata：
   - status、headers、content-type、content-length、是否 streaming、是否 binary。
   - 包装 response body，边写回浏览器边捕获有限 preview。
10. 响应返回浏览器：
   - 对 SSE/chunked 使用及时 flush。
   - 对大文件不聚合。
11. body 复制结束或发生错误时 finalize log：
   - 计算耗时、大小、截断状态、错误信息。
   - 异步或短事务写入 SQLite。

### 5.1 如何避免读取 request body 后导致转发失败

不要在代理前执行 `io.ReadAll(r.Body)`。正确方式是把 `r.Body` 替换为一个自定义 `ReadCloser`：

```go
type captureReadCloser struct {
    rc      io.ReadCloser
    counter *BodyCapture
}

func (c *captureReadCloser) Read(p []byte) (int, error) {
    n, err := c.rc.Read(p)
    if n > 0 {
        c.counter.WriteObserved(p[:n])
    }
    return n, err
}

func (c *captureReadCloser) Close() error {
    return c.rc.Close()
}
```

这样 upstream 正常读取 body，RequestLens 只是旁路观察前 N 字节。

### 5.2 如何避免 response body 捕获破坏流式响应

不要完整读取 `res.Body` 后再返回浏览器。当前实现由自定义转发器边读 upstream response、边写回客户端、边捕获有限内容。对于 SSE 和未知长度响应：

1. 每次写回客户端后尽量 flush。
2. 捕获器只保存前 N 字节。
3. 日志写入等到 body EOF、close 或错误后完成。
4. 对长连接设置合理的状态更新，避免一直占用事务。

### 5.3 gzip/br/deflate 策略

1. RequestLens 是查看器，默认请求上游返回未压缩内容，方便保存和格式化 JSON/Text。
2. 浏览器拿到的是 RequestLens 写回的响应，不需要自己处理上游压缩体。
3. 对需要严格保持压缩协商的场景，可后续增加“原始压缩透传模式”。
4. `br` 解码和原始压缩保存可作为增强版功能。

## 六、SQLite 数据库设计

### 6.1 `proxy_rules`

```sql
CREATE TABLE IF NOT EXISTS proxy_rules (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  prefix TEXT NOT NULL UNIQUE,
  target_base_url TEXT NOT NULL,
  category TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 1,

  capture_request_headers INTEGER NOT NULL DEFAULT 1,
  capture_response_headers INTEGER NOT NULL DEFAULT 1,
  capture_request_body INTEGER NOT NULL DEFAULT 1,
  capture_response_body INTEGER NOT NULL DEFAULT 1,
  max_body_size INTEGER NOT NULL DEFAULT 262144,

  allow_binary_preview INTEGER NOT NULL DEFAULT 0,
  allow_stream_preview INTEGER NOT NULL DEFAULT 0,
  redact_sensitive_headers INTEGER NOT NULL DEFAULT 1,

  preserve_host INTEGER NOT NULL DEFAULT 0,
  cors_mode TEXT NOT NULL DEFAULT 'passthrough',
  rewrite_redirect_location INTEGER NOT NULL DEFAULT 1,
  rewrite_cookie INTEGER NOT NULL DEFAULT 0,
  timeout_ms INTEGER NOT NULL DEFAULT 60000,

  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_proxy_rules_enabled_prefix
ON proxy_rules(enabled, prefix);
```

约束建议：

1. `prefix` 必须以 `/` 开头，并建议以 `/` 结尾。
2. `target_base_url` 必须是 `http://` 或 `https://`。
3. 保存前规范化重复斜杠和尾部斜杠。

### 6.2 `http_logs`

```sql
CREATE TABLE IF NOT EXISTS http_logs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  rule_id INTEGER,
  request_id TEXT NOT NULL UNIQUE,

  started_at TEXT NOT NULL,
  finished_at TEXT,
  duration_ms INTEGER,

  method TEXT NOT NULL,
  original_url TEXT NOT NULL,
  proxied_url TEXT,
  scheme TEXT NOT NULL,
  host TEXT NOT NULL,
  path TEXT NOT NULL,
  query TEXT NOT NULL DEFAULT '',

  request_headers TEXT,
  request_body BLOB,
  request_body_truncated INTEGER NOT NULL DEFAULT 0,
  request_body_size INTEGER NOT NULL DEFAULT 0,
  request_body_omitted_reason TEXT NOT NULL DEFAULT '',

  response_status INTEGER,
  response_headers TEXT,
  response_body BLOB,
  response_body_truncated INTEGER NOT NULL DEFAULT 0,
  response_body_size INTEGER NOT NULL DEFAULT 0,
  response_body_omitted_reason TEXT NOT NULL DEFAULT '',

  content_type TEXT NOT NULL DEFAULT '',
  is_json INTEGER NOT NULL DEFAULT 0,
  is_text INTEGER NOT NULL DEFAULT 0,
  is_binary INTEGER NOT NULL DEFAULT 0,
  is_stream INTEGER NOT NULL DEFAULT 0,
  is_websocket INTEGER NOT NULL DEFAULT 0,

  error_message TEXT NOT NULL DEFAULT '',
  client_ip TEXT NOT NULL DEFAULT '',

  FOREIGN KEY(rule_id) REFERENCES proxy_rules(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_http_logs_started_at ON http_logs(started_at DESC);
CREATE INDEX IF NOT EXISTS idx_http_logs_rule_id ON http_logs(rule_id);
CREATE INDEX IF NOT EXISTS idx_http_logs_method ON http_logs(method);
CREATE INDEX IF NOT EXISTS idx_http_logs_status ON http_logs(response_status);
CREATE INDEX IF NOT EXISTS idx_http_logs_path ON http_logs(path);
CREATE INDEX IF NOT EXISTS idx_http_logs_request_id ON http_logs(request_id);
```

### 6.3 可选 `websocket_messages`

MVP 只记录 WebSocket 连接 metadata。增强版如需记录消息，单独建表，避免塞进 `http_logs`：

```sql
CREATE TABLE IF NOT EXISTS websocket_messages (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  log_id INTEGER NOT NULL,
  direction TEXT NOT NULL, -- client_to_server/server_to_client
  opcode INTEGER NOT NULL,
  message_size INTEGER NOT NULL DEFAULT 0,
  message_preview BLOB,
  message_truncated INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  FOREIGN KEY(log_id) REFERENCES http_logs(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_ws_messages_log_id
ON websocket_messages(log_id, created_at);
```

### 6.4 存储策略

1. 小 JSON/Text body 直接存入 `http_logs.request_body` 或 `http_logs.response_body`。
2. 默认 `max_body_size = 256 KiB`，每条规则可调整。
3. 超过阈值只保存前 N 字节，并设置 `*_truncated=1`。
4. 二进制、文件、音视频、大型/非文本流式内容默认不保存 body，仅保存：
   - `content_type`
   - `request_body_size`
   - `response_body_size`
   - `duration_ms`
   - `is_binary/is_stream`
   - omitted reason
5. SSE / LLM 常见文本流默认限量保存 preview，仍受 `max_body_size` 限制。
6. 可选开启二进制 preview，但仍受 `max_body_size` 限制。
7. 提供 `DELETE /api/logs` 一键清空。
8. 提供启动时清理策略：
   - `REQUESTLENS_LOG_RETENTION_DAYS=14`
   - `REQUESTLENS_MAX_LOG_ROWS=50000`
9. 敏感 headers 默认脱敏：
   - `Authorization`
   - `Cookie`
   - `Set-Cookie`
   - `X-Api-Key`
   - `Proxy-Authorization`

## 七、后端 API 设计

统一响应格式：

```json
{
  "ok": true,
  "data": {},
  "error": null
}
```

错误响应：

```json
{
  "ok": false,
  "data": null,
  "error": {
    "code": "validation_error",
    "message": "prefix must start with /"
  }
}
```

### 7.1 规则管理 API

#### `GET /api/rules`

查询参数：

| 参数 | 说明 |
| --- | --- |
| `category` | 按分类过滤 |
| `enabled` | `true` 或 `false` |

返回示例：

```json
{
  "ok": true,
  "data": [
    {
      "id": 1,
      "name": "DeepSeek",
      "prefix": "/sja/",
      "target_base_url": "https://api.deepseek.com/",
      "category": "llm",
      "enabled": true,
      "capture_request_body": true,
      "capture_response_body": true,
      "max_body_size": 262144
    }
  ],
  "error": null
}
```

#### `POST /api/rules`

请求体：

```json
{
  "name": "OpenAI",
  "prefix": "/openai/",
  "target_base_url": "https://api.openai.com/",
  "category": "llm",
  "enabled": true,
  "capture_request_body": true,
  "capture_response_body": true,
  "max_body_size": 262144,
  "allow_binary_preview": false,
  "allow_stream_preview": false
}
```

返回：创建后的规则对象。

#### `GET /api/rules/{id}`

返回单条规则详情。

#### `PUT /api/rules/{id}`

全量更新规则。校验规则同创建。

#### `DELETE /api/rules/{id}`

删除规则。历史日志保留，`rule_id` 置空。

#### `POST /api/rules/{id}/enable`

启用规则。

#### `POST /api/rules/{id}/disable`

禁用规则。

#### `POST /api/rules/{id}/test`

测试目标地址可达性。

请求体可选：

```json
{
  "path": "/",
  "timeout_ms": 3000
}
```

返回示例：

```json
{
  "ok": true,
  "data": {
    "reachable": true,
    "status": 200,
    "duration_ms": 183,
    "final_url": "https://api.deepseek.com/"
  },
  "error": null
}
```

### 7.2 日志查看 API

#### `GET /api/logs`

查询参数：

| 参数 | 说明 |
| --- | --- |
| `q` | URL、request id、错误信息模糊搜索 |
| `method` | HTTP method |
| `status` | 精确状态码 |
| `status_min` / `status_max` | 状态码范围 |
| `rule_id` | 规则 ID |
| `category` | 规则分类 |
| `content_type` | content-type 模糊搜索 |
| `from` / `to` | ISO8601 时间范围 |
| `only_errors` | 只看错误 |
| `is_json` | 只看 JSON |
| `is_stream` | 只看流式 |
| `is_websocket` | 只看 WebSocket |
| `limit` | 默认 50，最大 500 |
| `offset` | 分页偏移 |

返回示例：

```json
{
  "ok": true,
  "data": {
    "items": [
      {
        "id": 101,
        "request_id": "01JZ...",
        "started_at": "2026-06-18T10:00:00+08:00",
        "duration_ms": 241,
        "method": "POST",
        "original_url": "http://localhost:8080/sja/v1/chat/completions",
        "proxied_url": "https://api.deepseek.com/v1/chat/completions",
        "response_status": 200,
        "content_type": "application/json",
        "request_body_size": 860,
        "response_body_size": 4096,
        "rule_name": "DeepSeek",
        "is_json": true,
        "is_stream": false,
        "is_binary": false,
        "is_websocket": false,
        "error_message": ""
      }
    ],
    "total": 1,
    "limit": 50,
    "offset": 0
  },
  "error": null
}
```

#### `GET /api/logs/{id}`

返回日志详情，包含 headers 和 body preview metadata。

#### `DELETE /api/logs/{id}`

删除单条日志。

#### `DELETE /api/logs`

清空日志。可支持查询参数：

| 参数 | 说明 |
| --- | --- |
| `before` | 删除某时间之前日志 |
| `rule_id` | 只删除某规则日志 |

#### `GET /api/logs/{id}/request-body`

查询参数：

| 参数 | 说明 |
| --- | --- |
| `format` | `raw`、`text`、`json`、`base64` |
| `limit` | 本次返回最大字节数 |

返回小文本示例：

```json
{
  "ok": true,
  "data": {
    "content_type": "application/json",
    "encoding": "utf-8",
    "truncated": false,
    "body": "{ \"model\": \"deepseek-chat\" }"
  },
  "error": null
}
```

#### `GET /api/logs/{id}/response-body`

同 request body，用于响应 body。

## 八、Web UI 页面设计

### 8.1 总体布局

1. 顶部导航：
   - Logs
   - Rules
   - Settings
2. 左侧或顶部过滤栏：
   - 当前分类、规则、method、状态、类型快速筛选。
3. 主区域：
   - 日志列表或规则列表。
4. 右侧抽屉或详情页：
   - 展示单条请求详情。

推荐用原生 HTML/CSS/JS 实现：

1. `fetch` 调用 API。
2. History API 管理列表和详情 URL。
3. CSS 使用简单响应式布局。
4. JSON viewer 使用递归 DOM 渲染和 `<details>` 展开折叠。

### 8.2 代理规则页面

功能：

1. 查看所有规则。
2. 新增规则。
3. 编辑规则。
4. 删除规则。
5. 启用/禁用规则。
6. 按分类过滤。
7. 测试目标地址。

表格字段：

| 字段 | 展示 |
| --- | --- |
| Enabled | 开关 |
| Name | 规则名称 |
| Prefix | 本地前缀 |
| Target | 目标地址 |
| Category | 分类 |
| Capture | req/res body 记录状态 |
| Max Body | 最大记录大小 |
| Actions | 编辑、测试、删除 |

表单字段：

1. `name`
2. `prefix`
3. `target_base_url`
4. `category`
5. `enabled`
6. `capture_request_body`
7. `capture_response_body`
8. `max_body_size`
9. `allow_binary_preview`
10. `allow_stream_preview`
11. `redact_sensitive_headers`
12. `cors_mode`
13. `rewrite_redirect_location`
14. `rewrite_cookie`

交互：

1. prefix 输入时实时校验是否以 `/` 开头。
2. target 输入时校验 URL scheme。
3. 与已有 prefix 冲突时提示“最长前缀优先”。
4. 测试目标按钮显示状态码和耗时。

### 8.3 请求日志列表页面

列：

1. 时间
2. Method
3. 状态码
4. 原始 URL
5. 转发 URL
6. 耗时
7. Content-Type
8. 请求大小
9. 响应大小
10. 规则
11. 标签：JSON、Stream、Binary、WS、Error

过滤：

1. URL 搜索。
2. Method。
3. 状态码范围。
4. 规则。
5. 分类。
6. Content-Type。
7. 时间范围。
8. 只看错误。
9. 只看 JSON。
10. 只看流式。
11. 只看 WebSocket。

交互：

1. 点击一行打开详情。
2. 自动刷新开关，默认关闭或 2 秒刷新。
3. 清空日志按钮需要确认。
4. 大量日志分页加载，不一次性渲染。

### 8.4 请求详情页面

区域：

1. Overview
   - request id、规则、时间、耗时、method、状态、client ip。
2. URLs
   - original URL。
   - proxied URL。
3. Request Headers
   - 表格展示 key/value。
   - 脱敏 header 标注。
4. Request Body
   - JSON 格式化、折叠、复制、搜索。
   - 文本直接展示。
   - 二进制/文件只展示 metadata 和展开按钮。
5. Response Headers
   - 表格展示 key/value。
6. Response Body
   - 同 request body。
7. Error
   - 目标不可达、超时、TLS 错误等。

JSON viewer：

1. 小 JSON 自动格式化。
2. 大 JSON 需要用户点击加载。
3. 递归节点用 `<details>`。
4. 搜索 key/value 时高亮匹配节点。
5. 复制支持复制格式化 JSON 或原始 JSON。

## 九、关键技术难点与解决方案

### 9.1 request body 捕获

难点：Go 的 `Request.Body` 是一次性流。如果先读完再转发，会破坏上传、流式请求和大文件性能。

方案：

1. 用包装 `ReadCloser` 观察读取过程。
2. 只保存前 `max_body_size` 字节。
3. 始终统计真实读取大小。
4. 对 `multipart/form-data` 不解析文件内容，只按 body 流处理。
5. 如果客户端中途断开，记录已读取大小和错误。

### 9.2 response body 捕获

难点：完整读取响应会阻塞 SSE、chunked 和大文件。

方案：

1. 自定义转发器先记录 headers/status。
2. 将 `res.Body` 包装为捕获型 `ReadCloser`。
3. 转发器边读边写回浏览器。
4. 捕获器限量保存 preview，EOF 后 finalize。
5. 流式响应即时 flush。

### 9.3 streaming 响应

识别条件：

1. `Content-Type: text/event-stream`。
2. `Transfer-Encoding: chunked` 且无明确 `Content-Length`。
3. LLM API 常见 SSE 响应。
4. 长时间不结束的响应。

处理：

1. 不等待完整响应。
2. 不做无限制完整 body 入库。
3. 文本流默认保存有限 preview，二进制流默认只保存 metadata。
4. UI 标记 `Stream`。
5. 对 preview 显示“可能不是完整内容”，后续增加 LLM/SSE 合并结果视图。

### 9.4 WebSocket

MVP：

1. 识别 `Connection: Upgrade` 和 `Upgrade: websocket`。
2. 使用 `httputil.ReverseProxy` 或专门 handler 透传。
3. 记录连接建立时间、目标 URL、状态码、错误信息。
4. 不记录每条消息内容。

增强版：

1. 使用 `nhooyr.io/websocket` 或 `gorilla/websocket` 分别接入 client 和 upstream。
2. 双向复制消息。
3. 按消息记录 direction、opcode、size、preview、truncated。
4. 二进制帧默认只记录 metadata。
5. 设置最大消息 preview 和最大消息数量。

### 9.5 文件上传下载

上传：

1. 不解析整个 multipart。
2. body 流式转发。
3. 只记录 content-type、content-length、已读大小、有限 preview。

下载：

1. 保留 `Content-Disposition`。
2. 保留 Range 和 `206 Partial Content`。
3. 不把大文件写进 SQLite。
4. UI 显示文件名、大小、content-type、是否截断。

### 9.6 大 body 截断

1. 默认 body capture 上限 256 KiB。
2. 每条规则可调。
3. 超过后停止追加 buffer，但继续计数。
4. DB 记录 `*_truncated=1`。
5. UI 明确显示“已截断，仅展示前 N 字节”。

### 9.7 Header 查看和敏感信息

1. Headers 以 JSON 存储，例如 `{"Content-Type":["application/json"]}`。
2. UI 表格展示多值 header。
3. 默认脱敏：
   - `Authorization: Bearer ****`
   - `Cookie: ****`
   - `Set-Cookie: ****`
4. 可在规则中关闭脱敏，但 UI 给出提示。

### 9.8 CORS

默认 pass-through。可选本地开发模式：

1. 对 `OPTIONS` 直接返回 `204`。
2. 设置：
   - `Access-Control-Allow-Origin`
   - `Access-Control-Allow-Methods`
   - `Access-Control-Allow-Headers`
   - `Access-Control-Allow-Credentials`
3. 若请求带 credentials，不能使用 `*`，应回显 `Origin`。

### 9.9 Redirect

1. 只重写指向当前 target base 的 `Location`。
2. 支持绝对 URL 和目标站内相对 URL。
3. 不重写外部域名。
4. 日志记录原始 Location 和重写后 Location，可放入 response headers metadata。

### 9.10 Cookie

1. 默认原样转发 Cookie 和 Set-Cookie。
2. 默认记录时脱敏。
3. 可选 Cookie rewrite：
   - Domain 改为当前本地域或删除 Domain。
   - Path 改为 rule prefix。
4. 对 `Secure` Cookie，如果本地是 `http://localhost`，浏览器行为可能不同，UI 应提示。

## 十、项目目录结构

推荐结构：

```txt
requestlens/
  cmd/
    requestlens/
      main.go
  internal/
    api/
      handlers.go
      rules.go
      logs.go
      response.go
    config/
      config.go
    db/
      db.go
      migrations.go
      models.go
      rules_store.go
      logs_store.go
    proxy/
      handler.go
      matcher.go
      rewrite.go
      capture.go
      classify.go
      websocket.go
    web/
      embed.go
  migrations/
    001_init.sql
  web/
    index.html
    assets/
      app.js
      styles.css
      json-viewer.js
  data/
    .gitkeep
  Dockerfile
  docker-compose.yml
  go.mod
  go.sum
  README.md
  PROJECT_DESIGN.md
```

职责说明：

1. `cmd/requestlens/main.go`：启动入口，组装 config、db、api、proxy、web。
2. `internal/proxy`：代理核心，不依赖 UI。
3. `internal/db`：SQLite 访问和 migration。
4. `internal/api`：JSON API。
5. `web`：静态前端，无构建链。
6. `migrations`：数据库 DDL，可用 `embed` 读取执行。

## 十一、Dockerfile 和 docker-compose 设计

### 11.1 Dockerfile

```dockerfile
# syntax=docker/dockerfile:1

FROM golang:1.22-bookworm AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/requestlens ./cmd/requestlens

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=builder /out/requestlens /app/requestlens

ENV REQUESTLENS_ADDR=:8080
ENV REQUESTLENS_DB_PATH=/data/requestlens.db

VOLUME ["/data"]
EXPOSE 8080

ENTRYPOINT ["/app/requestlens"]
```

说明：

1. 使用 `modernc.org/sqlite` 时可 `CGO_ENABLED=0`。
2. 若改用 `mattn/go-sqlite3`，需要开启 CGO，并使用带 libc 的 runtime 镜像。
3. `web/` 和 `migrations/` 推荐通过 `go:embed` 打进二进制。

### 11.2 docker-compose.yml

```yaml
services:
  requestlens:
    build: .
    container_name: requestlens
    ports:
      - "8080:8080"
    environment:
      REQUESTLENS_ADDR: ":8080"
      REQUESTLENS_DB_PATH: "/data/requestlens.db"
      REQUESTLENS_DEFAULT_MAX_BODY_SIZE: "262144"
      REQUESTLENS_LOG_RETENTION_DAYS: "14"
    volumes:
      - requestlens-data:/data
    restart: unless-stopped

volumes:
  requestlens-data:
```

启动：

```bash
docker compose up --build
```

访问：

```txt
http://localhost:8080/
```

代理示例：

```txt
http://localhost:8080/sja/v1/chat/completions
```

## 十二、开发步骤

### 12.1 MVP 范围

MVP 推荐只做这些，保证可用且边界安全：

1. SQLite migration。
2. 规则 CRUD API。
3. 规则列表 UI。
4. HTTP/HTTPS 反向代理。
5. 最长前缀匹配。
6. request/response metadata 记录。
7. JSON/Text body 限量捕获。
8. 二进制、文件、stream 默认 metadata。
9. 日志列表 UI。
10. 日志详情 UI。
11. 清空日志。
12. Dockerfile 和 docker-compose。

MVP 暂不做：

1. WebSocket 消息逐帧记录。
2. Brotli 解码。
3. 复杂 Cookie rewrite。
4. 高级 JSON diff。
5. 外部代理或系统代理模式。

### 12.2 第一阶段：基础骨架

1. 初始化 Go module。
2. 建立目录结构。
3. 实现 config。
4. 实现 SQLite migration。
5. 实现 `proxy_rules` CRUD。
6. 实现基础 Web UI 静态服务。

验收：

1. `docker compose up --build` 可启动。
2. `GET /api/rules` 返回空数组。
3. UI 可打开。

### 12.3 第二阶段：HTTP 代理

1. 实现规则 prefix 规范化。
2. 实现最长前缀匹配。
3. 实现 URL 拼接。
4. 集成 `httputil.ReverseProxy`。
5. 设置 Host 和 `X-Forwarded-*`。
6. 实现目标不可达错误处理。

验收：

1. `/sja/v1/models` 能转发到目标。
2. method、query、headers、body 保留。
3. 未匹配路径返回清晰 404。

### 12.4 第三阶段：日志捕获

1. 实现 request body capture wrapper。
2. 实现 response body capture wrapper。
3. 实现 content-type 分类。
4. 实现日志插入。
5. 实现日志列表和详情 API。
6. 实现清空日志 API。

验收：

1. JSON 请求和响应可在日志里查看。
2. 超过阈值显示截断。
3. 文件下载不撑爆 SQLite。
4. SSE 不阻塞首包。

### 12.5 第四阶段：Web UI 完善

1. 规则表单。
2. 日志过滤。
3. 详情页 headers 表格。
4. JSON 格式化和折叠。
5. body 复制。
6. 二进制和 stream 安全提示。

验收：

1. 可以在浏览器里完成规则管理。
2. 可以搜索并查看请求详情。
3. 大 JSON 不导致页面卡死。

### 12.6 第五阶段：增强功能

1. WebSocket metadata 完整记录。
2. WebSocket 消息表和有限 preview。
3. Redirect Location 重写配置。
4. Cookie rewrite 配置。
5. 本地 CORS 模式。
6. 日志自动清理。
7. 导出日志为 HAR 或 JSON。
8. 规则导入导出。

## 十三、验收标准

### 13.1 代理规则

- [ ] 可以创建 `/sja/ -> https://api.deepseek.com/`。
- [ ] 可以创建 `/openai/ -> https://api.openai.com/`。
- [ ] 启用/禁用规则即时生效。
- [ ] prefix 冲突时使用最长前缀匹配。
- [ ] 删除规则不导致历史日志查询崩溃。

### 13.2 HTTP 转发

- [ ] `GET` 请求 query 保留。
- [ ] `POST application/json` body 正确转发。
- [ ] `multipart/form-data` 文件上传正确转发。
- [ ] `Authorization`、`Cookie` 默认转发。
- [ ] Host 默认改为目标 host。
- [ ] `X-Forwarded-For`、`X-Forwarded-Host`、`X-Forwarded-Proto` 正确追加。
- [ ] 目标不可达时返回清晰错误，并写入日志。
- [ ] TLS 证书错误、DNS 错误、超时都有错误日志。

### 13.3 响应处理

- [ ] JSON/Text 小响应可查看。
- [ ] 大响应被截断并标记。
- [ ] 二进制响应默认不直接展示。
- [ ] 文件下载保留文件名和下载行为。
- [ ] Range 请求返回正确分片。
- [ ] gzip 响应不破坏浏览器接收。
- [ ] chunked 响应不被完整缓存。
- [ ] SSE 首包和后续事件不因日志记录延迟。

### 13.4 WebSocket

- [ ] WebSocket Upgrade 能建立连接。
- [ ] `ws://` 本地入口可代理到 `ws` 或 `wss` 目标。
- [ ] MVP 至少记录连接 metadata。
- [ ] 长连接关闭后日志状态正确更新。

### 13.5 日志与 UI

- [ ] 日志列表展示时间、method、URL、状态、耗时、大小、规则和类型标签。
- [ ] 支持 URL、method、状态码、规则、分类、content-type、时间范围过滤。
- [ ] 支持只看错误、JSON、stream、WebSocket。
- [ ] 请求详情 headers 以表格展示。
- [ ] JSON body 可格式化、折叠、复制、搜索。
- [ ] 截断 body 有明确提示。
- [ ] 清空日志可用且需要确认。

### 13.6 Docker

- [ ] 本机不安装 Go 也能构建。
- [ ] `docker compose up --build` 成功启动。
- [ ] SQLite 数据保存在 volume。
- [ ] 容器重启后规则和日志仍存在。
- [ ] 默认访问 `http://localhost:8080/` 可打开 UI。

## 推荐 MVP 与后续增强

### 推荐 MVP

先实现“规则 CRUD + HTTP/HTTPS 转发 + metadata 日志 + 小 JSON/Text body 查看 + Docker 部署”。这部分已经能覆盖个人 API 调试的主要场景，而且不会被 WebSocket、brotli、复杂 Cookie rewrite 拖慢第一版。

MVP 规则字段：

1. `name`
2. `prefix`
3. `target_base_url`
4. `category`
5. `enabled`
6. `capture_request_body`
7. `capture_response_body`
8. `max_body_size`
9. `allow_binary_preview`
10. `allow_stream_preview`

MVP UI 页面：

1. Logs
2. Log Detail
3. Rules

MVP 代理策略：

1. Host 改为目标 host。
2. 追加 `X-Forwarded-*`。
3. CORS pass-through。
4. Redirect Location 对目标 base 做安全重写。
5. Cookie 不重写。
6. 敏感 headers 记录时默认脱敏。

### 后续增强

1. WebSocket 消息逐帧记录。
2. HAR 导出。
3. 规则导入/导出。
4. Brotli 按需解码。
5. 本地开发 CORS 模式。
6. Cookie Domain/Path rewrite。
7. 请求重放功能。
8. 从历史日志复制为 curl。
9. 日志保留策略 UI。
10. 更强 JSON 搜索、折叠和差异对比。
