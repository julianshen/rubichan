package a2atools

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/julianshen/rubichan/internal/tools/netutil"
)

// dialTimeout bounds a single dial attempt made by the SSRF-guarded client.
const dialTimeout = 10 * time.Second

// defaultA2AHTTPClient is used by DiscoverTool, CallTool, and DispatchTool
// whenever they aren't given an explicit httpClient override. It blocks
// requests to private, loopback, link-local, or otherwise internal
// addresses, mirroring internal/tools/http's SSRF protection so these
// agent-facing tools can't be aimed at internal infrastructure (e.g. a
// cloud metadata endpoint) via a raw URL or a misconfigured [[a2a.agents]]
// entry. The check applies uniformly to configured agents and raw URLs
// alike, matching internal/tools/http's own tools, which apply no
// allowlist exemption.
var defaultA2AHTTPClient = &http.Client{
	Transport: &ssrfGuardTransport{
		guarded: &http.Transport{DialContext: guardedDialContext},
	},
}

// httpClientOrDefault returns c if non-nil (the test seam used to reach
// testutil's hermetic mem:// fixtures), or defaultA2AHTTPClient otherwise.
func httpClientOrDefault(c *http.Client) *http.Client {
	if c != nil {
		return c
	}
	return defaultA2AHTTPClient
}

// ssrfGuardTransport applies the SSRF guard only to requests that actually
// touch the network. Non-HTTP(S) schemes — notably mem://, the in-memory
// transport internal/testutil registers on http.DefaultTransport for
// hermetic tests — never dial anything, so they're passed straight through
// to http.DefaultTransport rather than the restricted one, which has no
// knowledge of mem:// (RegisterProtocol only affects the *http.Transport
// instance it's called on).
type ssrfGuardTransport struct {
	guarded http.RoundTripper
}

func (t *ssrfGuardTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
		return http.DefaultTransport.RoundTrip(req)
	}
	return t.guarded.RoundTrip(req)
}

// guardedDialContext resolves the dial target, rejects it if any resolved
// address is private/local/link-local per netutil.IsPrivateAddress, and
// otherwise dials directly to the validated IP (rather than re-resolving
// via addr) so there's no TOCTOU gap between the check and the connection.
func guardedDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve host %q: %w", host, err)
	}
	for _, ip := range ips {
		if netutil.IsPrivateAddress(ip.IP) {
			return nil, fmt.Errorf("requests to private or local addresses are not allowed")
		}
	}

	d := &net.Dialer{Timeout: dialTimeout}
	var lastErr error
	for _, ip := range ips {
		conn, dialErr := d.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no addresses to connect to for host %q", host)
}
