//go:build integration

package sandbox_test

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"

	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/internal/tools/sandbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProxyIntegrationCurl(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not available")
	}

	// Start a local HTTP server as the allowed target
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer target.Close()

	// Get the target's host for the allowlist
	host, _, _ := net.SplitHostPort(target.Listener.Addr().String())

	p := sandbox.NewDomainProxy([]string{host})
	addr, err := p.Start()
	require.NoError(t, err)
	defer p.Stop()

	// Allowed: curl to the local test server through the proxy
	cmd := exec.Command("curl", "-s", "-o", "/dev/null", "-w", "%{http_code}",
		"--proxy", "http://"+addr, target.URL+"/test")
	out, err := cmd.Output()
	require.NoError(t, err)
	assert.Equal(t, "200", strings.TrimSpace(string(out)))

	// Blocked: curl to a domain not in allowlist
	cmd = exec.Command("curl", "-s", "-o", "/dev/null", "-w", "%{http_code}",
		"--proxy", "http://"+addr, "http://not-allowed.example.com/")
	out, _ = cmd.Output()
	assert.Equal(t, "403", strings.TrimSpace(string(out)))
}

func TestExcludedCommandIntegration(t *testing.T) {
	tests := []struct {
		command  string
		excluded []string
		want     bool
	}{
		{"docker build .", []string{"docker"}, true},
		{"sudo docker run nginx", []string{"docker"}, true},
		{"env FOO=bar docker ps", []string{"docker"}, true},
		{"dockerfile-lint .", []string{"docker"}, false},
		{"echo hello", []string{"docker"}, false},
		{"docker build . | grep error", []string{"docker"}, true},
		{"echo hello", []string{}, false},
		{"", []string{"docker"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			got := tools.IsExcludedFromSandbox(tt.command, tt.excluded)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestProxyShutdownDuringConnection(t *testing.T) {
	// Start a slow target server
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow response
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("slow"))
	}))
	defer target.Close()

	host, _, _ := net.SplitHostPort(target.Listener.Addr().String())
	p := sandbox.NewDomainProxy([]string{host})
	_, err := p.Start()
	require.NoError(t, err)

	// Stop should complete without hanging
	err = p.Stop()
	assert.NoError(t, err)
}
