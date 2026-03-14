package dbtools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"

	"github.com/julianshen/rubichan/internal/tools/netutil"

	"github.com/julianshen/rubichan/internal/tools"
)

const (
	defaultTimeout = 30 * time.Second
	maxTimeout     = 2 * time.Minute
	defaultMaxRows = 50
	maxRowsCap     = 200
)

type queryInput struct {
	Engine    string        `json:"engine"`
	Database  string        `json:"database"`
	Query     string        `json:"query"`
	Params    []interface{} `json:"params,omitempty"`
	TimeoutMS int           `json:"timeout_ms,omitempty"`
	MaxRows   int           `json:"max_rows,omitempty"`
}

// QueryTool executes read-only SQL queries against SQLite, Postgres, or MySQL.
type QueryTool struct {
	workDir string
}

// NewQueryTool creates a QueryTool rooted at the given working directory.
func NewQueryTool(workDir string) *QueryTool {
	return &QueryTool{workDir: workDir}
}

// Name returns the tool name.
func (t *QueryTool) Name() string { return "db_query" }

// Description returns a human-readable description of the tool.
func (t *QueryTool) Description() string {
	return "Execute a read-only SQL query against SQLite, Postgres, or MySQL and return a truncated result set."
}

// InputSchema returns the JSON schema for the tool's input.
func (t *QueryTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"engine": {"type": "string", "enum": ["sqlite", "postgres", "mysql"], "description": "Database engine"},
			"database": {"type": "string", "description": "SQLite file path or Postgres/MySQL DSN"},
			"query": {"type": "string", "description": "Read-only SQL query"},
			"params": {
				"type": "array",
				"description": "Optional positional query parameters",
				"items": {
					"anyOf": [
						{"type": "string"},
						{"type": "number"},
						{"type": "integer"},
						{"type": "boolean"},
						{"type": "null"}
					]
				}
			},
			"timeout_ms": {"type": "integer", "description": "Query timeout in milliseconds (max 120000)"},
			"max_rows": {"type": "integer", "description": "Maximum rows to return (default 50, max 200)"}
		},
		"required": ["engine", "database", "query"]
	}`)
}

// Execute runs a read-only SQL query with DSN sanitization and SSRF validation.
func (t *QueryTool) Execute(ctx context.Context, input json.RawMessage) (tools.ToolResult, error) {
	var in queryInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}
	if err := validateQuery(in.Query); err != nil {
		return tools.ToolResult{Content: err.Error(), IsError: true}, nil
	}
	driverName, dsn, err := t.resolveConnection(ctx, in.Engine, in.Database)
	if err != nil {
		return tools.ToolResult{Content: err.Error(), IsError: true}, nil
	}

	timeout := defaultTimeout
	if in.TimeoutMS > 0 {
		timeout = time.Duration(in.TimeoutMS) * time.Millisecond
		if timeout > maxTimeout {
			timeout = maxTimeout
		}
	}
	maxRows := in.MaxRows
	if maxRows <= 0 {
		maxRows = defaultMaxRows
	}
	if maxRows > maxRowsCap {
		maxRows = maxRowsCap
	}

	queryCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("open database: %s", err), IsError: true}, nil
	}
	defer db.Close()

	tx, err := db.BeginTx(queryCtx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("begin read-only transaction: %s", err), IsError: true}, nil
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(queryCtx, in.Query, in.Params...)
	if err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("query failed: %s", err), IsError: true}, nil
	}
	defer rows.Close()

	content, err := formatRows(rows, maxRows)
	if err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("format rows: %s", err), IsError: true}, nil
	}
	if content == "" {
		content = "<empty>"
	}
	return tools.ToolResult{Content: content}, nil
}

func (t *QueryTool) resolveConnection(ctx context.Context, engine, database string) (string, string, error) {
	switch engine {
	case "sqlite":
		resolved, err := t.resolveSQLitePath(database)
		if err != nil {
			return "", "", err
		}
		return "sqlite", resolved, nil
	case "postgres":
		if strings.TrimSpace(database) == "" {
			return "", "", fmt.Errorf("database DSN is required")
		}
		sanitized, err := sanitizePostgresDSN(database)
		if err != nil {
			return "", "", err
		}
		if err := validateDSNHost(ctx, extractPostgresHost(sanitized)); err != nil {
			return "", "", err
		}
		return "pgx", sanitized, nil
	case "mysql":
		if strings.TrimSpace(database) == "" {
			return "", "", fmt.Errorf("database DSN is required")
		}
		sanitized, err := sanitizeMySQLDSN(database)
		if err != nil {
			return "", "", err
		}
		if err := validateDSNHost(ctx, extractMySQLHost(sanitized)); err != nil {
			return "", "", err
		}
		return "mysql", sanitized, nil
	default:
		return "", "", fmt.Errorf("unsupported engine %q", engine)
	}
}

func (t *QueryTool) resolveSQLitePath(database string) (string, error) {
	if strings.TrimSpace(database) == "" {
		return "", fmt.Errorf("database path is required")
	}
	var abs string
	if filepath.IsAbs(database) {
		abs = filepath.Clean(database)
	} else {
		abs = filepath.Join(t.workDir, database)
	}
	abs, err := filepath.Abs(abs)
	if err != nil {
		return "", fmt.Errorf("invalid sqlite path: %w", err)
	}
	if !strings.HasPrefix(abs, t.workDir+string(filepath.Separator)) && abs != t.workDir {
		return "", fmt.Errorf("sqlite database path must stay within the workspace")
	}
	return abs, nil
}

func validateQuery(query string) error {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return fmt.Errorf("query is required")
	}
	trimmed = strings.TrimSuffix(trimmed, ";")
	if strings.Contains(trimmed, ";") {
		return fmt.Errorf("multi-statement queries are not allowed")
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return fmt.Errorf("query is required")
	}
	switch strings.ToUpper(fields[0]) {
	case "SELECT", "WITH", "EXPLAIN", "PRAGMA", "SHOW", "DESCRIBE", "DESC":
		return nil
	default:
		return fmt.Errorf("only read-only queries are allowed")
	}
}

func formatRows(rows *sql.Rows, maxRows int) (string, error) {
	cols, err := rows.Columns()
	if err != nil {
		return "", err
	}
	var out strings.Builder
	out.WriteString("columns: ")
	out.WriteString(strings.Join(cols, ", "))
	out.WriteString("\n")

	count := 0
	truncated := false
	for rows.Next() {
		if count >= maxRows {
			truncated = true
			break
		}
		values := make([]interface{}, len(cols))
		scanTargets := make([]interface{}, len(cols))
		for i := range values {
			scanTargets[i] = &values[i]
		}
		if err := rows.Scan(scanTargets...); err != nil {
			return "", err
		}
		rowMap := make(map[string]interface{}, len(cols))
		for i, col := range cols {
			rowMap[col] = normalizeValue(values[i])
		}
		encoded, err := json.Marshal(rowMap)
		if err != nil {
			return "", err
		}
		out.WriteString(string(encoded))
		out.WriteString("\n")
		count++
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	out.WriteString(fmt.Sprintf("row_count: %d", count))
	if truncated {
		out.WriteString(fmt.Sprintf("\n... row output truncated at %d rows", maxRows))
	}
	return strings.TrimRight(out.String(), "\n"), nil
}

// mysqlDSNAllowedParams lists the only DSN parameters that are safe to pass
// through. Everything else (e.g. allowAllFiles, allowCleartextPasswords) is
// stripped to prevent local-file-inclusion and credential-exfiltration attacks.
var mysqlDSNAllowedParams = map[string]bool{
	"charset":          true,
	"collation":        true,
	"loc":              true,
	"parseTime":        true,
	"timeout":          true,
	"readTimeout":      true,
	"writeTimeout":     true,
	"tls":              true,
	"maxAllowedPacket": true,
}

func sanitizeMySQLDSN(dsn string) (string, error) {
	// go-sql-driver DSN format: [user[:password]@][net[(addr)]]/dbname[?param1=value1&...]
	questionMark := strings.IndexByte(dsn, '?')
	if questionMark < 0 {
		return dsn, nil // no params — nothing to sanitize
	}
	base := dsn[:questionMark]
	paramStr := dsn[questionMark+1:]
	if paramStr == "" {
		return base, nil
	}
	var kept []string
	for _, pair := range strings.Split(paramStr, "&") {
		eqIdx := strings.IndexByte(pair, '=')
		var key string
		if eqIdx >= 0 {
			key = pair[:eqIdx]
		} else {
			key = pair
		}
		if mysqlDSNAllowedParams[key] {
			kept = append(kept, pair)
		}
	}
	if len(kept) == 0 {
		return base, nil
	}
	return base + "?" + strings.Join(kept, "&"), nil
}

// validateDSNHost resolves the given host and rejects connections to
// private/local IP addresses to prevent SSRF via database connections.
func validateDSNHost(ctx context.Context, host string) error {
	if host == "" || host == "localhost" {
		// localhost is allowed for local development databases.
		return nil
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("resolve database host %q: %w", host, err)
	}
	for _, addr := range addrs {
		if netutil.IsPrivateAddress(addr.IP) {
			return fmt.Errorf("database connections to private or local addresses are not allowed")
		}
	}
	return nil
}

// extractPostgresHost extracts the host from a Postgres DSN (URI or key=value).
func extractPostgresHost(dsn string) string {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return ""
		}
		return u.Hostname()
	}
	for _, kv := range splitPostgresKV(dsn) {
		if strings.HasPrefix(kv, "host=") {
			v := kv[len("host="):]
			return strings.Trim(v, "'")
		}
	}
	return ""
}

// extractMySQLHost extracts the host from a go-sql-driver MySQL DSN.
// Format: [user[:password]@][net[(addr)]]/dbname[?param=value]
func extractMySQLHost(dsn string) string {
	// Strip params
	if idx := strings.IndexByte(dsn, '?'); idx >= 0 {
		dsn = dsn[:idx]
	}
	// Strip dbname
	if idx := strings.LastIndexByte(dsn, '/'); idx >= 0 {
		dsn = dsn[:idx]
	}
	// Find addr in net[(addr)]
	if lparen := strings.IndexByte(dsn, '('); lparen >= 0 {
		if rparen := strings.IndexByte(dsn[lparen:], ')'); rparen >= 0 {
			addr := dsn[lparen+1 : lparen+rparen]
			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				return addr // no port, entire string is the host
			}
			return host
		}
	}
	return ""
}

// postgresDSNAllowedParams lists the only DSN parameters that are safe to pass
// through. Parameters like sslkey, sslcert, sslrootcert, and service can be
// used for credential exfiltration or SSRF attacks and are stripped.
var postgresDSNAllowedParams = map[string]bool{
	"sslmode":                             true,
	"connect_timeout":                     true,
	"application_name":                    true,
	"search_path":                         true,
	"timezone":                            true,
	"client_encoding":                     true,
	"options":                             true,
	"statement_timeout":                   true,
	"lock_timeout":                        true,
	"idle_in_transaction_session_timeout": true,
}

func sanitizePostgresDSN(dsn string) (string, error) {
	// Postgres DSN can be either URI format (postgres://...) or key=value format.
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		return sanitizePostgresURI(dsn)
	}
	return sanitizePostgresKeyValue(dsn)
}

func sanitizePostgresURI(dsn string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("invalid postgres DSN: %w", err)
	}
	q := u.Query()
	for key := range q {
		if !postgresDSNAllowedParams[key] {
			q.Del(key)
		}
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func sanitizePostgresKeyValue(dsn string) (string, error) {
	// key=value pairs separated by spaces; values may be single-quoted.
	var kept []string
	// Simple key=value allowlist approach — only keep known-safe keys.
	pairs := splitPostgresKV(dsn)
	for _, kv := range pairs {
		eqIdx := strings.IndexByte(kv, '=')
		if eqIdx < 0 {
			continue
		}
		key := strings.TrimSpace(kv[:eqIdx])
		// Always keep connection essentials.
		switch key {
		case "host", "port", "dbname", "user", "password":
			kept = append(kept, kv)
		default:
			if postgresDSNAllowedParams[key] {
				kept = append(kept, kv)
			}
		}
	}
	return strings.Join(kept, " "), nil
}

func splitPostgresKV(dsn string) []string {
	var parts []string
	var current strings.Builder
	inQuote := false
	for i := 0; i < len(dsn); i++ {
		ch := dsn[i]
		if ch == '\'' && !inQuote {
			inQuote = true
			current.WriteByte(ch)
		} else if ch == '\'' && inQuote {
			inQuote = false
			current.WriteByte(ch)
		} else if ch == ' ' && !inQuote {
			if current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
		} else {
			current.WriteByte(ch)
		}
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

func normalizeValue(v interface{}) interface{} {
	switch x := v.(type) {
	case []byte:
		return string(x)
	case time.Time:
		return x.Format(time.RFC3339Nano)
	default:
		return x
	}
}
