package netutil

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsPrivateAddress(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		ip      net.IP
		private bool
	}{
		{"nil", nil, true},
		{"loopback", net.ParseIP("127.0.0.1"), true},
		{"private 10.x", net.ParseIP("10.0.0.1"), true},
		{"private 192.168.x", net.ParseIP("192.168.1.1"), true},
		{"private 172.16.x", net.ParseIP("172.16.0.1"), true},
		{"link-local", net.ParseIP("169.254.1.1"), true},
		{"unspecified", net.ParseIP("0.0.0.0"), true},
		{"multicast", net.ParseIP("224.0.0.1"), true},
		{"public", net.ParseIP("93.184.216.34"), false},
		{"public ipv6", net.ParseIP("2606:2800:220:1:248:1893:25c8:1946"), false},
		{"loopback ipv6", net.ParseIP("::1"), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.private, IsPrivateAddress(tc.ip))
		})
	}
}
