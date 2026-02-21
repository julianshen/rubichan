package scanner

import (
	"context"
	"testing"

	"github.com/julianshen/rubichan/internal/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigScannerName(t *testing.T) {
	s := NewConfigScanner()
	assert.Equal(t, "config", s.Name())
}

func TestConfigScannerInterface(t *testing.T) {
	var _ security.StaticScanner = NewConfigScanner()
}

func TestConfigScannerDockerfileRoot(t *testing.T) {
	dir := t.TempDir()

	t.Run("explicit USER root", func(t *testing.T) {
		writeFile(t, dir, "Dockerfile", `FROM ubuntu:22.04
RUN apt-get update
USER root
CMD ["app"]
`)
		s := NewConfigScanner()
		findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
		require.NoError(t, err)
		require.NotEmpty(t, findings)

		found := false
		for _, f := range findings {
			if f.Title == "Container runs as root user" && f.CWE == "CWE-250" {
				found = true
				assert.Equal(t, security.SeverityMedium, f.Severity)
				assert.Equal(t, security.CategoryMisconfiguration, f.Category)
			}
		}
		assert.True(t, found, "expected a root user finding")
	})

	t.Run("missing USER directive", func(t *testing.T) {
		writeFile(t, dir, "Dockerfile.prod", `FROM ubuntu:22.04
RUN apt-get update
CMD ["app"]
`)
		s := NewConfigScanner()
		findings, err := s.Scan(context.Background(), security.ScanTarget{
			RootDir: dir,
			Files:   []string{"Dockerfile.prod"},
		})
		require.NoError(t, err)
		require.NotEmpty(t, findings)

		found := false
		for _, f := range findings {
			if f.Title == "Container runs as root user" {
				found = true
				assert.Contains(t, f.Description, "no USER directive")
			}
		}
		assert.True(t, found, "expected a missing USER finding")
	})

	t.Run("ADD with URL", func(t *testing.T) {
		writeFile(t, dir, "Dockerfile.dev", `FROM ubuntu:22.04
ADD https://example.com/app.tar.gz /opt/
USER nobody
CMD ["app"]
`)
		s := NewConfigScanner()
		findings, err := s.Scan(context.Background(), security.ScanTarget{
			RootDir: dir,
			Files:   []string{"Dockerfile.dev"},
		})
		require.NoError(t, err)

		found := false
		for _, f := range findings {
			if f.Title == "Prefer COPY over ADD with URLs" {
				found = true
				assert.Equal(t, security.SeverityLow, f.Severity)
			}
		}
		assert.True(t, found, "expected an ADD-with-URL finding")
	})
}

func TestConfigScannerK8sPrivileged(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "deploy.yaml", `apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
spec:
  template:
    spec:
      containers:
      - name: app
        securityContext:
          privileged: true
`)

	s := NewConfigScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.NotEmpty(t, findings)

	found := false
	for _, f := range findings {
		if f.Title == "Kubernetes container running in privileged mode" {
			found = true
			assert.Equal(t, security.SeverityHigh, f.Severity)
			assert.Equal(t, "CWE-250", f.CWE)
		}
	}
	assert.True(t, found, "expected a privileged mode finding")
}

func TestConfigScannerK8sHostNetwork(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pod.yaml", `apiVersion: v1
kind: Pod
metadata:
  name: my-pod
spec:
  hostNetwork: true
  containers:
  - name: app
    image: myapp:latest
`)

	s := NewConfigScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)

	found := false
	for _, f := range findings {
		if f.Title == "Kubernetes pod uses host network" {
			found = true
			assert.Equal(t, security.SeverityMedium, f.Severity)
		}
	}
	assert.True(t, found, "expected a hostNetwork finding")
}

func TestConfigScannerCISecrets(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".github/workflows/ci.yml", `name: CI
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    env:
      API_KEY: sk_live_abc123def456ghi789
    steps:
      - uses: actions/checkout@v3
`)

	s := NewConfigScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.NotEmpty(t, findings)

	found := false
	for _, f := range findings {
		if f.Title == "Potential secret in CI configuration" {
			found = true
			assert.Equal(t, security.SeverityHigh, f.Severity)
			assert.Equal(t, "CWE-798", f.CWE)
		}
	}
	assert.True(t, found, "expected a CI secrets finding")
}

func TestConfigScannerCISecretsIgnoresReferences(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".github/workflows/safe.yml", `name: CI
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    env:
      API_KEY: ${{ secrets.API_KEY }}
    steps:
      - uses: actions/checkout@v3
`)

	s := NewConfigScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{
		RootDir: dir,
		Files:   []string{".github/workflows/safe.yml"},
	})
	require.NoError(t, err)

	for _, f := range findings {
		assert.NotEqual(t, "Potential secret in CI configuration", f.Title,
			"should not flag secret references like ${{ secrets.X }}")
	}
}

func TestConfigScannerDebugMode(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.yaml", `server:
  port: 8080
  debug: true
`)

	s := NewConfigScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)

	found := false
	for _, f := range findings {
		if f.Title == "Debug mode enabled in configuration" {
			found = true
			assert.Equal(t, security.SeverityLow, f.Severity)
		}
	}
	assert.True(t, found, "expected a debug mode finding")
}

func TestConfigScannerPermissiveCORS(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "server.yaml", `server:
  port: 8080
  cors: "*"
`)

	s := NewConfigScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)

	found := false
	for _, f := range findings {
		if f.Title == "Permissive CORS configuration" {
			found = true
			assert.Equal(t, security.SeverityMedium, f.Severity)
		}
	}
	assert.True(t, found, "expected a permissive CORS finding")
}

func TestConfigScannerCleanFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Dockerfile", `FROM ubuntu:22.04
RUN apt-get update
COPY app /app
USER nobody
CMD ["/app"]
`)
	writeFile(t, dir, "deploy.yaml", `apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
spec:
  template:
    spec:
      containers:
      - name: app
        image: myapp:latest
        securityContext:
          runAsNonRoot: true
`)
	writeFile(t, dir, "config.yaml", `server:
  port: 8080
  debug: false
`)

	s := NewConfigScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	assert.Empty(t, findings, "clean config files should produce no findings")
}
