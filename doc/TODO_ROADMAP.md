# RequestLens TODO / 路线图

## 当前已完成

- Go 后端服务与 Docker 部署。
- SQLite 保存代理规则和请求日志。
- 自定义 HTTP 转发管线，不再依赖简单 ReverseProxy。
- Request Body / Response Body 捕获、保存、复制、下载。
- JSON Body 格式化查看。
- 中文科技感简洁 Web UI。
- 明亮色 Web UI 主题。
- 规则管理、日志列表、日志详情、清空日志。
- v1.0.0 变更记录与 Git 发布。
- 启动脚本与本地 SQLite 持久化目录。
- Docker 构建镜像源可配置，运行阶段去掉 `apt-get update`。
- SQLite 数据目录权限检查与容器 UID/GID 配置。

## 高优先级

- [x] 普通 HTTP 请求不使用简单 proxy，改为自定义转发和 body 捕获。
- [x] 小型 JSON/Text request body 在转发前缓存，确保上游失败时也能保存。
- [x] response body 在写回浏览器时同步捕获。
- [x] JSON body 支持格式化查看、复制和保存。
- [x] 文本流式响应默认限量保存，避免 SSE/LLM 流式响应完全看不到结果。
- [x] 前端调整为明亮色 UI，使用浅色背景、白色面板和清晰状态色。
- [x] 新增 v1.0.0 变更记录。
- [x] 新增 `start.sh`，默认将 SQLite 保存到项目 `data/` 目录。
- [x] Docker 构建支持指定基础镜像，并移除运行阶段 `apt-get update`。
- [x] 启动时检查 SQLite 数据目录/文件权限，并支持容器使用宿主机 UID/GID。
- [ ] 为 SSE/LLM 流式响应增加专门视图：展示原始事件、合并后的文本结果、`[DONE]` 标记。
- [ ] 日志详情页区分“仍在进行中的流”和“已结束的流”。
- [ ] 增加日志导出功能：单条导出 JSON/HAR，body 单独导出。

## 中优先级

- [ ] 增加请求重放功能。
- [ ] 增加复制为 curl。
- [ ] 增加日志保留策略 UI。
- [ ] 增加规则导入/导出。
- [ ] 增加 WebSocket 消息逐帧记录。
- [ ] 增加 Brotli 响应按需解码。

## 低优先级

- [ ] 更细的 Header 脱敏配置。
- [ ] Cookie Domain/Path rewrite 配置。
- [ ] 更强的 JSON 搜索和差异对比。
- [ ] 移动端细节优化。

## 设计原则

- 这是请求调试工具，不是只负责转发的简单 proxy。
- 对 JSON/Text 优先保证可查看、可保存、可复盘。
- 对大文件、二进制、音视频、无限流默认限量保存或只保存元信息。
- 流式内容不能因为记录日志而阻塞返回给浏览器。
- 所有用户新增需求都追加到 `doc/CONVERSATION_REQUIREMENTS.md`。
