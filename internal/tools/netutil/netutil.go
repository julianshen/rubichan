// Package netutil provides shared network utility functions for tool packages.
package netutil

import "net"

// IsPrivateAddress returns true if the given IP is loopback, private, link-local,
// multicast, or unspecified — addresses that should be blocked in SSRF checks.
func IsPrivateAddress(ip net.IP) bool {
	if ip == nil {
		return true
	}
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified()
}
