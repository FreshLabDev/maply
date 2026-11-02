// SPDX-License-Identifier: Apache-2.0
package round

import (
	"testing"
	"time"
)

// A phone's zone becomes the choice with the same clock all year, or none.
func TestMatchKeepsTheClock(t *testing.T) {
	z, err := LoadZones()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.November, 2, 12, 0, 0, 0, time.UTC)
	for device, want := range map[string]string{
		"Europe/Berlin":      "Europe/Berlin",
		"Europe/Paris":       "Europe/Berlin",
		"Europe/Warsaw":      "Europe/Berlin",
		"Europe/Riga":        "Europe/Kyiv",
		"Europe/Dublin":      "Europe/London",
		"Asia/Yekaterinburg": "",
		"Africa/Lagos":       "", // UTC+1 all year, Berlin moves in summer
		"Not/AZone":          "",
		"":                   "",
	} {
		if got := z.Match(device, now); got != want {
			t.Errorf("Match(%q) = %q, want %q", device, got, want)
		}
	}
}
