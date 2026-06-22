package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Store struct {
	db *sql.DB
}

type ProxyRule struct {
	ID                     int64  `json:"id"`
	Name                   string `json:"name"`
	Prefix                 string `json:"prefix"`
	TargetBaseURL          string `json:"target_base_url"`
	Category               string `json:"category"`
	Enabled                bool   `json:"enabled"`
	CaptureRequestHeaders  bool   `json:"capture_request_headers"`
	CaptureResponseHeaders bool   `json:"capture_response_headers"`
	CaptureRequestBody     bool   `json:"capture_request_body"`
	CaptureResponseBody    bool   `json:"capture_response_body"`
	MaxBodySize            int64  `json:"max_body_size"`
	AllowBinaryPreview     bool   `json:"allow_binary_preview"`
	AllowStreamPreview     bool   `json:"allow_stream_preview"`
	RedactSensitiveHeaders bool   `json:"redact_sensitive_headers"`
	PreserveHost           bool   `json:"preserve_host"`
	CorsMode               string `json:"cors_mode"`
	RewriteRedirect        bool   `json:"rewrite_redirect_location"`
	RewriteCookie          bool   `json:"rewrite_cookie"`
	TimeoutMS              int    `json:"timeout_ms"`
	CreatedAt              string `json:"created_at"`
	UpdatedAt              string `json:"updated_at"`
}

type RuleFilter struct {
	Category string
	Enabled  *bool
}

type HTTPLog struct {
	ID                         int64   `json:"id"`
	RuleID                     *int64  `json:"rule_id"`
	RequestID                  string  `json:"request_id"`
	StartedAt                  string  `json:"started_at"`
	FinishedAt                 string  `json:"finished_at"`
	DurationMS                 int64   `json:"duration_ms"`
	Method                     string  `json:"method"`
	OriginalURL                string  `json:"original_url"`
	ProxiedURL                 string  `json:"proxied_url"`
	Scheme                     string  `json:"scheme"`
	Host                       string  `json:"host"`
	Path                       string  `json:"path"`
	Query                      string  `json:"query"`
	RequestHeaders             string  `json:"request_headers"`
	RequestContentType         string  `json:"request_content_type"`
	RequestBody                []byte  `json:"-"`
	RequestBodyTruncated       bool    `json:"request_body_truncated"`
	RequestBodySize            int64   `json:"request_body_size"`
	RequestBodyOmittedReason   string  `json:"request_body_omitted_reason"`
	ResponseStatus             int     `json:"response_status"`
	ResponseHeaders            string  `json:"response_headers"`
	ResponseBody               []byte  `json:"-"`
	ResponseBodyTruncated      bool    `json:"response_body_truncated"`
	ResponseBodySize           int64   `json:"response_body_size"`
	ResponseBodyOmittedReason  string  `json:"response_body_omitted_reason"`
	ContentType                string  `json:"content_type"`
	IsJSON                     bool    `json:"is_json"`
	IsText                     bool    `json:"is_text"`
	IsBinary                   bool    `json:"is_binary"`
	IsStream                   bool    `json:"is_stream"`
	IsWebSocket                bool    `json:"is_websocket"`
	ErrorMessage               string  `json:"error_message"`
	ClientIP                   string  `json:"client_ip"`
	RequestBodyStoredBytes     int     `json:"request_body_stored_bytes"`
	ResponseBodyStoredBytes    int     `json:"response_body_stored_bytes"`
	RuleName                   string  `json:"rule_name,omitempty"`
	RuleCategory               string  `json:"rule_category,omitempty"`
	ResponseStatusNullableHack *string `json:"-"`
}

type LogListItem struct {
	ID               int64  `json:"id"`
	RuleID           *int64 `json:"rule_id"`
	RuleName         string `json:"rule_name"`
	RuleCategory     string `json:"rule_category"`
	RequestID        string `json:"request_id"`
	StartedAt        string `json:"started_at"`
	FinishedAt       string `json:"finished_at"`
	DurationMS       int64  `json:"duration_ms"`
	Method           string `json:"method"`
	OriginalURL      string `json:"original_url"`
	ProxiedURL       string `json:"proxied_url"`
	ResponseStatus   int    `json:"response_status"`
	ContentType      string `json:"content_type"`
	RequestBodySize  int64  `json:"request_body_size"`
	ResponseBodySize int64  `json:"response_body_size"`
	IsJSON           bool   `json:"is_json"`
	IsText           bool   `json:"is_text"`
	IsBinary         bool   `json:"is_binary"`
	IsStream         bool   `json:"is_stream"`
	IsWebSocket      bool   `json:"is_websocket"`
	ErrorMessage     string `json:"error_message"`
}

type LogFilter struct {
	Query       string
	Method      string
	Status      int
	StatusMin   int
	StatusMax   int
	RuleID      int64
	Category    string
	ContentType string
	From        string
	To          string
	OnlyErrors  bool
	IsJSON      *bool
	IsStream    *bool
	IsWebSocket *bool
	Limit       int
	Offset      int
}

type LogListResult struct {
	Items  []LogListItem `json:"items"`
	Total  int64         `json:"total"`
	Limit  int           `json:"limit"`
	Offset int           `json:"offset"`
}

type BodyContent struct {
	ContentType   string `json:"content_type"`
	Encoding      string `json:"encoding"`
	Truncated     bool   `json:"truncated"`
	Size          int64  `json:"size"`
	StoredBytes   int    `json:"stored_bytes"`
	OmittedReason string `json:"omitted_reason"`
	Body          string `json:"body"`
}

func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("database path is empty")
	}
	if err := ensureWritableDatabasePath(path); err != nil {
		return nil, err
	}
	handle, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}
	if err := handle.Ping(); err != nil {
		_ = handle.Close()
		return nil, fmt.Errorf("open sqlite database %q: %w", path, err)
	}
	handle.SetMaxOpenConns(1)
	store := &Store{db: handle}
	if err := store.migrate(context.Background()); err != nil {
		_ = handle.Close()
		return nil, err
	}
	return store, nil
}

func ensureWritableDatabasePath(path string) error {
	if path == ":memory:" {
		return nil
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create database directory %q: %w", dir, err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("stat database directory %q: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("database directory path %q is not a directory", dir)
	}

	if info, err := os.Stat(path); err == nil {
		if info.IsDir() {
			return fmt.Errorf("database path %q is a directory", path)
		}
		file, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			return fmt.Errorf("database file %q is not writable: %w", path, err)
		}
		_ = file.Close()
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat database file %q: %w", path, err)
	}

	temp, err := os.CreateTemp(dir, ".requestlens-db-write-test-*")
	if err != nil {
		return fmt.Errorf("database directory %q is not writable: %w", dir, err)
	}
	tempName := temp.Name()
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempName)
		return fmt.Errorf("close database write test file %q: %w", tempName, err)
	}
	if err := os.Remove(tempName); err != nil {
		return fmt.Errorf("remove database write test file %q: %w", tempName, err)
	}

	return nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) CleanupOldLogs(ctx context.Context, retentionDays int) error {
	if retentionDays <= 0 {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM http_logs WHERE started_at < datetime('now', ?)`, fmt.Sprintf("-%d days", retentionDays))
	return err
}

func (s *Store) migrate(ctx context.Context) error {
	statements := []string{
		`PRAGMA foreign_keys = ON`,
		`PRAGMA journal_mode = WAL`,
		`CREATE TABLE IF NOT EXISTS proxy_rules (
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
			max_body_size INTEGER NOT NULL DEFAULT 0,
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
		)`,
		`CREATE INDEX IF NOT EXISTS idx_proxy_rules_enabled_prefix ON proxy_rules(enabled, prefix)`,
		`CREATE TABLE IF NOT EXISTS http_logs (
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
			request_content_type TEXT NOT NULL DEFAULT '',
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
		)`,
		`CREATE INDEX IF NOT EXISTS idx_http_logs_started_at ON http_logs(started_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_http_logs_rule_id ON http_logs(rule_id)`,
		`CREATE INDEX IF NOT EXISTS idx_http_logs_method ON http_logs(method)`,
		`CREATE INDEX IF NOT EXISTS idx_http_logs_status ON http_logs(response_status)`,
		`CREATE INDEX IF NOT EXISTS idx_http_logs_path ON http_logs(path)`,
		`CREATE INDEX IF NOT EXISTS idx_http_logs_request_id ON http_logs(request_id)`,
	}
	for _, stmt := range statements {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if err := s.runVersionedMigrations(ctx); err != nil {
		return err
	}
	return nil
}

func (s *Store) runVersionedMigrations(ctx context.Context) error {
	var version int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version < 1 {
		if _, err := s.db.ExecContext(ctx, `UPDATE proxy_rules SET max_body_size = 0 WHERE max_body_size = 262144`); err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, `PRAGMA user_version = 1`); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ListRules(ctx context.Context, filter RuleFilter) ([]ProxyRule, error) {
	query := ruleSelect + ` WHERE 1=1`
	args := []any{}
	if filter.Category != "" {
		query += ` AND category = ?`
		args = append(args, filter.Category)
	}
	if filter.Enabled != nil {
		query += ` AND enabled = ?`
		args = append(args, boolToInt(*filter.Enabled))
	}
	query += ` ORDER BY enabled DESC, category ASC, prefix ASC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rules := []ProxyRule{}
	for rows.Next() {
		rule, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func (s *Store) GetRule(ctx context.Context, id int64) (ProxyRule, error) {
	row := s.db.QueryRowContext(ctx, ruleSelect+` WHERE id = ?`, id)
	return scanRule(row)
}

func (s *Store) CreateRule(ctx context.Context, rule ProxyRule) (ProxyRule, error) {
	result, err := s.db.ExecContext(ctx, `INSERT INTO proxy_rules (
		name, prefix, target_base_url, category, enabled,
		capture_request_headers, capture_response_headers, capture_request_body, capture_response_body,
		max_body_size, allow_binary_preview, allow_stream_preview, redact_sensitive_headers,
		preserve_host, cors_mode, rewrite_redirect_location, rewrite_cookie, timeout_ms
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rule.Name, rule.Prefix, rule.TargetBaseURL, rule.Category, boolToInt(rule.Enabled),
		boolToInt(rule.CaptureRequestHeaders), boolToInt(rule.CaptureResponseHeaders),
		boolToInt(rule.CaptureRequestBody), boolToInt(rule.CaptureResponseBody),
		rule.MaxBodySize, boolToInt(rule.AllowBinaryPreview), boolToInt(rule.AllowStreamPreview),
		boolToInt(rule.RedactSensitiveHeaders), boolToInt(rule.PreserveHost), rule.CorsMode,
		boolToInt(rule.RewriteRedirect), boolToInt(rule.RewriteCookie), rule.TimeoutMS)
	if err != nil {
		return ProxyRule{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return ProxyRule{}, err
	}
	return s.GetRule(ctx, id)
}

func (s *Store) UpdateRule(ctx context.Context, rule ProxyRule) (ProxyRule, error) {
	_, err := s.db.ExecContext(ctx, `UPDATE proxy_rules SET
		name = ?, prefix = ?, target_base_url = ?, category = ?, enabled = ?,
		capture_request_headers = ?, capture_response_headers = ?, capture_request_body = ?, capture_response_body = ?,
		max_body_size = ?, allow_binary_preview = ?, allow_stream_preview = ?, redact_sensitive_headers = ?,
		preserve_host = ?, cors_mode = ?, rewrite_redirect_location = ?, rewrite_cookie = ?, timeout_ms = ?,
		updated_at = datetime('now')
		WHERE id = ?`,
		rule.Name, rule.Prefix, rule.TargetBaseURL, rule.Category, boolToInt(rule.Enabled),
		boolToInt(rule.CaptureRequestHeaders), boolToInt(rule.CaptureResponseHeaders),
		boolToInt(rule.CaptureRequestBody), boolToInt(rule.CaptureResponseBody),
		rule.MaxBodySize, boolToInt(rule.AllowBinaryPreview), boolToInt(rule.AllowStreamPreview),
		boolToInt(rule.RedactSensitiveHeaders), boolToInt(rule.PreserveHost), rule.CorsMode,
		boolToInt(rule.RewriteRedirect), boolToInt(rule.RewriteCookie), rule.TimeoutMS, rule.ID)
	if err != nil {
		return ProxyRule{}, err
	}
	return s.GetRule(ctx, rule.ID)
}

func (s *Store) DeleteRule(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM proxy_rules WHERE id = ?`, id)
	return err
}

func (s *Store) SetRuleEnabled(ctx context.Context, id int64, enabled bool) (ProxyRule, error) {
	_, err := s.db.ExecContext(ctx, `UPDATE proxy_rules SET enabled = ?, updated_at = datetime('now') WHERE id = ?`, boolToInt(enabled), id)
	if err != nil {
		return ProxyRule{}, err
	}
	return s.GetRule(ctx, id)
}

func (s *Store) InsertLog(ctx context.Context, log HTTPLog) error {
	var ruleID any
	if log.RuleID != nil {
		ruleID = *log.RuleID
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO http_logs (
		rule_id, request_id, started_at, finished_at, duration_ms, method,
		original_url, proxied_url, scheme, host, path, query,
		request_headers, request_content_type, request_body, request_body_truncated, request_body_size, request_body_omitted_reason,
		response_status, response_headers, response_body, response_body_truncated, response_body_size, response_body_omitted_reason,
		content_type, is_json, is_text, is_binary, is_stream, is_websocket, error_message, client_ip
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ruleID, log.RequestID, log.StartedAt, log.FinishedAt, log.DurationMS, log.Method,
		log.OriginalURL, log.ProxiedURL, log.Scheme, log.Host, log.Path, log.Query,
		log.RequestHeaders, log.RequestContentType, log.RequestBody, boolToInt(log.RequestBodyTruncated),
		log.RequestBodySize, log.RequestBodyOmittedReason,
		nullableStatus(log.ResponseStatus), log.ResponseHeaders, log.ResponseBody, boolToInt(log.ResponseBodyTruncated),
		log.ResponseBodySize, log.ResponseBodyOmittedReason,
		log.ContentType, boolToInt(log.IsJSON), boolToInt(log.IsText), boolToInt(log.IsBinary),
		boolToInt(log.IsStream), boolToInt(log.IsWebSocket), log.ErrorMessage, log.ClientIP)
	return err
}

func (s *Store) ListLogs(ctx context.Context, filter LogFilter) (LogListResult, error) {
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Limit > 500 {
		filter.Limit = 500
	}
	where, args := buildLogWhere(filter)

	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM http_logs l LEFT JOIN proxy_rules r ON r.id = l.rule_id `+where, args...).Scan(&total); err != nil {
		return LogListResult{}, err
	}

	query := `SELECT
		l.id, l.rule_id, COALESCE(r.name, ''), COALESCE(r.category, ''),
		l.request_id, l.started_at, COALESCE(l.finished_at, ''), COALESCE(l.duration_ms, 0),
		l.method, l.original_url, COALESCE(l.proxied_url, ''), COALESCE(l.response_status, 0),
		l.content_type, l.request_body_size, l.response_body_size,
		l.is_json, l.is_text, l.is_binary, l.is_stream, l.is_websocket, l.error_message
		FROM http_logs l
		LEFT JOIN proxy_rules r ON r.id = l.rule_id ` + where + `
		ORDER BY l.started_at DESC, l.id DESC
		LIMIT ? OFFSET ?`
	args = append(args, filter.Limit, filter.Offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return LogListResult{}, err
	}
	defer rows.Close()

	items := []LogListItem{}
	for rows.Next() {
		var item LogListItem
		var ruleID sql.NullInt64
		var isJSON, isText, isBinary, isStream, isWebSocket int
		if err := rows.Scan(
			&item.ID, &ruleID, &item.RuleName, &item.RuleCategory,
			&item.RequestID, &item.StartedAt, &item.FinishedAt, &item.DurationMS,
			&item.Method, &item.OriginalURL, &item.ProxiedURL, &item.ResponseStatus,
			&item.ContentType, &item.RequestBodySize, &item.ResponseBodySize,
			&isJSON, &isText, &isBinary, &isStream, &isWebSocket, &item.ErrorMessage,
		); err != nil {
			return LogListResult{}, err
		}
		if ruleID.Valid {
			item.RuleID = &ruleID.Int64
		}
		item.IsJSON = intToBool(isJSON)
		item.IsText = intToBool(isText)
		item.IsBinary = intToBool(isBinary)
		item.IsStream = intToBool(isStream)
		item.IsWebSocket = intToBool(isWebSocket)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return LogListResult{}, err
	}

	return LogListResult{Items: items, Total: total, Limit: filter.Limit, Offset: filter.Offset}, nil
}

func (s *Store) GetLog(ctx context.Context, id int64) (HTTPLog, error) {
	row := s.db.QueryRowContext(ctx, `SELECT
		l.id, l.rule_id, COALESCE(r.name, ''), COALESCE(r.category, ''),
		l.request_id, l.started_at, COALESCE(l.finished_at, ''), COALESCE(l.duration_ms, 0),
		l.method, l.original_url, COALESCE(l.proxied_url, ''), l.scheme, l.host, l.path, l.query,
		COALESCE(l.request_headers, ''), l.request_content_type, COALESCE(l.request_body, x''),
		l.request_body_truncated, l.request_body_size, l.request_body_omitted_reason,
		COALESCE(l.response_status, 0), COALESCE(l.response_headers, ''), COALESCE(l.response_body, x''),
		l.response_body_truncated, l.response_body_size, l.response_body_omitted_reason,
		l.content_type, l.is_json, l.is_text, l.is_binary, l.is_stream, l.is_websocket,
		l.error_message, l.client_ip
		FROM http_logs l
		LEFT JOIN proxy_rules r ON r.id = l.rule_id
		WHERE l.id = ?`, id)

	var log HTTPLog
	var ruleID sql.NullInt64
	var isJSON, isText, isBinary, isStream, isWebSocket int
	var reqTruncated, resTruncated int
	if err := row.Scan(
		&log.ID, &ruleID, &log.RuleName, &log.RuleCategory,
		&log.RequestID, &log.StartedAt, &log.FinishedAt, &log.DurationMS,
		&log.Method, &log.OriginalURL, &log.ProxiedURL, &log.Scheme, &log.Host, &log.Path, &log.Query,
		&log.RequestHeaders, &log.RequestContentType, &log.RequestBody,
		&reqTruncated, &log.RequestBodySize, &log.RequestBodyOmittedReason,
		&log.ResponseStatus, &log.ResponseHeaders, &log.ResponseBody,
		&resTruncated, &log.ResponseBodySize, &log.ResponseBodyOmittedReason,
		&log.ContentType, &isJSON, &isText, &isBinary, &isStream, &isWebSocket,
		&log.ErrorMessage, &log.ClientIP,
	); err != nil {
		return HTTPLog{}, err
	}
	if ruleID.Valid {
		log.RuleID = &ruleID.Int64
	}
	log.RequestBodyTruncated = intToBool(reqTruncated)
	log.ResponseBodyTruncated = intToBool(resTruncated)
	log.IsJSON = intToBool(isJSON)
	log.IsText = intToBool(isText)
	log.IsBinary = intToBool(isBinary)
	log.IsStream = intToBool(isStream)
	log.IsWebSocket = intToBool(isWebSocket)
	log.RequestBodyStoredBytes = len(log.RequestBody)
	log.ResponseBodyStoredBytes = len(log.ResponseBody)
	log.RequestBody = nil
	log.ResponseBody = nil
	return log, nil
}

func (s *Store) GetBody(ctx context.Context, id int64, kind string) (BodyContent, error) {
	var contentType string
	var body []byte
	var truncated int
	var size int64
	var omitted string
	var query string
	switch kind {
	case "request":
		query = `SELECT request_content_type, COALESCE(request_body, x''), request_body_truncated, request_body_size, request_body_omitted_reason FROM http_logs WHERE id = ?`
	case "response":
		query = `SELECT content_type, COALESCE(response_body, x''), response_body_truncated, response_body_size, response_body_omitted_reason FROM http_logs WHERE id = ?`
	default:
		return BodyContent{}, errors.New("invalid body kind")
	}
	if err := s.db.QueryRowContext(ctx, query, id).Scan(&contentType, &body, &truncated, &size, &omitted); err != nil {
		return BodyContent{}, err
	}
	return BodyContent{
		ContentType:   contentType,
		Encoding:      "utf-8",
		Truncated:     intToBool(truncated),
		Size:          size,
		StoredBytes:   len(body),
		OmittedReason: omitted,
		Body:          string(body),
	}, nil
}

func (s *Store) DeleteLog(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM http_logs WHERE id = ?`, id)
	return err
}

func (s *Store) DeleteLogs(ctx context.Context, before string, ruleID int64) (int64, error) {
	where := ` WHERE 1=1`
	args := []any{}
	if before != "" {
		where += ` AND started_at < ?`
		args = append(args, before)
	}
	if ruleID > 0 {
		where += ` AND rule_id = ?`
		args = append(args, ruleID)
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM http_logs`+where, args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func buildLogWhere(filter LogFilter) (string, []any) {
	where := ` WHERE 1=1`
	args := []any{}
	if filter.Query != "" {
		like := "%" + filter.Query + "%"
		where += ` AND (l.original_url LIKE ? OR l.proxied_url LIKE ? OR l.request_id LIKE ? OR l.error_message LIKE ?)`
		args = append(args, like, like, like, like)
	}
	if filter.Method != "" {
		where += ` AND l.method = ?`
		args = append(args, strings.ToUpper(filter.Method))
	}
	if filter.Status > 0 {
		where += ` AND l.response_status = ?`
		args = append(args, filter.Status)
	}
	if filter.StatusMin > 0 {
		where += ` AND l.response_status >= ?`
		args = append(args, filter.StatusMin)
	}
	if filter.StatusMax > 0 {
		where += ` AND l.response_status <= ?`
		args = append(args, filter.StatusMax)
	}
	if filter.RuleID > 0 {
		where += ` AND l.rule_id = ?`
		args = append(args, filter.RuleID)
	}
	if filter.Category != "" {
		where += ` AND r.category = ?`
		args = append(args, filter.Category)
	}
	if filter.ContentType != "" {
		where += ` AND l.content_type LIKE ?`
		args = append(args, "%"+filter.ContentType+"%")
	}
	if filter.From != "" {
		where += ` AND l.started_at >= ?`
		args = append(args, filter.From)
	}
	if filter.To != "" {
		where += ` AND l.started_at <= ?`
		args = append(args, filter.To)
	}
	if filter.OnlyErrors {
		where += ` AND (l.error_message <> '' OR l.response_status >= 400 OR l.response_status IS NULL)`
	}
	if filter.IsJSON != nil {
		where += ` AND l.is_json = ?`
		args = append(args, boolToInt(*filter.IsJSON))
	}
	if filter.IsStream != nil {
		where += ` AND l.is_stream = ?`
		args = append(args, boolToInt(*filter.IsStream))
	}
	if filter.IsWebSocket != nil {
		where += ` AND l.is_websocket = ?`
		args = append(args, boolToInt(*filter.IsWebSocket))
	}
	return where, args
}

type ruleScanner interface {
	Scan(dest ...any) error
}

const ruleSelect = `SELECT
	id, name, prefix, target_base_url, category, enabled,
	capture_request_headers, capture_response_headers, capture_request_body, capture_response_body,
	max_body_size, allow_binary_preview, allow_stream_preview, redact_sensitive_headers,
	preserve_host, cors_mode, rewrite_redirect_location, rewrite_cookie, timeout_ms,
	created_at, updated_at
	FROM proxy_rules`

func scanRule(scanner ruleScanner) (ProxyRule, error) {
	var rule ProxyRule
	var enabled, reqHeaders, resHeaders, reqBody, resBody int
	var binaryPreview, streamPreview, redact, preserveHost, rewriteRedirect, rewriteCookie int
	err := scanner.Scan(
		&rule.ID, &rule.Name, &rule.Prefix, &rule.TargetBaseURL, &rule.Category, &enabled,
		&reqHeaders, &resHeaders, &reqBody, &resBody,
		&rule.MaxBodySize, &binaryPreview, &streamPreview, &redact,
		&preserveHost, &rule.CorsMode, &rewriteRedirect, &rewriteCookie, &rule.TimeoutMS,
		&rule.CreatedAt, &rule.UpdatedAt,
	)
	if err != nil {
		return ProxyRule{}, err
	}
	rule.Enabled = intToBool(enabled)
	rule.CaptureRequestHeaders = intToBool(reqHeaders)
	rule.CaptureResponseHeaders = intToBool(resHeaders)
	rule.CaptureRequestBody = intToBool(reqBody)
	rule.CaptureResponseBody = intToBool(resBody)
	rule.AllowBinaryPreview = intToBool(binaryPreview)
	rule.AllowStreamPreview = intToBool(streamPreview)
	rule.RedactSensitiveHeaders = intToBool(redact)
	rule.PreserveHost = intToBool(preserveHost)
	rule.RewriteRedirect = intToBool(rewriteRedirect)
	rule.RewriteCookie = intToBool(rewriteCookie)
	return rule, nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func intToBool(value int) bool {
	return value != 0
}

func nullableStatus(status int) any {
	if status <= 0 {
		return nil
	}
	return status
}

func NowString() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
