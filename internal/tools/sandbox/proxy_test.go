package sandbox

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProxyBlocksDisallowedHTTP(t *testing.T) {
	var blocked atomic.Int32
	var blockedDomain, blockedCommand string

	p := NewDomainProxy([]string{"allowed.com"}, WithOnBlocked(func(domain, command string) {
		blocked.Add(1)
		blockedDomain = domain
		blockedCommand = command
	}))

	addr, err := p.Start()
	require.NoError(t, err)
	defer p.Stop()

	proxyURL, _ := url.Parse("http://" + addr)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}

	resp, err := client.Get("http://evil.com/path")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, int32(1), blocked.Load())
	assert.Equal(t, "evil.com", blockedDomain)
	assert.Equal(t, "GET http://evil.com/path", blockedCommand)
}

func TestProxyAllowsCONNECT(t *testing.T) {
	// Create a local TCP server as the CONNECT target.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// Echo back what we receive.
		buf := make([]byte, 64)
		n, _ := conn.Read(buf)
		conn.Write(buf[:n])
	}()

	targetAddr := ln.Addr().String()
	targetHost, _, _ := net.SplitHostPort(targetAddr)

	p := NewDomainProxy([]string{targetHost})
	addr, err := p.Start()
	require.NoError(t, err)
	defer p.Stop()

	// Issue CONNECT via raw TCP.
	conn, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	defer conn.Close()

	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", targetAddr, targetAddr)

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Tunnel should be established — send data through.
	_, err = conn.Write([]byte("hello"))
	require.NoError(t, err)

	buf := make([]byte, 5)
	_, err = io.ReadFull(br, buf)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(buf))
}

func TestProxyBlocksCONNECT(t *testing.T) {
	var blocked atomic.Int32

	p := NewDomainProxy([]string{"allowed.com"}, WithOnBlocked(func(domain, command string) {
		blocked.Add(1)
	}))

	addr, err := p.Start()
	require.NoError(t, err)
	defer p.Stop()

	conn, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	defer conn.Close()

	fmt.Fprintf(conn, "CONNECT evil.com:443 HTTP/1.1\r\nHost: evil.com:443\r\n\r\n")

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, int32(1), blocked.Load())
}

func TestProxyRuntimeAllow(t *testing.T) {
	// Start a real HTTP target server.
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer target.Close()

	targetURL, _ := url.Parse(target.URL)
	targetHost := targetURL.Hostname()

	p := NewDomainProxy([]string{}) // no domains allowed initially
	addr, err := p.Start()
	require.NoError(t, err)
	defer p.Stop()

	proxyURL, _ := url.Parse("http://" + addr)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}

	// Initially blocked.
	resp, err := client.Get(target.URL + "/test")
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	// Allow at runtime.
	p.AllowDomain(targetHost)

	// Now allowed.
	resp, err = client.Get(target.URL + "/test")
	require.NoError(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "ok", string(body))
}

func TestProxyShutdown(t *testing.T) {
	p := NewDomainProxy([]string{"example.com"})
	_, err := p.Start()
	require.NoError(t, err)

	err = p.Stop()
	assert.NoError(t, err)

	// Double-stop should be safe.
	err = p.Stop()
	assert.NoError(t, err)
}

func TestProxyPort(t *testing.T) {
	p := NewDomainProxy([]string{"example.com"})
	addr, err := p.Start()
	require.NoError(t, err)
	defer p.Stop()

	_, portStr, _ := net.SplitHostPort(addr)
	port := p.Port()
	assert.NotZero(t, port)
	assert.Equal(t, portStr, fmt.Sprintf("%d", port))
}
