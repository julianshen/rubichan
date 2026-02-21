package skills

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveVersionExact(t *testing.T) {
	available := []string{"1.0.0", "1.1.0", "2.0.0"}

	got, err := ResolveVersion("1.1.0", available)
	require.NoError(t, err)
	assert.Equal(t, "1.1.0", got)
}

func TestResolveVersionExactNotFound(t *testing.T) {
	available := []string{"1.0.0", "1.1.0", "2.0.0"}

	_, err := ResolveVersion("3.0.0", available)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestResolveVersionCaret(t *testing.T) {
	available := []string{"1.0.0", "1.1.0", "1.2.3", "2.0.0", "2.1.0"}

	// ^1.0.0 should match >=1.0.0, <2.0.0 — highest is 1.2.3
	got, err := ResolveVersion("^1.0.0", available)
	require.NoError(t, err)
	assert.Equal(t, "1.2.3", got)
}

func TestResolveVersionTilde(t *testing.T) {
	available := []string{"1.0.0", "1.0.5", "1.1.0", "1.2.0"}

	// ~1.0.0 should match >=1.0.0, <1.1.0 — highest is 1.0.5
	got, err := ResolveVersion("~1.0.0", available)
	require.NoError(t, err)
	assert.Equal(t, "1.0.5", got)
}

func TestResolveVersionRange(t *testing.T) {
	available := []string{"1.0.0", "1.5.0", "2.0.0", "2.5.0", "3.0.0"}

	// >=1.5.0 <3.0.0 — highest is 2.5.0
	got, err := ResolveVersion(">=1.5.0, <3.0.0", available)
	require.NoError(t, err)
	assert.Equal(t, "2.5.0", got)
}

func TestResolveVersionLatest(t *testing.T) {
	available := []string{"1.0.0", "2.0.0", "3.0.0"}

	got, err := ResolveVersion("latest", available)
	require.NoError(t, err)
	assert.Equal(t, "3.0.0", got)
}

func TestResolveVersionEmptyAvailable(t *testing.T) {
	_, err := ResolveVersion("^1.0.0", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no versions available")
}

func TestResolveVersionNoMatch(t *testing.T) {
	available := []string{"1.0.0", "1.1.0"}

	_, err := ResolveVersion("^2.0.0", available)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no version matching")
}

func TestResolveVersionPrerelease(t *testing.T) {
	available := []string{"1.0.0", "1.1.0-beta.1", "1.1.0", "2.0.0-rc.1"}

	// ^1.0.0 should prefer stable versions — highest stable is 1.1.0
	got, err := ResolveVersion("^1.0.0", available)
	require.NoError(t, err)
	assert.Equal(t, "1.1.0", got)
}

func TestResolveVersionLatestSkipsPrerelease(t *testing.T) {
	available := []string{"1.0.0", "2.0.0-rc.1", "1.5.0"}

	got, err := ResolveVersion("latest", available)
	require.NoError(t, err)
	assert.Equal(t, "1.5.0", got) // highest stable, not 2.0.0-rc.1
}

func TestResolveVersionLatestFallsBackToPrerelease(t *testing.T) {
	available := []string{"1.0.0-alpha.1", "2.0.0-rc.1"}

	got, err := ResolveVersion("latest", available)
	require.NoError(t, err)
	assert.Equal(t, "2.0.0-rc.1", got) // no stable versions, fall back to highest
}

func TestResolveVersionAllUnparseable(t *testing.T) {
	available := []string{"not-a-version", "also-bad", "!!!"}

	_, err := ResolveVersion("^1.0.0", available)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no valid semver versions")
	assert.Contains(t, err.Error(), "skipped unparseable")
}

func TestResolveVersionPartiallyUnparseable(t *testing.T) {
	available := []string{"junk", "1.0.0", "bad", "1.2.0"}

	got, err := ResolveVersion("^1.0.0", available)
	require.NoError(t, err)
	assert.Equal(t, "1.2.0", got)
}

func TestIsSemVerRange(t *testing.T) {
	assert.True(t, IsSemVerRange("^1.0.0"))
	assert.True(t, IsSemVerRange("~1.0.0"))
	assert.True(t, IsSemVerRange(">=1.0.0"))
	assert.True(t, IsSemVerRange(">=1.0.0, <2.0.0"))
	assert.False(t, IsSemVerRange("1.0.0"))
	assert.False(t, IsSemVerRange("latest"))
	assert.False(t, IsSemVerRange(""))
}
