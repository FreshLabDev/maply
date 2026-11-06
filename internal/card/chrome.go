// SPDX-License-Identifier: Apache-2.0

package card

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/remote"
)

const (
	// Size is the card's side in pixels.
	Size = 1280
	// A card that takes longer than this is not worth waiting for: the result
	// goes out with the photo instead.
	renderTimeout = 20 * time.Second
	// The renderer is software-rendered WebGL on a small host; two pages at
	// once already slow each other down.
	maxRenders = 2
)

// Shot turns a page into a picture. Chrome is the real one; tests use a fake.
type Shot interface {
	Screenshot(ctx context.Context, pageURL string) ([]byte, error)
}

// Chrome drives a headless Chromium that is already running, over the Chrome
// DevTools Protocol. The browser lives in its own container; this side only
// opens a tab, waits for the card to say it is drawn and takes the picture.
type Chrome struct {
	// URL is the browser's DevTools endpoint, e.g. http://card-renderer:9222.
	URL     string
	Timeout time.Duration

	slots chan struct{}

	mu   sync.Mutex
	conn context.Context // holds the connection to the browser; tabs are its children
	stop func()
}

func NewChrome(endpoint string) *Chrome {
	return &Chrome{URL: endpoint, Timeout: renderTimeout, slots: make(chan struct{}, maxRenders)}
}

// ErrNotDrawn means the card page loaded but reported that it could not draw
// the map, usually because the tiles did not arrive.
var ErrNotDrawn = errors.New("card page did not draw the map")

// errPage marks failures of the page itself, as opposed to the connection:
// the browser is fine and the next card can use the same connection.
type errPage struct{ error }

func (e errPage) Unwrap() error { return e.error }

// browser returns the live connection, opening it when there is none. The
// first context of a chromedp allocator attaches to the browser's existing
// tab and would close the browser with it, so it is only the holder of the
// connection; every card gets a tab of its own below it.
func (c *Chrome) browser(ctx context.Context) (context.Context, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil && c.conn.Err() == nil {
		return c.conn, nil
	}
	if c.stop != nil {
		c.stop()
		c.conn, c.stop = nil, nil
	}
	// The allocator asks the browser for its WebSocket address and resolves
	// the container's name to an IP first: Chrome answers DevTools requests
	// only when Host is an IP address or localhost. A deadline here would
	// outlive the connect and kill the browser connection with it, so the
	// wait is bounded from the outside.
	allocCtx, cancelAlloc := remote.NewAllocator(context.Background(), c.URL)
	conn, cancelConn := chromedp.NewContext(allocCtx)
	stop := func() { cancelConn(); cancelAlloc() }
	done := make(chan error, 1)
	go func() { done <- chromedp.Do(conn) }()
	select {
	case err := <-done:
		if err != nil {
			stop()
			return nil, fmt.Errorf("connect to the renderer: %w", err)
		}
	case <-ctx.Done():
		stop()
		return nil, fmt.Errorf("connect to the renderer: %w", ctx.Err())
	}
	c.conn, c.stop = conn, stop
	return conn, nil
}

// drop forgets conn after a failure that may have been the connection's, so
// the next card reconnects.
func (c *Chrome) drop(conn context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == conn && c.stop != nil {
		c.stop()
		c.conn, c.stop = nil, nil
	}
}

func (c *Chrome) Screenshot(ctx context.Context, pageURL string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	case <-ctx.Done():
		return nil, fmt.Errorf("waiting for the renderer: %w", ctx.Err())
	}
	conn, err := c.browser(ctx)
	if err != nil {
		return nil, err
	}
	png, err := c.shoot(ctx, conn, pageURL)
	var page errPage
	if err != nil && !errors.As(err, &page) && ctx.Err() == nil {
		c.drop(conn)
	}
	return png, err
}

func (c *Chrome) shoot(ctx, conn context.Context, pageURL string) ([]byte, error) {
	// The tab lives and dies with this context: cancelling it closes the tab.
	// It is a child of the connection for its tree of values and of ctx for
	// its deadline.
	tabCtx, cancelTab := chromedp.NewContext(conn)
	defer cancelTab()
	stopTimer := context.AfterFunc(ctx, cancelTab)
	defer stopTimer()

	if err := chromedp.Do(tabCtx, chromedp.EmulateViewport(Size, Size, chromedp.EmulateScale(1))); err != nil {
		return nil, fmt.Errorf("render card: %w", err)
	}
	resp, err := chromedp.Run(tabCtx, chromedp.NavigateResponse(pageURL))
	if err != nil {
		return nil, fmt.Errorf("render card: %w", err)
	}
	if resp != nil && resp.Status != http.StatusOK {
		return nil, errPage{fmt.Errorf("card page answered %d", resp.Status)}
	}
	// card.js sets data-ready on <html> to "yes" once the map is drawn, or to
	// "failed" when it cannot be.
	state, err := chromedp.Run(tabCtx, chromedp.Poll[string](`document.documentElement.dataset.ready`,
		chromedp.WithPollingInterval(100*time.Millisecond), chromedp.WithPollingTimeout(c.Timeout)))
	if err != nil {
		return nil, fmt.Errorf("wait for card: %w", err)
	}
	if state != "yes" {
		return nil, errPage{ErrNotDrawn}
	}
	png, err := chromedp.Run(tabCtx, chromedp.CaptureScreenshot())
	if err != nil {
		return nil, fmt.Errorf("capture card: %w", err)
	}
	return png, nil
}
