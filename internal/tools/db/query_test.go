package dbtools

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLiteQuery(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	dbPath := filepath.Join(workDir, "test.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`create table users (id integer primary key, name text); insert into users(name) values ('alice');`)
	require.NoError(t, err)

	tool := NewQueryTool(workDir)
	input := json.RawMessage(`{"engine":"sqlite","database":"test.db","query":"select name from users","max_rows":5}`)
	result, execErr := tool.Execute(context.Background(), input)
	require.NoError(t, execErr)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, `"name":"alice"`)
}

func TestRejectWriteQuery(t *testing.T) {
	t.Parallel()
	tool := NewQueryTool(t.TempDir())
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"engine":"sqlite","database":"test.db","query":"update users set name='x'"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "read-only")
}

func TestRejectPathEscape(t *testing.T) {
	t.Parallel()
	tool := NewQueryTool(t.TempDir())
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"engine":"sqlite","database":"../test.db","query":"select 1"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "workspace")
}

func TestRejectsWritableCTEInReadOnlyTransaction(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	dbPath := filepath.Join(workDir, "test.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`create table users (id integer primary key, name text); insert into users(name) values ('alice');`)
	require.NoError(t, err)

	tool := NewQueryTool(workDir)
	input := json.RawMessage(`{"engine":"sqlite","database":"test.db","query":"with changed as (update users set name = 'bob' where id = 1 returning name) select name from changed"}`)
	result, execErr := tool.Execute(context.Background(), input)
	require.NoError(t, execErr)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "query failed")
}

func TestSanitizePostgresDSN(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		dsn      string
		expected string
	}{
		{"uri no params", "postgres://user:pass@host/db", "postgres://user:pass@host/db"},
		{"uri safe params kept", "postgres://user:pass@host/db?sslmode=require&connect_timeout=10", "postgres://user:pass@host/db?connect_timeout=10&sslmode=require"},
		{"uri dangerous params stripped", "postgres://user:pass@host/db?sslkey=/tmp/key&sslmode=require", "postgres://user:pass@host/db?sslmode=require"},
		{"kv format basic", "host=localhost port=5432 dbname=mydb user=test", "host=localhost port=5432 dbname=mydb user=test"},
		{"kv format strips dangerous", "host=localhost sslkey=/tmp/key sslmode=require dbname=mydb", "host=localhost sslmode=require dbname=mydb"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := sanitizePostgresDSN(tc.dsn)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, got)
		})
	}
}

func TestExtractPostgresHost(t *testing.T) {
	t.Parallel()
	tests := []struct {
		dsn  string
		want string
	}{
		{"postgres://user:pass@myhost.com/db", "myhost.com"},
		{"postgres://user:pass@myhost.com:5432/db", "myhost.com"},
		{"host=myhost.com port=5432 dbname=mydb", "myhost.com"},
		{"host='myhost.com' port=5432 dbname=mydb", "myhost.com"},
		{"dbname=mydb user=test", ""},
	}
	for _, tc := range tests {
		t.Run(tc.dsn, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, extractPostgresHost(tc.dsn))
		})
	}
}

func TestExtractMySQLHost(t *testing.T) {
	t.Parallel()
	tests := []struct {
		dsn  string
		want string
	}{
		{"user:pass@tcp(myhost.com:3306)/db", "myhost.com"},
		{"user:pass@tcp(myhost.com)/db", "myhost.com"},
		{"user:pass@/db", ""},
	}
	for _, tc := range tests {
		t.Run(tc.dsn, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, extractMySQLHost(tc.dsn))
		})
	}
}

func TestSanitizeMySQLDSN(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		dsn      string
		expected string
	}{
		{"no params", "user:pass@tcp(host)/db", "user:pass@tcp(host)/db"},
		{"safe params kept", "user:pass@tcp(host)/db?charset=utf8&parseTime=true", "user:pass@tcp(host)/db?charset=utf8&parseTime=true"},
		{"dangerous params stripped", "user:pass@tcp(host)/db?allowAllFiles=true&charset=utf8", "user:pass@tcp(host)/db?charset=utf8"},
		{"all dangerous stripped", "user:pass@tcp(host)/db?allowAllFiles=true&allowCleartextPasswords=true", "user:pass@tcp(host)/db"},
		{"empty params", "user:pass@tcp(host)/db?", "user:pass@tcp(host)/db"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := sanitizeMySQLDSN(tc.dsn)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, got)
		})
	}
}
