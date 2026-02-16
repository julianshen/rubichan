package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveAPIKeyFromEnv(t *testing.T) {
	t.Setenv("TEST_API_KEY", "sk-test-12345")
	key, err := ResolveAPIKey("env", "", "TEST_API_KEY")
	require.NoError(t, err)
	assert.Equal(t, "sk-test-12345", key)
}

func TestResolveAPIKeyFromConfig(t *testing.T) {
	key, err := ResolveAPIKey("config", "sk-from-config", "")
	require.NoError(t, err)
	assert.Equal(t, "sk-from-config", key)
}

func TestResolveAPIKeyMissingEnvVar(t *testing.T) {
	_, err := ResolveAPIKey("env", "", "NONEXISTENT_KEY_VAR")
	assert.Error(t, err)
}

func TestResolveAPIKeyEmptyConfig(t *testing.T) {
	_, err := ResolveAPIKey("config", "", "")
	assert.Error(t, err)
}
