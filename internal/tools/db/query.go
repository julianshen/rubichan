package dbtools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"

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

type QueryTool struct {
	workDir string
}

func NewQueryTool(workDir string) *QueryTool {
	return &QueryTool{workDir: workDir}
}

func (t *QueryTool) Name() string { return "db_query" }

func (t *QueryTool) Description() string {
	return "Execute a read-only SQL query against SQLite, Postgres, or MySQL and return a truncated result set."
}

func (t *QueryTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"engine": {"type": "string", "enum": ["sqlite", "postgres", "mysql"], "description": "Database engine"},
			"database": {"type": "string", "description": "SQLite file path or Postgres/MySQL DSN"},
			"query": {"type": "string", "description": "Read-only SQL query"},
			"params": {"type": "array", "description": "Optional positional query parameters"},
			"timeout_ms": {"type": "integer", "description": "Query timeout in milliseconds (max 120000)"},
			"max_rows": {"type": "integer", "description": "Maximum rows to return (default 50, max 200)"}
		},
		"required": ["engine", "database", "query"]
	}`)
}

func (t *QueryTool) Execute(ctx context.Context, input json.RawMessage) (tools.ToolResult, error) {
	var in queryInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}
	if err := validateQuery(in.Query); err != nil {
		return tools.ToolResult{Content: err.Error(), IsError: true}, nil
	}
	driverName, dsn, err := t.resolveConnection(in.Engine, in.Database)
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

func (t *QueryTool) resolveConnection(engine, database string) (string, string, error) {
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
		return "pgx", database, nil
	case "mysql":
		if strings.TrimSpace(database) == "" {
			return "", "", fmt.Errorf("database DSN is required")
		}
		sanitized, err := sanitizeMySQLDSN(database)
		if err != nil {
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
	"charset":      true,
	"collation":    true,
	"loc":          true,
	"parseTime":    true,
	"timeout":      true,
	"readTimeout":  true,
	"writeTimeout": true,
	"tls":          true,
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
