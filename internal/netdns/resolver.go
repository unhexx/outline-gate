// Package netdns resolves names via public DNS, not Docker's 127.0.0.11.
package netdns

import (
	"context"
	"net"
	"time"
)

var servers = []string{"1.1.1.1:53", "8.8.8.8:53", "77.88.8.8:53"}

// Resolver is a Go resolver that queries public resolvers over UDP.
func Resolver() *net.Resolver {
	return &net.Resolver{PreferGo: true, Dial: dialNS}
}

// Dialer dials TCP after resolving with Resolver.
func Dialer() *net.Dialer {
	return &net.Dialer{Timeout: 15 * time.Second, Resolver: Resolver()}
}

// LookupIP returns A/AAAA for host using public DNS.
func LookupIP(ctx context.Context, host string) ([]net.IP, error) {
	return Resolver().LookupIP(ctx, "ip", host)
}

func dialNS(ctx context.Context, network, _ string) (net.Conn, error) {
	d := net.Dialer{Timeout: 3 * time.Second}
	var last error
	for _, ns := range servers {
		c, err := d.DialContext(ctx, "udp", ns)
		if err == nil {
			return c, nil
		}
		last = err
	}
	return nil, last
}
