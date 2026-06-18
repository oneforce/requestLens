const $ = (selector, root = document) => root.querySelector(selector);
const $$ = (selector, root = document) => Array.from(root.querySelectorAll(selector));

const state = {
  view: location.hash.replace("#", "") || "logs",
  rules: [],
  logs: [],
  selectedLogId: null,
  bodyCache: new Map(),
  autoRefresh: false,
  timer: null,
};

async function api(path, options = {}) {
  const init = {
    method: options.method || "GET",
    headers: { "Content-Type": "application/json" },
  };
  if (options.body !== undefined) {
    init.body = JSON.stringify(options.body);
  }
  const response = await fetch(path, init);
  const payload = await response.json().catch(() => null);
  if (!response.ok || !payload?.ok) {
    const message = payload?.error?.message || response.statusText || "请求失败";
    throw new Error(message);
  }
  return payload.data;
}

function toast(message) {
  const el = $("#toast");
  el.textContent = message;
  el.hidden = false;
  clearTimeout(el._timer);
  el._timer = setTimeout(() => {
    el.hidden = true;
  }, 2600);
}

function setView(view) {
  state.view = view;
  location.hash = view;
  $$(".nav-tab").forEach((btn) => btn.classList.toggle("active", btn.dataset.view === view));
  $$(".view").forEach((el) => el.classList.toggle("active", el.id === `${view}View`));
  if (view === "logs") {
    loadLogs();
  }
  if (view === "rules") {
    loadRules();
  }
}

async function loadHealth() {
  try {
    const data = await api("/api/health");
    $("#healthText").textContent = `在线 ${new Date(data.time).toLocaleTimeString()}`;
  } catch (error) {
    $("#healthText").textContent = "离线";
  }
}

async function loadRules() {
  try {
    state.rules = await api("/api/rules");
    renderRuleOptions();
    renderRules();
  } catch (error) {
    toast(error.message);
  }
}

function renderRuleOptions() {
  const select = $("#filterRule");
  const current = select.value;
  select.innerHTML = `<option value="">全部</option>`;
  state.rules.forEach((rule) => {
    const option = document.createElement("option");
    option.value = rule.id;
    option.textContent = rule.name || rule.prefix;
    select.appendChild(option);
  });
  select.value = current;
}

function renderRules() {
  const tbody = $("#rulesTable");
  tbody.innerHTML = "";
  if (!state.rules.length) {
    tbody.innerHTML = `<tr><td colspan="6" class="muted">暂无规则</td></tr>`;
    return;
  }
  state.rules.forEach((rule) => {
    const tr = document.createElement("tr");
    tr.innerHTML = `
      <td><button class="icon-btn" data-action="toggle" title="切换启用状态">${rule.enabled ? "●" : "○"}</button></td>
      <td><strong>${escapeHTML(rule.name)}</strong><br><span class="badge">${escapeHTML(rule.category || "默认")}</span></td>
      <td class="mono">${escapeHTML(rule.prefix)}</td>
      <td class="mono url-cell" title="${escapeAttr(rule.target_base_url)}">${escapeHTML(rule.target_base_url)}</td>
      <td>
        <span class="badge ${rule.capture_request_body ? "ok" : ""}">REQ</span>
        <span class="badge ${rule.capture_response_body ? "ok" : ""}">RES</span>
        <span class="badge">${formatBytes(rule.max_body_size)}</span>
      </td>
      <td>
        <div class="row-actions">
          <button class="ghost-btn" data-action="edit">编辑</button>
          <button class="ghost-btn" data-action="test">测试</button>
          <button class="danger-btn" data-action="delete">删除</button>
        </div>
      </td>
    `;
    tr.dataset.id = rule.id;
    tbody.appendChild(tr);
  });
}

function collectRuleForm() {
  return {
    name: $("#ruleName").value.trim(),
    prefix: $("#rulePrefix").value.trim(),
    target_base_url: $("#ruleTarget").value.trim(),
    category: $("#ruleCategory").value.trim(),
    enabled: $("#ruleEnabled").checked,
    capture_request_headers: true,
    capture_response_headers: true,
    capture_request_body: $("#ruleReqBody").checked,
    capture_response_body: $("#ruleResBody").checked,
    max_body_size: Number($("#ruleMaxBody").value || 0),
    allow_binary_preview: $("#ruleBinary").checked,
    allow_stream_preview: $("#ruleStream").checked,
    redact_sensitive_headers: $("#ruleRedact").checked,
    preserve_host: false,
    cors_mode: $("#ruleCors").value,
    rewrite_redirect_location: $("#ruleRedirect").checked,
    rewrite_cookie: false,
    timeout_ms: 60000,
  };
}

function fillRuleForm(rule) {
  $("#ruleId").value = rule?.id || "";
  $("#ruleName").value = rule?.name || "";
  $("#rulePrefix").value = rule?.prefix || "";
  $("#ruleTarget").value = rule?.target_base_url || "";
  $("#ruleCategory").value = rule?.category || "";
  $("#ruleEnabled").checked = rule?.enabled ?? true;
  $("#ruleReqBody").checked = rule?.capture_request_body ?? true;
  $("#ruleResBody").checked = rule?.capture_response_body ?? true;
  $("#ruleMaxBody").value = rule?.max_body_size || 262144;
  $("#ruleBinary").checked = rule?.allow_binary_preview ?? false;
  $("#ruleStream").checked = rule?.allow_stream_preview ?? false;
  $("#ruleRedact").checked = rule?.redact_sensitive_headers ?? true;
  $("#ruleCors").value = rule?.cors_mode || "passthrough";
  $("#ruleRedirect").checked = rule?.rewrite_redirect_location ?? true;
  $("#ruleFormTitle").textContent = rule ? "编辑规则" : "新增规则";
  $("#ruleFormMode").textContent = rule ? `#${rule.id}` : "草稿";
  $("#ruleFeedback").textContent = "";
}

async function saveRule(event) {
  event.preventDefault();
  const id = $("#ruleId").value;
  const body = collectRuleForm();
  try {
    if (id) {
      await api(`/api/rules/${id}`, { method: "PUT", body });
      toast("规则已更新");
    } else {
      await api("/api/rules", { method: "POST", body });
      toast("规则已创建");
    }
    fillRuleForm(null);
    await loadRules();
  } catch (error) {
    $("#ruleFeedback").textContent = error.message;
  }
}

async function loadLogs() {
  const params = new URLSearchParams();
  const q = $("#filterQuery").value.trim();
  const method = $("#filterMethod").value;
  const status = $("#filterStatus").value;
  const ruleId = $("#filterRule").value;
  const type = $("#filterType").value;
  if (q) params.set("q", q);
  if (method) params.set("method", method);
  if (status) params.set("status", status);
  if (ruleId) params.set("rule_id", ruleId);
  if (type === "errors") params.set("only_errors", "true");
  if (type === "json") params.set("is_json", "true");
  if (type === "stream") params.set("is_stream", "true");
  if (type === "websocket") params.set("is_websocket", "true");
  params.set("limit", "100");

  try {
    const result = await api(`/api/logs?${params.toString()}`);
    state.logs = result.items || [];
    $("#logsCount").textContent = `共 ${result.total} 条`;
    renderLogs();
  } catch (error) {
    toast(error.message);
  }
}

function renderLogs() {
  const tbody = $("#logsTable");
  tbody.innerHTML = "";
  if (!state.logs.length) {
    tbody.innerHTML = `<tr><td colspan="7" class="muted">暂无请求记录</td></tr>`;
    return;
  }
  state.logs.forEach((log) => {
    const tr = document.createElement("tr");
    tr.dataset.id = log.id;
    tr.classList.toggle("selected", Number(state.selectedLogId) === log.id);
    tr.innerHTML = `
      <td class="mono">${formatTime(log.started_at)}</td>
      <td><span class="method">${escapeHTML(log.method)}</span></td>
      <td>${statusBadge(log.response_status, log.error_message)}</td>
      <td class="url-cell mono" title="${escapeAttr(log.original_url)}">${escapeHTML(log.original_url)}</td>
      <td>${log.duration_ms || 0} ms</td>
      <td>${typeBadges(log)}</td>
      <td>${formatBytes(log.request_body_size)} / ${formatBytes(log.response_body_size)}</td>
    `;
    tbody.appendChild(tr);
  });
}

async function openLog(id) {
  state.selectedLogId = Number(id);
  renderLogs();
  try {
    const log = await api(`/api/logs/${id}`);
    renderLogDetail(log);
  } catch (error) {
    toast(error.message);
  }
}

function renderLogDetail(log) {
  const panel = $("#detailPanel");
  panel.innerHTML = `
    <div class="panel-title">
      <h2>${escapeHTML(log.method)} ${statusBadge(log.response_status, log.error_message)}</h2>
      <button class="icon-btn" id="closeDetailBtn" title="关闭">×</button>
    </div>
    <div class="detail-content">
      <div class="kv-grid">
        ${kv("请求 ID", log.request_id)}
        ${kv("规则", log.rule_name || "-")}
        ${kv("开始时间", formatDate(log.started_at))}
        ${kv("耗时", `${log.duration_ms || 0} ms`)}
        ${kv("原始地址", log.original_url)}
        ${kv("转发地址", log.proxied_url || "-")}
      </div>
      <h3 class="section-title">请求 Headers</h3>
      ${headersTable(log.request_headers)}
      <h3 class="section-title">请求 Body</h3>
      ${bodyShell("request", log)}
      <h3 class="section-title">响应 Headers</h3>
      ${headersTable(log.response_headers)}
      <h3 class="section-title">响应 Body</h3>
      ${bodyShell("response", log)}
      ${log.error_message ? `<h3 class="section-title">错误</h3><pre class="body-box">${escapeHTML(log.error_message)}</pre>` : ""}
    </div>
  `;
  $("#closeDetailBtn").addEventListener("click", () => {
    state.selectedLogId = null;
    panel.innerHTML = `<div class="empty-state"><div class="pulse-line"></div><strong>未选择请求</strong></div>`;
    renderLogs();
  });
  $$("[data-load-body]", panel).forEach((btn) => {
    btn.addEventListener("click", () => loadBody(log.id, btn.dataset.loadBody, log));
  });
  $$("[data-copy-body]", panel).forEach((btn) => {
    btn.addEventListener("click", () => copyBody(log.id, btn.dataset.copyBody, log));
  });
  $$("[data-save-body]", panel).forEach((btn) => {
    btn.addEventListener("click", () => saveBody(log.id, btn.dataset.saveBody, log));
  });
  if (log.request_body_stored_bytes > 0 || log.request_body_omitted_reason) {
    loadBody(log.id, "request", log);
  }
  if (log.response_body_stored_bytes > 0 || log.response_body_omitted_reason) {
    loadBody(log.id, "response", log);
  }
}

function bodyShell(kind, log) {
  const omitted = kind === "request" ? log.request_body_omitted_reason : log.response_body_omitted_reason;
  const truncated = kind === "request" ? log.request_body_truncated : log.response_body_truncated;
  const size = kind === "request" ? log.request_body_size : log.response_body_size;
  const stored = kind === "request" ? log.request_body_stored_bytes : log.response_body_stored_bytes;
  const warning = omitted ? `<span class="badge warn">${escapeHTML(omitted)}</span>` : "";
  const cut = truncated ? `<span class="badge warn">已截断</span>` : "";
  const hasStored = stored > 0;
  return `
    <div class="body-toolbar">
      <button class="ghost-btn" data-load-body="${kind}">${hasStored ? "重新加载" : "加载"}</button>
      <button class="ghost-btn" data-copy-body="${kind}" ${hasStored ? "" : "disabled"}>复制</button>
      <button class="ghost-btn" data-save-body="${kind}" ${hasStored ? "" : "disabled"}>保存</button>
      <span class="badge">${formatBytes(size)}</span>
      <span class="badge">已存 ${formatBytes(stored)}</span>
      ${warning}${cut}
    </div>
    <div id="${kind}BodyBox" class="body-box">${hasStored ? "自动加载中" : omitted || "空内容"}</div>
  `;
}

async function loadBody(id, kind, log) {
  const box = $(`#${kind}BodyBox`);
  box.classList.remove("json-tree");
  box.textContent = "加载中";
  try {
    const body = await fetchBody(id, kind);
    if (!body.body) {
      box.textContent = body.omitted_reason || "空内容";
      return;
    }
    const contentType = kind === "request" ? log.request_content_type : log.content_type;
    if ((contentType || "").includes("json")) {
      renderJSONBody(box, body.body);
    } else {
      box.textContent = body.body;
    }
  } catch (error) {
    box.textContent = error.message;
  }
}

async function fetchBody(id, kind) {
  const key = `${id}:${kind}`;
  if (state.bodyCache.has(key)) {
    return state.bodyCache.get(key);
  }
  const body = await api(`/api/logs/${id}/${kind}-body?format=text`);
  state.bodyCache.set(key, body);
  return body;
}

async function copyBody(id, kind, log) {
  try {
    const body = await fetchBody(id, kind);
    const text = formatBodyForExport(body.body, body.content_type || (kind === "request" ? log.request_content_type : log.content_type));
    await writeClipboard(text);
    toast("Body 已复制");
  } catch (error) {
    toast(`复制失败：${error.message}`);
  }
}

async function saveBody(id, kind, log) {
  try {
    const body = await fetchBody(id, kind);
    const contentType = body.content_type || (kind === "request" ? log.request_content_type : log.content_type);
    const text = formatBodyForExport(body.body, contentType);
    const ext = contentType.includes("json") ? "json" : "txt";
    const filename = `${log.request_id || id}-${kind}-body.${ext}`;
    const blob = new Blob([text], { type: contentType.includes("json") ? "application/json;charset=utf-8" : "text/plain;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = filename;
    document.body.appendChild(link);
    link.click();
    link.remove();
    URL.revokeObjectURL(url);
    toast(`已保存 ${filename}`);
  } catch (error) {
    toast(`保存失败：${error.message}`);
  }
}

function renderJSONBody(box, text) {
  try {
    const parsed = JSON.parse(text);
    const formatted = JSON.stringify(parsed, null, 2);
    box.innerHTML = "";
    box.classList.add("json-tree");
    const title = document.createElement("div");
    title.className = "body-note";
    title.textContent = "JSON 格式化视图";
    box.appendChild(title);
    box.appendChild(jsonNode(parsed));
    const raw = document.createElement("details");
    raw.className = "json-raw";
    raw.innerHTML = `<summary>格式化文本</summary><pre>${escapeHTML(formatted)}</pre>`;
    box.appendChild(raw);
  } catch {
    box.textContent = text;
  }
}

function formatBodyForExport(text, contentType = "") {
  if ((contentType || "").includes("json")) {
    try {
      return JSON.stringify(JSON.parse(text), null, 2);
    } catch {
      return text;
    }
  }
  return text || "";
}

async function writeClipboard(text) {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(text);
    return;
  }
  const textarea = document.createElement("textarea");
  textarea.value = text;
  textarea.setAttribute("readonly", "");
  textarea.style.position = "fixed";
  textarea.style.left = "-9999px";
  document.body.appendChild(textarea);
  textarea.select();
  const ok = document.execCommand("copy");
  textarea.remove();
  if (!ok) {
    throw new Error("浏览器不允许写入剪贴板");
  }
}

function jsonNode(value, key = null) {
  const wrap = document.createElement("div");
  const prefix = key === null ? "" : `"${key}": `;
  if (value && typeof value === "object") {
    const details = document.createElement("details");
    details.open = true;
    const summary = document.createElement("summary");
    summary.innerHTML = `${key === null ? "" : `<span class="json-key">"${escapeHTML(key)}"</span>: `}${Array.isArray(value) ? "[" : "{"}${Object.keys(value).length}${Array.isArray(value) ? "]" : "}"}`;
    details.appendChild(summary);
    Object.entries(value).forEach(([childKey, childValue]) => details.appendChild(jsonNode(childValue, childKey)));
    wrap.appendChild(details);
    return wrap;
  }
  const type = value === null ? "null" : typeof value;
  const rendered = type === "string" ? `"${escapeHTML(value)}"` : escapeHTML(String(value));
  wrap.innerHTML = `<span class="json-key">${escapeHTML(prefix)}</span><span class="json-${type}">${rendered}</span>`;
  return wrap;
}

function headersTable(raw) {
  if (!raw) {
    return `<div class="body-box">空内容</div>`;
  }
  let headers;
  try {
    headers = JSON.parse(raw);
  } catch {
    return `<pre class="body-box">${escapeHTML(raw)}</pre>`;
  }
  const rows = Object.entries(headers)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([key, values]) => `<tr><td class="mono">${escapeHTML(key)}</td><td class="mono">${escapeHTML((values || []).join("\\n"))}</td></tr>`)
    .join("");
  return `<table class="mini-table"><tbody>${rows || `<tr><td colspan="2">空内容</td></tr>`}</tbody></table>`;
}

function kv(label, value) {
  return `<div class="kv"><b>${escapeHTML(label)}</b><span class="mono">${escapeHTML(value || "-")}</span></div>`;
}

function statusBadge(status, error) {
  if (error) return `<span class="badge error">错误</span>`;
  if (!status) return `<span class="badge warn">0</span>`;
  const cls = status >= 500 ? "error" : status >= 400 ? "warn" : status >= 300 ? "info" : "ok";
  return `<span class="badge ${cls}">${status}</span>`;
}

function typeBadges(log) {
  const badges = [];
  if (log.is_json) badges.push(`<span class="badge ok">JSON</span>`);
  if (log.is_stream) badges.push(`<span class="badge info">流式</span>`);
  if (log.is_binary) badges.push(`<span class="badge warn">二进制</span>`);
  if (log.is_websocket) badges.push(`<span class="badge info">WS</span>`);
  if (log.error_message) badges.push(`<span class="badge error">错误</span>`);
  return badges.join(" ") || `<span class="badge">文本</span>`;
}

function formatBytes(value) {
  const bytes = Number(value || 0);
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

function formatTime(value) {
  if (!value) return "-";
  return new Date(value).toLocaleTimeString();
}

function formatDate(value) {
  if (!value) return "-";
  return new Date(value).toLocaleString();
}

function escapeHTML(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}

function escapeAttr(value) {
  return escapeHTML(value).replaceAll("\n", " ");
}

function bindEvents() {
  $$(".nav-tab").forEach((btn) => btn.addEventListener("click", () => setView(btn.dataset.view)));
  $("#refreshLogsBtn").addEventListener("click", loadLogs);
  $("#autoRefreshBtn").addEventListener("click", () => {
    state.autoRefresh = !state.autoRefresh;
    $("#autoRefreshBtn").classList.toggle("active", state.autoRefresh);
    clearInterval(state.timer);
    if (state.autoRefresh) state.timer = setInterval(loadLogs, 2500);
  });
  $("#clearLogsBtn").addEventListener("click", async () => {
    if (!confirm("确定清空所有请求日志？")) return;
    await api("/api/logs", { method: "DELETE" });
    state.selectedLogId = null;
    await loadLogs();
    toast("日志已清空");
  });
  ["filterQuery", "filterMethod", "filterStatus", "filterRule", "filterType"].forEach((id) => {
    const el = $(`#${id}`);
    el.addEventListener(id === "filterQuery" ? "input" : "change", debounce(loadLogs, 260));
  });
  $("#logsTable").addEventListener("click", (event) => {
    const row = event.target.closest("tr[data-id]");
    if (row) openLog(row.dataset.id);
  });
  $("#ruleForm").addEventListener("submit", saveRule);
  $("#newRuleBtn").addEventListener("click", () => fillRuleForm(null));
  $("#resetRuleBtn").addEventListener("click", () => fillRuleForm(null));
  $("#testRuleBtn").addEventListener("click", async () => {
    const id = $("#ruleId").value;
    if (!id) {
      $("#ruleFeedback").textContent = "请先保存规则";
      return;
    }
    try {
      const result = await api(`/api/rules/${id}/test`, { method: "POST", body: {} });
      $("#ruleFeedback").textContent = JSON.stringify(result, null, 2);
    } catch (error) {
      $("#ruleFeedback").textContent = error.message;
    }
  });
  $("#rulesTable").addEventListener("click", async (event) => {
    const button = event.target.closest("button[data-action]");
    const row = event.target.closest("tr[data-id]");
    if (!button || !row) return;
    const id = row.dataset.id;
    const rule = state.rules.find((item) => String(item.id) === id);
    const action = button.dataset.action;
    try {
      if (action === "edit") fillRuleForm(rule);
      if (action === "toggle") {
        await api(`/api/rules/${id}/${rule.enabled ? "disable" : "enable"}`, { method: "POST" });
        await loadRules();
      }
      if (action === "test") {
        const result = await api(`/api/rules/${id}/test`, { method: "POST", body: {} });
        toast(result.reachable ? `目标可达：${result.status}` : result.error);
      }
      if (action === "delete") {
        if (!confirm(`确定删除 ${rule.name || rule.prefix}？`)) return;
        await api(`/api/rules/${id}`, { method: "DELETE" });
        await loadRules();
      }
    } catch (error) {
      toast(error.message);
    }
  });
}

function debounce(fn, wait) {
  let timer;
  return (...args) => {
    clearTimeout(timer);
    timer = setTimeout(() => fn(...args), wait);
  };
}

window.addEventListener("hashchange", () => setView(location.hash.replace("#", "") || "logs"));

bindEvents();
loadHealth();
loadRules().then(loadLogs);
setView(state.view === "rules" ? "rules" : "logs");
