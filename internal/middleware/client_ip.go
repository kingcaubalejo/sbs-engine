package middleware

import (
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
)

var (
	trustedCIDROnce sync.Once
	trustedCIDRs    []*net.IPNet
)

// loadTrustedCIDRs parses TRUSTED_PROXY_CIDRS once. The env var is a
// comma-separated list of CIDR blocks (e.g. "10.0.0.0/8,172.16.0.0/12").
// When set, X-Forwarded-For is trusted only for requests originating from
// these networks — typically the ALB/CloudFront subnets.
func loadTrustedCIDRs() {
	trustedCIDROnce.Do(func() {
		raw := os.Getenv("TRUSTED_PROXY_CIDRS")
		if raw == "" {
			return
		}
		for _, c := range strings.Split(raw, ",") {
			c = strings.TrimSpace(c)
			if c == "" {
				continue
			}
			_, n, err := net.ParseCIDR(c)
			if err != nil {
				continue
			}
			trustedCIDRs = append(trustedCIDRs, n)
		}
	})
}

// clientIP returns the best-effort client IP for the request. When the
// remote address is inside a configured trusted-proxy CIDR, the leftmost
// non-empty entry of X-Forwarded-For is used. Otherwise r.RemoteAddr is
// used. Returns an empty string if neither can be parsed.
func clientIP(r *http.Request) string {
	loadTrustedCIDRs()

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}

	if len(trustedCIDRs) > 0 {
		ip := net.ParseIP(host)
		if ip != nil {
			for _, n := range trustedCIDRs {
				if n.Contains(ip) {
					if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
						parts := strings.Split(xff, ",")
						return strings.TrimSpace(parts[0])
					}
					break
				}
			}
		}
	}
	return host
}
