# RequestLens 变更记录

## v1.0.0 - 2026-06-18

首次可用版本。

### 新增

- Go 后端服务与 Docker 构建运行。
- SQLite 持久化代理规则、请求日志、请求 body 和响应 body。
- 自定义 HTTP 转发管线，不依赖简单 ReverseProxy。
- WebSocket 101 升级后的双向隧道转发。
- 规则管理：新增、编辑、删除、启用、禁用和连通性测试。
- 请求日志列表、筛选、详情查看和清空。
- Request Body / Response Body 查看、复制和保存。
- JSON Body 格式化树视图和格式化文本查看。
- 文本流式响应限量保存，支持查看 SSE / LLM 常见最终输出片段。
- 中文明亮色 Web UI。
- 项目设计文档、TODO / 路线图、对话需求记录。

### 说明

- 二进制、大文件和无限流默认只保存元信息或限量内容，避免撑爆 SQLite。
- Brotli 响应暂未做按需解码。
- SSE / LLM 专门视图、请求重放、复制为 curl、HAR 导出等能力进入后续路线图。
