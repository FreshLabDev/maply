// SPDX-License-Identifier: Apache-2.0
package api

import (
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"time"
)

// limiter is a fixed window per key. It is crude on purpose: it stops a loop
// hammering /api/guess, which is all it is for.
type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	start  time.Time
	counts map[string]int
	now    func() time.Time
}

func newLimiter(max int, window time.Duration) *limiter {
	return &limiter{max: max, window: window, counts: map[string]int{}, now: time.Now}
}

func (l *limiter) allowKey(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if now.Sub(l.start) >= l.window {
		// Starting a fresh window also drops every old key.
		l.start, l.counts = now, map[string]int{}
	}
	l.counts[key]++
	return l.counts[key] <= l.max
}

func (l *limiter) allow(userID int64) bool {
	return l.allowKey(strconv.FormatInt(userID, 10))
}

// allowIP counts an IPv6 address by its /64: one home or phone gets a whole
// /64, and counting each address would give it 2^64 buckets.
func (l *limiter) allowIP(ip string) bool {
	return l.allowKey(ipKey(ip))
}

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

// clientIP trusts CF-Connecting-IP because the service is reachable only
// through the Cloudflare Tunnel; anything else is the socket's address.
func clientIP(r *http.Request) string {
	if ip := r.Header.Get("CF-Connecting-IP"); net.ParseIP(ip) != nil {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
