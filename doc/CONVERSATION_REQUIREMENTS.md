# 对话需求记录

## 2026-06-18

### 初始需求

用户希望开发一个“本地 HTTP 请求转发、拦截、查看与调试工具”，类似轻量版本地 Reverse Proxy + Request Inspector。

核心要求：

- 配置本地路径前缀到目标地址的转发规则。
- 支持多规则、分类、启用/禁用、是否记录请求和响应。
- 保存请求/响应日志到 SQLite。
- Web UI 查看、搜索、分析请求。
- 支持 Docker 构建和运行。
- 前端要求科技感、简洁。

### 前端中文化

用户要求：

> 中文

> 这个前端， 给我改成中文

处理结果：

- Web UI 导航、筛选、表格、表单、按钮、提示、空状态改为中文。
- 保留 HTTP Method、JSON、WebSocket 等技术名词。

### Body 查看和保存

用户要求：

> 看不到request body 和response body， 需要能看到并保存
> 这个如果是json， 支持json格式查看

处理结果：

- 请求详情中自动加载 Request Body / Response Body。
- Body 支持复制和保存为文件。
- JSON 支持格式化树视图和格式化文本。

### 不使用简单 Proxy

用户指出：

> 你不用用简单的proxy， proxy是不会给你 request body 和response body的。

处理结果：

- 普通 HTTP 转发改为自定义转发管线。
- 自己构造 upstream request。
- 自己调用 RoundTrip。
- 自己写回 response stream。
- 在 request/response 流程中同步捕获 body。
- 上游失败时，小型 JSON/Text request body 仍可保存。

### 流式响应问题

用户指出：

> 流式 请求， 就很差， 没有看到最后的结果

处理计划 / 当前处理：

- 已调整文本流式响应捕获策略：SSE / LLM 常见文本流默认限量保存。
- 二进制流仍默认只保存元信息，避免撑爆 SQLite 和页面。
- 下一步增强：为 SSE/LLM 增加专门视图，展示原始事件和合并后的最终文本。

### 文档归档要求

用户要求：

> 将需求统一保存到 doc 目录下
> 增加一个todo / 路线图 文件
> 每次对话， 我的内容也保存起来

处理结果：

- 设计文档移动到 `doc/PROJECT_DESIGN.md`。
- 新增 `doc/TODO_ROADMAP.md`。
- 新增 `doc/CONVERSATION_REQUIREMENTS.md`。
- 后续每次用户提出新需求，都应追加记录到 `doc/CONVERSATION_REQUIREMENTS.md`。

### 明亮色 UI

用户要求：

> 我给一个明亮色的ui

处理计划：

- 将当前深色科技感 UI 调整为明亮色系。
- 保持简洁、工具台、可扫描的布局。
- 使用浅色背景、白色面板、蓝青主色，并保留清晰状态色。

处理结果：

- 已将前端切换为明亮色 UI。
- 已刷新静态资源版本号，避免旧样式缓存。
- 已通过 Docker 构建、Go 测试、桌面与手机宽度浏览器检查。

### Git 发布与提交

用户要求：

> 记录变更， 使用git 版本 v1.0.0
> 代码提交到 git@github.com:oneforce/requestLens.git

处理计划：

- 新增 `doc/CHANGELOG.md` 记录 `v1.0.0` 变更。
- 在 README 中标注当前版本。
- 创建 Git 提交和 `v1.0.0` 标签。
- 设置远端为 `git@github.com:oneforce/requestLens.git` 并推送代码。
