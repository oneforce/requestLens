package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"requestlens/internal/config"
	"requestlens/internal/db"
)

type Handler struct {
	store              *db.Store
	defaultMaxBodySize int64
	authEnabled        bool
	client             *http.Client
}

type envelope struct {
	OK    bool `json:"ok"`
	Data  any  `json:"data"`
	Error any  `json:"error"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func NewHandler(store *db.Store, cfg config.Config) *Handler {
	return &Handler{
		store:              store,
		defaultMaxBodySize: cfg.DefaultMaxBodySize,
		authEnabled:        cfg.AuthToken != "",
		client:             &http.Client{Timeout: 5 * time.Second},
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	switch {
	case r.URL.Path == "/api/auth/status":
		h.handleAuthStatus(w, r)
	case r.URL.Path == "/api/health":
		h.handleHealth(w, r)
	case r.URL.Path == "/api/database" || strings.HasPrefix(r.URL.Path, "/api/database/"):
		h.handleDatabase(w, r)
	case r.URL.Path == "/api/rules" || strings.HasPrefix(r.URL.Path, "/api/rules/"):
		h.handleRules(w, r)
	case r.URL.Path == "/api/logs" || strings.HasPrefix(r.URL.Path, "/api/logs/"):
		h.handleLogs(w, r)
	default:
		writeError(w, http.StatusNotFound, "not_found", "api route not found")
	}
}

func (h *Handler) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	writeOK(w, map[string]any{"enabled": h.authEnabled})
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	writeOK(w, map[string]any{"status": "ok", "time": time.Now().UTC().Format(time.RFC3339Nano)})
}

func (h *Handler) handleRules(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/rules" {
		switch r.Method {
		case http.MethodGet:
			h.listRules(w, r)
		case http.MethodPost:
			h.createRule(w, r)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
		return
	}

	parts := splitPath(strings.TrimPrefix(r.URL.Path, "/api/rules/"))
	if len(parts) == 0 {
		writeError(w, http.StatusNotFound, "not_found", "rule route not found")
		return
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "validation_error", "invalid rule id")
		return
	}

	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			h.getRule(w, r, id)
		case http.MethodPut:
			h.updateRule(w, r, id)
		case http.MethodDelete:
			h.deleteRule(w, r, id)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
		return
	}

	if len(parts) == 2 && r.Method == http.MethodPost {
		switch parts[1] {
		case "enable":
			h.setRuleEnabled(w, r, id, true)
		case "disable":
			h.setRuleEnabled(w, r, id, false)
		case "test":
			h.testRule(w, r, id)
		default:
			writeError(w, http.StatusNotFound, "not_found", "rule action not found")
		}
		return
	}

	writeError(w, http.StatusNotFound, "not_found", "rule route not found")
}

func (h *Handler) listRules(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	filter := db.RuleFilter{Category: query.Get("category")}
	if raw := query.Get("enabled"); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "validation_error", "enabled must be true or false")
			return
		}
		filter.Enabled = &value
	}
	rules, err := h.store.ListRules(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeOK(w, rules)
}

func (h *Handler) createRule(w http.ResponseWriter, r *http.Request) {
	payload, err := decodeRuleRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	rule, err := buildRuleFromPayload(payload, nil, h.defaultMaxBodySize)
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	created, err := h.store.CreateRule(r.Context(), rule)
	if err != nil {
		writeError(w, http.StatusBadRequest, "store_error", err.Error())
		return
	}
	writeStatusOK(w, http.StatusCreated, created)
}

func (h *Handler) getRule(w http.ResponseWriter, r *http.Request, id int64) {
	rule, err := h.store.GetRule(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeOK(w, rule)
}

func (h *Handler) updateRule(w http.ResponseWriter, r *http.Request, id int64) {
	existing, err := h.store.GetRule(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	payload, err := decodeRuleRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	rule, err := buildRuleFromPayload(payload, &existing, h.defaultMaxBodySize)
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	rule.ID = id
	updated, err := h.store.UpdateRule(r.Context(), rule)
	if err != nil {
		writeError(w, http.StatusBadRequest, "store_error", err.Error())
		return
	}
	writeOK(w, updated)
}

func (h *Handler) deleteRule(w http.ResponseWriter, r *http.Request, id int64) {
	if err := h.store.DeleteRule(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	writeOK(w, map[string]any{"deleted": true})
}

func (h *Handler) setRuleEnabled(w http.ResponseWriter, r *http.Request, id int64, enabled bool) {
	rule, err := h.store.SetRuleEnabled(r.Context(), id, enabled)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeOK(w, rule)
}

func (h *Handler) testRule(w http.ResponseWriter, r *http.Request, id int64) {
	rule, err := h.store.GetRule(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	var payload struct {
		Path      string `json:"path"`
		TimeoutMS int    `json:"timeout_ms"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)
	if payload.TimeoutMS <= 0 {
		payload.TimeoutMS = 5000
	}
	target, err := url.Parse(rule.TargetBaseURL)
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	if payload.Path != "" {
		target.Path = singleJoiningSlash(target.Path, payload.Path)
	}

	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(payload.TimeoutMS)*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, target.String(), nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	req.Header.Set("User-Agent", "RequestLens/1.0")

	start := time.Now()
	resp, err := h.client.Do(req)
	duration := time.Since(start).Milliseconds()
	if err != nil {
		writeOK(w, map[string]any{
			"reachable":   false,
			"status":      0,
			"duration_ms": duration,
			"final_url":   target.String(),
			"error":       err.Error(),
		})
		return
	}
	defer resp.Body.Close()
	writeOK(w, map[string]any{
		"reachable":   true,
		"status":      resp.StatusCode,
		"duration_ms": duration,
		"final_url":   resp.Request.URL.String(),
	})
}

func (h *Handler) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/logs" {
		switch r.Method {
		case http.MethodGet:
			h.listLogs(w, r)
		case http.MethodDelete:
			h.deleteLogs(w, r)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
		return
	}

	parts := splitPath(strings.TrimPrefix(r.URL.Path, "/api/logs/"))
	if len(parts) == 0 {
		writeError(w, http.StatusNotFound, "not_found", "log route not found")
		return
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "validation_error", "invalid log id")
		return
	}

	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			h.getLog(w, r, id)
		case http.MethodDelete:
			h.deleteLog(w, r, id)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
		return
	}

	if len(parts) == 2 && r.Method == http.MethodGet {
		switch parts[1] {
		case "request-body":
			h.getBody(w, r, id, "request")
		case "response-body":
			h.getBody(w, r, id, "response")
		default:
			writeError(w, http.StatusNotFound, "not_found", "body route not found")
		}
		return
	}

	writeError(w, http.StatusNotFound, "not_found", "log route not found")
}

func (h *Handler) listLogs(w http.ResponseWriter, r *http.Request) {
	filter, err := parseLogFilter(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	result, err := h.store.ListLogs(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeOK(w, result)
}

func (h *Handler) getLog(w http.ResponseWriter, r *http.Request, id int64) {
	logEntry, err := h.store.GetLog(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeOK(w, logEntry)
}

func (h *Handler) deleteLog(w http.ResponseWriter, r *http.Request, id int64) {
	if err := h.store.DeleteLog(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	writeOK(w, map[string]any{"deleted": true})
}

func (h *Handler) deleteLogs(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	var ruleID int64
	if raw := query.Get("rule_id"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "validation_error", "invalid rule_id")
			return
		}
		ruleID = parsed
	}
	deleted, err := h.store.DeleteLogs(r.Context(), query.Get("before"), ruleID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeOK(w, map[string]any{"deleted": deleted})
}

func (h *Handler) getBody(w http.ResponseWriter, r *http.Request, id int64, kind string) {
	body, err := h.store.GetBody(r.Context(), id, kind)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	format := r.URL.Query().Get("format")
	switch format {
	case "base64":
		body.Body = base64.StdEncoding.EncodeToString([]byte(body.Body))
		body.Encoding = "base64"
	case "json":
		var indented bytes.Buffer
		if err := json.Indent(&indented, []byte(body.Body), "", "  "); err == nil {
			body.Body = indented.String()
		}
	case "", "text", "raw":
		if !utf8.ValidString(body.Body) {
			body.Body = base64.StdEncoding.EncodeToString([]byte(body.Body))
			body.Encoding = "base64"
		}
	default:
		writeError(w, http.StatusBadRequest, "validation_error", "format must be raw, text, json, or base64")
		return
	}
	writeOK(w, body)
}

func (h *Handler) handleDatabase(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/database/schema":
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		schema, err := h.store.DatabaseSchema(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "store_error", err.Error())
			return
		}
		writeOK(w, schema)
	case "/api/database/query":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		h.queryDatabase(w, r)
	default:
		writeError(w, http.StatusNotFound, "not_found", "database route not found")
	}
}

type databaseQueryRequest struct {
	SQL   string `json:"sql"`
	Limit int    `json:"limit"`
}

func (h *Handler) queryDatabase(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var payload databaseQueryRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	result, err := h.store.QueryDatabase(r.Context(), payload.SQL, payload.Limit)
	if err != nil {
		writeError(w, http.StatusBadRequest, "sql_error", err.Error())
		return
	}
	writeOK(w, result)
}

type ruleRequest struct {
	Name                   *string `json:"name"`
	Prefix                 *string `json:"prefix"`
	TargetBaseURL          *string `json:"target_base_url"`
	Category               *string `json:"category"`
	Enabled                *bool   `json:"enabled"`
	CaptureRequestHeaders  *bool   `json:"capture_request_headers"`
	CaptureResponseHeaders *bool   `json:"capture_response_headers"`
	CaptureRequestBody     *bool   `json:"capture_request_body"`
	CaptureResponseBody    *bool   `json:"capture_response_body"`
	MaxBodySize            *int64  `json:"max_body_size"`
	AllowBinaryPreview     *bool   `json:"allow_binary_preview"`
	AllowStreamPreview     *bool   `json:"allow_stream_preview"`
	RedactSensitiveHeaders *bool   `json:"redact_sensitive_headers"`
	PreserveHost           *bool   `json:"preserve_host"`
	CorsMode               *string `json:"cors_mode"`
	RewriteRedirect        *bool   `json:"rewrite_redirect_location"`
	RewriteCookie          *bool   `json:"rewrite_cookie"`
	TimeoutMS              *int    `json:"timeout_ms"`
}

func decodeRuleRequest(r *http.Request) (ruleRequest, error) {
	defer r.Body.Close()
	var payload ruleRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return ruleRequest{}, err
	}
	return payload, nil
}

func buildRuleFromPayload(payload ruleRequest, existing *db.ProxyRule, defaultMaxBodySize int64) (db.ProxyRule, error) {
	var rule db.ProxyRule
	if existing != nil {
		rule = *existing
	} else {
		rule = db.ProxyRule{
			Enabled:                true,
			CaptureRequestHeaders:  true,
			CaptureResponseHeaders: true,
			CaptureRequestBody:     true,
			CaptureResponseBody:    true,
			MaxBodySize:            defaultMaxBodySize,
			RedactSensitiveHeaders: true,
			CorsMode:               "passthrough",
			RewriteRedirect:        true,
			TimeoutMS:              60000,
		}
	}

	if payload.Name != nil {
		rule.Name = strings.TrimSpace(*payload.Name)
	}
	if payload.Prefix != nil {
		rule.Prefix = normalizePrefix(*payload.Prefix)
	}
	if payload.TargetBaseURL != nil {
		target, err := normalizeTarget(*payload.TargetBaseURL)
		if err != nil {
			return db.ProxyRule{}, err
		}
		rule.TargetBaseURL = target
	}
	if payload.Category != nil {
		rule.Category = strings.TrimSpace(*payload.Category)
	}
	if payload.Enabled != nil {
		rule.Enabled = *payload.Enabled
	}
	if payload.CaptureRequestHeaders != nil {
		rule.CaptureRequestHeaders = *payload.CaptureRequestHeaders
	}
	if payload.CaptureResponseHeaders != nil {
		rule.CaptureResponseHeaders = *payload.CaptureResponseHeaders
	}
	if payload.CaptureRequestBody != nil {
		rule.CaptureRequestBody = *payload.CaptureRequestBody
	}
	if payload.CaptureResponseBody != nil {
		rule.CaptureResponseBody = *payload.CaptureResponseBody
	}
	if payload.MaxBodySize != nil {
		rule.MaxBodySize = *payload.MaxBodySize
	}
	if payload.AllowBinaryPreview != nil {
		rule.AllowBinaryPreview = *payload.AllowBinaryPreview
	}
	if payload.AllowStreamPreview != nil {
		rule.AllowStreamPreview = *payload.AllowStreamPreview
	}
	if payload.RedactSensitiveHeaders != nil {
		rule.RedactSensitiveHeaders = *payload.RedactSensitiveHeaders
	}
	if payload.PreserveHost != nil {
		rule.PreserveHost = *payload.PreserveHost
	}
	if payload.CorsMode != nil {
		rule.CorsMode = strings.TrimSpace(*payload.CorsMode)
	}
	if payload.RewriteRedirect != nil {
		rule.RewriteRedirect = *payload.RewriteRedirect
	}
	if payload.RewriteCookie != nil {
		rule.RewriteCookie = *payload.RewriteCookie
	}
	if payload.TimeoutMS != nil {
		rule.TimeoutMS = *payload.TimeoutMS
	}

	if rule.Prefix == "" {
		return db.ProxyRule{}, errors.New("prefix is required")
	}
	if rule.Prefix == "/" {
		return db.ProxyRule{}, errors.New("prefix / is reserved for the Web UI")
	}
	if strings.HasPrefix(rule.Prefix, "/api/") || strings.HasPrefix(rule.Prefix, "/assets/") {
		return db.ProxyRule{}, errors.New("prefix conflicts with reserved RequestLens routes")
	}
	if rule.TargetBaseURL == "" {
		return db.ProxyRule{}, errors.New("target_base_url is required")
	}
	if rule.Name == "" {
		rule.Name = strings.Trim(rule.Prefix, "/")
	}
	if rule.MaxBodySize < 0 {
		return db.ProxyRule{}, errors.New("max_body_size cannot be negative")
	}
	if rule.CorsMode == "" {
		rule.CorsMode = "passthrough"
	}
	if rule.CorsMode != "passthrough" && rule.CorsMode != "local" {
		return db.ProxyRule{}, errors.New("cors_mode must be passthrough or local")
	}
	if rule.TimeoutMS <= 0 {
		rule.TimeoutMS = 60000
	}
	return rule, nil
}

func normalizePrefix(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	if !strings.HasSuffix(value, "/") {
		value += "/"
	}
	return value
}

func normalizeTarget(value string) (string, error) {
	value = strings.TrimSpace(value)
	parsed, err := url.Parse(value)
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("target_base_url must use http or https")
	}
	if parsed.Host == "" {
		return "", errors.New("target_base_url must include host")
	}
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	return parsed.String(), nil
}

func parseLogFilter(values url.Values) (db.LogFilter, error) {
	filter := db.LogFilter{
		Query:       values.Get("q"),
		Method:      strings.ToUpper(values.Get("method")),
		Category:    values.Get("category"),
		ContentType: values.Get("content_type"),
		From:        values.Get("from"),
		To:          values.Get("to"),
		OnlyErrors:  parseBoolDefault(values.Get("only_errors"), false),
	}
	var err error
	if filter.Status, err = parseOptionalInt(values.Get("status")); err != nil {
		return db.LogFilter{}, errors.New("invalid status")
	}
	if filter.StatusMin, err = parseOptionalInt(values.Get("status_min")); err != nil {
		return db.LogFilter{}, errors.New("invalid status_min")
	}
	if filter.StatusMax, err = parseOptionalInt(values.Get("status_max")); err != nil {
		return db.LogFilter{}, errors.New("invalid status_max")
	}
	if raw := values.Get("rule_id"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return db.LogFilter{}, errors.New("invalid rule_id")
		}
		filter.RuleID = parsed
	}
	if filter.Limit, err = parseOptionalInt(values.Get("limit")); err != nil {
		return db.LogFilter{}, errors.New("invalid limit")
	}
	if filter.Offset, err = parseOptionalInt(values.Get("offset")); err != nil {
		return db.LogFilter{}, errors.New("invalid offset")
	}
	if value, ok, err := parseOptionalBool(values.Get("is_json")); err != nil {
		return db.LogFilter{}, errors.New("invalid is_json")
	} else if ok {
		filter.IsJSON = &value
	}
	if value, ok, err := parseOptionalBool(values.Get("is_stream")); err != nil {
		return db.LogFilter{}, errors.New("invalid is_stream")
	} else if ok {
		filter.IsStream = &value
	}
	if value, ok, err := parseOptionalBool(values.Get("is_websocket")); err != nil {
		return db.LogFilter{}, errors.New("invalid is_websocket")
	} else if ok {
		filter.IsWebSocket = &value
	}
	return filter, nil
}

func parseOptionalInt(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	return strconv.Atoi(raw)
}

func parseOptionalBool(raw string) (bool, bool, error) {
	if raw == "" {
		return false, false, nil
	}
	value, err := strconv.ParseBool(raw)
	return value, true, err
}

func parseBoolDefault(raw string, fallback bool) bool {
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return value
}

func splitPath(path string) []string {
	raw := strings.Split(strings.Trim(path, "/"), "/")
	parts := make([]string, 0, len(raw))
	for _, part := range raw {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	default:
		return a + b
	}
}

func writeOK(w http.ResponseWriter, data any) {
	writeStatusOK(w, http.StatusOK, data)
}

func writeStatusOK(w http.ResponseWriter, status int, data any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope{OK: true, Data: data, Error: nil})
}

func writeError(w http.ResponseWriter, status int, code string, message string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope{OK: false, Data: nil, Error: apiError{Code: code, Message: message}})
}

func writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	writeError(w, http.StatusInternalServerError, "store_error", err.Error())
}
