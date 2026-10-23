// SPDX-License-Identifier: Apache-2.0

package card

import (
	"context"
	"testing"
	"time"
)

// With nothing listening the renderer must fail quickly, not hang the result.
func TestChromeFailsFastWhenTheBrowserIsGone(t *testing.T) {
	c := NewChrome("http://127.0.0.1:1")
	c.Timeout = 5 * time.Second
	start := time.Now()
	if _, err := c.Screenshot(context.Background(), "http://127.0.0.1:2/card"); err == nil {
		t.Fatal("a screenshot came from nowhere")
	}
	if time.Since(start) > 4*time.Second {
		t.Fatalf("failure took %v", time.Since(start))
	}
}
