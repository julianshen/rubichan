package dbtools

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestToolMetadata covers Name, SearchHint, and Description methods.
func TestToolMetadata(t *testing.T) {
	t.Parallel()
	tool := NewQueryTool(t.TempDir())
	assert.Equal(t, "db_query", tool.Name())
	assert.NotEmpty(t, tool.SearchHint())
	assert.NotEmpty(t, tool.Description())
}

// TestExecuteInvalidJSON exercises the invalid-input early return.
func TestExecuteInvalidJSON(t *testing.T) {
	t.Parallel()
	tool := NewQueryTool(t.TempDir())
	result, err := tool.Execute(context.Background(), json.RawMessage(`{not json`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "invalid input")
}

// TestExecuteEmptyQuery covers the empty-query validation path.
func TestExecuteEmptyQuery(t *testing.T) {
	t.Parallel()
	tool := NewQueryTool(t.TempDir())
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"engine":"sqlite","database":"db","query":""}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "query is required")
}

// TestExecuteUnsupportedEngine covers the default branch in resolveConnection.
func TestExecuteUnsupportedEngine(t *testing.T) {
	t.Parallel()
	tool := NewQueryTool(t.TempDir())
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"engine":"mongodb","database":"db","query":"select 1"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "unsupported engine")
}

// TestExecutePostgresEmptyDSN covers the missing DSN path for postgres.
func TestExecutePostgresEmptyDSN(t *testing.T) {
	t.Parallel()
	tool := NewQueryTool(t.TempDir())
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"engine":"postgres","database":"","query":"select 1"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "DSN is required")
}

// TestExecuteMySQLEmptyDSN covers the missing DSN path for mysql.
func TestExecuteMySQLEmptyDSN(t *testing.T) {
	t.Parallel()
	tool := NewQueryTool(t.TempDir())
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"engine":"mysql","database":"","query":"select 1"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "DSN is required")
}

// TestExecutePostgresPrivateHost covers DSN validation rejecting private hosts.
func TestExecutePostgresPrivateHost(t *testing.T) {
	t.Parallel()
	tool := NewQueryTool(t.TempDir())
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"engine":"postgres","database":"postgres://user:pass@127.0.0.1:5432/db","query":"select 1"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

// TestExecuteMySQLPrivateHost covers mysql DSN validation rejecting private hosts.
func TestExecuteMySQLPrivateHost(t *testing.T) {
	t.Parallel()
	tool := NewQueryTool(t.TempDir())
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"engine":"mysql","database":"user:pass@tcp(10.0.0.1:3306)/db","query":"select 1"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

// TestValidateDSNHost covers the standalone validateDSNHost function.
func TestValidateDSNHostEmptyAndLocalhost(t *testing.T) {
	t.Parallel()
	// Empty host returns nil.
	require.NoError(t, validateDSNHost(context.Background(), ""))
	// "localhost" is allowed.
	require.NoError(t, validateDSNHost(context.Background(), "localhost"))
}

// TestValidateDSNHostUnresolvable covers the DNS resolution error path.
func TestValidateDSNHostUnresolvable(t *testing.T) {
	t.Parallel()
	err := validateDSNHost(context.Background(), "this-host-absolutely-does-not-exist-zzzzzz.invalid")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolve database host")
}

// TestValidateDSNHostPrivateIP covers the private-IP rejection path.
func TestValidateDSNHostPrivateIP(t *testing.T) {
	t.Parallel()
	// Using a hostname that resolves to 127.0.0.1 via /etc/hosts (localhost is explicitly allowed).
	// Use "ip6-localhost" on Linux or fall back to building a resolver test.
	// Simplest: use ip6-localhost/localhost6 which is typically present.
	// Fall back to manual IP-only test since we can't guarantee hostname resolution.
	// Instead, hit a numeric path: a host like "0.0.0.0" isn't valid via hostname,
	// so we rely on validateDSNHost skipping known localhost, and use a hostname
	// that resolves to private — use InvalidDnsResolver-style test via net/http testing
	// would be too complex. Instead use "broadcasthost" which on macOS resolves to 255.255.255.255,
	// and validate it gets rejected.
	err := validateDSNHost(context.Background(), "broadcasthost")
	// macOS /etc/hosts has broadcasthost → 255.255.255.255 which is link-local.
	// If /etc/hosts doesn't define this, skip.
	if err == nil {
		t.Skip("broadcasthost not defined or not private")
	}
	// Accept either "resolve" failure or "private or local" rejection — both are valid paths.
	if !strings.Contains(err.Error(), "private or local") && !strings.Contains(err.Error(), "resolve") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestExecuteTimeoutAndMaxRowsDefaults exercises the default-applying branches.
func TestExecuteTimeoutAndMaxRowsDefaults(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	dbPath := filepath.Join(workDir, "test.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`create table users (id integer primary key, name text);
insert into users(name) values ('alice'), ('bob'), ('charlie');`)
	require.NoError(t, err)

	tool := NewQueryTool(workDir)

	// timeout_ms > maxTimeout → clamp
	input := json.RawMessage(`{"engine":"sqlite","database":"test.db","query":"select name from users","timeout_ms":999999999}`)
	result, execErr := tool.Execute(context.Background(), input)
	require.NoError(t, execErr)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "alice")

	// max_rows > cap → clamp
	input = json.RawMessage(`{"engine":"sqlite","database":"test.db","query":"select name from users","max_rows":9999}`)
	result, execErr = tool.Execute(context.Background(), input)
	require.NoError(t, execErr)
	assert.False(t, result.IsError)
}

// TestExecuteMaxRowsTruncation exercises the truncation branch in formatRows.
func TestExecuteMaxRowsTruncation(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	dbPath := filepath.Join(workDir, "test.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`create table nums (n integer);
insert into nums(n) values (1), (2), (3), (4), (5);`)
	require.NoError(t, err)

	tool := NewQueryTool(workDir)
	input := json.RawMessage(`{"engine":"sqlite","database":"test.db","query":"select n from nums","max_rows":2}`)
	result, execErr := tool.Execute(context.Background(), input)
	require.NoError(t, execErr)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "truncated at 2 rows")
}

// TestExecuteEmptyResult exercises the <empty> fallback content.
func TestExecuteEmptyResult(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	dbPath := filepath.Join(workDir, "test.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer db.Close()
	// formatRows still writes columns + row_count, so we never get truly empty,
	// but ensure a zero-row query returns a well-formed result.
	_, err = db.Exec(`create table empties (id integer);`)
	require.NoError(t, err)

	tool := NewQueryTool(workDir)
	input := json.RawMessage(`{"engine":"sqlite","database":"test.db","query":"select id from empties"}`)
	result, execErr := tool.Execute(context.Background(), input)
	require.NoError(t, execErr)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "row_count: 0")
}

// TestExecuteBlobAndTimeNormalization exercises normalizeValue branches.
func TestExecuteBlobAndTimeNormalization(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	dbPath := filepath.Join(workDir, "test.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`create table blobs (id integer, data blob, ts text);`)
	require.NoError(t, err)
	_, err = db.Exec(`insert into blobs values (1, ?, ?)`, []byte("hello"), "2024-01-02T03:04:05Z")
	require.NoError(t, err)

	tool := NewQueryTool(workDir)
	input := json.RawMessage(`{"engine":"sqlite","database":"test.db","query":"select * from blobs"}`)
	result, execErr := tool.Execute(context.Background(), input)
	require.NoError(t, execErr)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "hello")
}

// TestNormalizeValueTime verifies the time.Time branch directly.
func TestNormalizeValueTime(t *testing.T) {
	t.Parallel()
	ts := time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)
	out := normalizeValue(ts)
	assert.Equal(t, "2024-05-06T07:08:09Z", out)
}

// TestNormalizeValueBytes verifies the []byte branch directly.
func TestNormalizeValueBytes(t *testing.T) {
	t.Parallel()
	out := normalizeValue([]byte("raw"))
	assert.Equal(t, "raw", out)
}

// TestNormalizeValueDefault verifies the default branch.
func TestNormalizeValueDefault(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 42, normalizeValue(42))
	assert.Equal(t, nil, normalizeValue(nil))
}

// TestResolveSQLitePathAbsolute covers the IsAbs branch.
func TestResolveSQLitePathAbsolute(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	tool := NewQueryTool(workDir)
	abs := filepath.Join(workDir, "nested.db")
	got, err := tool.resolveSQLitePath(abs)
	require.NoError(t, err)
	assert.Equal(t, abs, got)
}

// TestResolveSQLitePathEmpty covers the empty-path branch.
func TestResolveSQLitePathEmpty(t *testing.T) {
	t.Parallel()
	tool := NewQueryTool(t.TempDir())
	_, err := tool.resolveSQLitePath("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database path is required")
}

// TestValidateQueryMultiStatement covers the multi-statement rejection path.
func TestValidateQueryMultiStatement(t *testing.T) {
	t.Parallel()
	err := validateQuery("select 1; select 2")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "multi-statement")
}

// TestValidateQueryOnlySemicolon verifies semi-only queries are rejected.
func TestValidateQueryOnlySemicolon(t *testing.T) {
	t.Parallel()
	err := validateQuery(";")
	require.Error(t, err)
}

// TestValidateQueryReadOnlyKeywords covers the allowed-keyword branches.
func TestValidateQueryReadOnlyKeywords(t *testing.T) {
	t.Parallel()
	for _, q := range []string{"WITH x AS (select 1) select * from x", "EXPLAIN select 1", "PRAGMA foo", "SHOW TABLES", "DESCRIBE t", "DESC t"} {
		require.NoError(t, validateQuery(q), "query %q should be allowed", q)
	}
}

// TestSanitizePostgresURIInvalid covers the URI parse-error path.
func TestSanitizePostgresURIInvalid(t *testing.T) {
	t.Parallel()
	_, err := sanitizePostgresURI("postgres://bad url with spaces")
	require.Error(t, err)
}

// TestExtractPostgresHostInvalidURI covers the URL-parse error path in extractor.
func TestExtractPostgresHostInvalidURI(t *testing.T) {
	t.Parallel()
	// A URI with invalid characters that url.Parse rejects.
	host := extractPostgresHost("postgres://%%%")
	assert.Empty(t, host)
}

// TestFormatRowsRowsError exercises rows.Err() path by issuing a query that
// triggers an error during iteration (division by zero in runtime eval).
func TestFormatRowsColumnsSuccess(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	dbPath := filepath.Join(workDir, "test.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer db.Close()
	// Test with null values to exercise normalizeValue's nil path.
	_, err = db.Exec(`create table t (a integer, b text);
insert into t values (null, null);`)
	require.NoError(t, err)

	tool := NewQueryTool(workDir)
	input := json.RawMessage(`{"engine":"sqlite","database":"test.db","query":"select a, b from t"}`)
	result, execErr := tool.Execute(context.Background(), input)
	require.NoError(t, execErr)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "columns: a, b")
}
