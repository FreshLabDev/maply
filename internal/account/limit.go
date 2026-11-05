// SPDX-License-Identifier: Apache-2.0
package account

import (
	"net"
	"net/http"
	"net/netip"
	"sync"
	"time"
)

// limiter is a fixed window per key, as the api package's: it stops a loop,
// which is all it is for.
type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	start  time.Time
	counts map[string]int
	now    func() time.Time
}

func newLimiter(max int, window time.Duration, now func() time.Time) *limiter {
	return &limiter{max: max, window: window, counts: map[string]int{}, now: now}
}

func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if now.Sub(l.start) >= l.window || now.Before(l.start) {
		l.start, l.counts = now, map[string]int{}
	}
	l.counts[key]++
	return l.counts[key] <= l.max
}

// ipKey counts an IPv6 address by its /64.
func ipKey(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil || addr.Unmap().Is4() {
		return ip
	}
	p, err := addr.Prefix(64)
	if err != nil {
		return ip
	}
	return p.String()
}

// clientIP trusts CF-Connecting-IP: the service is reachable only through
// the Cloudflare Tunnel. It is a key for the limiter and is never logged.
func clientIP(r *http.Request) string {
	if ip := r.Header.Get("CF-Connecting-IP"); net.ParseIP(ip) != nil {
		return ipKey(ip)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ipKey(host)
}
