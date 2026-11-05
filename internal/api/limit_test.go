// SPDX-License-Identifier: Apache-2.0
package api

import (
	"testing"
	"time"
)

// An IPv6 address counts by its /64, which one home or phone gets whole;
// IPv4, mapped or not, counts by the address.
func TestIPv6CountsByItsSlash64(t *testing.T) {
	l := newLimiter(2, time.Minute)
	for _, ip := range []string{"2001:db8:1:2::1", "2001:db8:1:2:ffff::9"} {
		if !l.allowIP(ip) {
			t.Fatalf("%s refused", ip)
		}
	}
	if l.allowIP("2001:db8:1:2:abcd::5") {
		t.Fatal("a third address in the same /64 was let through")
	}
	if !l.allowIP("2001:db8:1:3::1") {
		t.Fatal("the next /64 was refused")
	}
	for in, want := range map[string]string{
		"203.0.113.7":        "203.0.113.7",
		"::ffff:203.0.113.7": "::ffff:203.0.113.7",
		"2001:db8::1":        "2001:db8::/64",
		"not an address":     "not an address",
	} {
		if got := ipKey(in); got != want {
			t.Errorf("ipKey(%q) = %q, want %q", in, got, want)
		}
	}
}
