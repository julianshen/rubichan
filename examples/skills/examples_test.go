package skills_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/julianshen/rubichan/internal/skills"
	"github.com/stretchr/testify/require"
)

func TestExampleManifestsValid(t *testing.T) {
	examples := []string{"kubernetes", "ddd-expert", "rfc-writer"}
	for _, name := range examples {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(name, "SKILL.yaml"))
			require.NoError(t, err)

			manifest, err := skills.ParseManifest(data)
			require.NoError(t, err)
			require.Equal(t, name, manifest.Name)
		})
	}
}
