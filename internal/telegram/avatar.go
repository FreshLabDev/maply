// SPDX-License-Identifier: Apache-2.0
package telegram

import (
	"context"
	"fmt"
	"os"

	"github.com/FreshLabDev/tg"
)

// ProfilePhoto is the person's current profile photo in its smallest size
// of at least minSide pixels (Telegram keeps 160, 320 and 640), or ok false
// when they have none the bot may see.
func (c *Client) ProfilePhoto(ctx context.Context, userID int64, minSide int) (tg.Photo, bool, error) {
	out, err := call[struct {
		Photos [][]tg.Photo `json:"photos"`
	}](ctx, c, "getUserProfilePhotos", map[string]any{"user_id": userID, "limit": 1})
	if err != nil {
		return tg.Photo{}, false, err
	}
	if len(out.Photos) == 0 || len(out.Photos[0]) == 0 {
		return tg.Photo{}, false, nil
	}
	sizes := out.Photos[0]
	pick := sizes[len(sizes)-1]
	for _, s := range sizes {
		if min(s.Width, s.Height) >= minSide && s.Width < pick.Width {
			pick = s
		}
	}
	return pick, true, nil
}

// FileBytes downloads a file Telegram has, up to max bytes.
func (c *Client) FileBytes(ctx context.Context, fileID string, max int64) ([]byte, error) {
	f, err := c.GetFile(ctx, fileID)
	if err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp("", "maply-file-*")
	if err != nil {
		return nil, err
	}
	path := tmp.Name()
	tmp.Close()
	defer os.Remove(path)
	if err := c.DownloadToFile(ctx, f.FilePath, path, max); err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	return os.ReadFile(path)
}
