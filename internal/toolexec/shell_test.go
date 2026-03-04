package toolexec_test

import (
	"testing"

	"github.com/julianshen/rubichan/internal/toolexec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Parser tests ---

func TestParseCommandSimple(t *testing.T) {
	parts, err := toolexec.ParseCommand("git push origin main")
	require.NoError(t, err)
	require.Len(t, parts, 1)
	assert.Equal(t, "git", parts[0].Prefix)
	assert.Equal(t, "git push origin main", parts[0].Full)
}

func TestParseCommandCompound(t *testing.T) {
	parts, err := toolexec.ParseCommand("go test ./... && rm -rf /tmp")
	require.NoError(t, err)
	require.Len(t, parts, 2)

	assert.Equal(t, "go", parts[0].Prefix)
	assert.Equal(t, "go test ./...", parts[0].Full)

	assert.Equal(t, "rm", parts[1].Prefix)
	assert.Equal(t, "rm -rf /tmp", parts[1].Full)
}

func TestParseCommandPipeline(t *testing.T) {
	parts, err := toolexec.ParseCommand("cat file | grep error | wc -l")
	require.NoError(t, err)
	require.Len(t, parts, 3)

	assert.Equal(t, "cat", parts[0].Prefix)
	assert.Equal(t, "cat file", parts[0].Full)

	assert.Equal(t, "grep", parts[1].Prefix)
	assert.Equal(t, "grep error", parts[1].Full)

	assert.Equal(t, "wc", parts[2].Prefix)
	assert.Equal(t, "wc -l", parts[2].Full)
}

func TestParseCommandSubshell(t *testing.T) {
	parts, err := toolexec.ParseCommand("echo $(rm -rf /)")
	require.NoError(t, err)
	// Should find both echo and rm inside the command substitution.
	require.Len(t, parts, 2)

	assert.Equal(t, "echo", parts[0].Prefix)
	assert.Equal(t, "rm", parts[1].Prefix)
	assert.Equal(t, "rm -rf /", parts[1].Full)
}

func TestParseCommandEnvPrefix(t *testing.T) {
	parts, err := toolexec.ParseCommand("RAILS_ENV=prod rails db:migrate")
	require.NoError(t, err)
	require.Len(t, parts, 1)

	assert.Equal(t, "rails", parts[0].Prefix)
	assert.Equal(t, "rails db:migrate", parts[0].Full)
}

func TestParseCommandBashDashC(t *testing.T) {
	parts, err := toolexec.ParseCommand(`bash -c "npm install"`)
	require.NoError(t, err)
	// Should find npm from the re-parsed -c argument.
	found := false
	for _, p := range parts {
		if p.Prefix == "npm" {
			found = true
			assert.Equal(t, "npm install", p.Full)
		}
	}
	assert.True(t, found, "should find npm command from bash -c argument")
}

func TestParseCommandQuotedArgs(t *testing.T) {
	parts, err := toolexec.ParseCommand("rm '-rf' /")
	require.NoError(t, err)
	require.Len(t, parts, 1)

	assert.Equal(t, "rm", parts[0].Prefix)
	assert.Equal(t, "rm -rf /", parts[0].Full)
}

func TestParseCommandEmpty(t *testing.T) {
	parts, err := toolexec.ParseCommand("")
	require.NoError(t, err)
	assert.Nil(t, parts)
}

func TestParseCommandSemicolon(t *testing.T) {
	parts, err := toolexec.ParseCommand("echo hello; rm -rf /")
	require.NoError(t, err)
	require.Len(t, parts, 2)

	assert.Equal(t, "echo", parts[0].Prefix)
	assert.Equal(t, "echo hello", parts[0].Full)

	assert.Equal(t, "rm", parts[1].Prefix)
	assert.Equal(t, "rm -rf /", parts[1].Full)
}
