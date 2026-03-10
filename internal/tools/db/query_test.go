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
	tool := NewQueryTool(t.TempDir())
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"engine":"sqlite","database":"test.db","query":"update users set name='x'"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "read-only")
}

func TestRejectPathEscape(t *testing.T) {
	tool := NewQueryTool(t.TempDir())
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"engine":"sqlite","database":"../test.db","query":"select 1"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "workspace")
}
