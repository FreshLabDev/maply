// SPDX-License-Identifier: Apache-2.0

package schedule

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/FreshLabDev/maply/internal/geo"
)

// OpenMeteo asks open-meteo.com, which needs no key, for the place's UTC
// offset and current temperature. Only the place's coordinates leave the
// server, never anything about a player.
type OpenMeteo struct {
	Client *http.Client
	// Base is the API root; a test points it elsewhere.
	Base string
}

func (o OpenMeteo) Now(ctx context.Context, p geo.Point) (time.Duration, *float64, error) {
	base := o.Base
	if base == "" {
		base = "https://api.open-meteo.com"
	}
	q := url.Values{
		"latitude":  {strconv.FormatFloat(p.Lat, 'f', 3, 64)},
		"longitude": {strconv.FormatFloat(p.Lon, 'f', 3, 64)},
		"current":   {"temperature_2m"},
		"timezone":  {"auto"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/forecast?"+q.Encode(), nil)
	if err != nil {
		return 0, nil, err
	}
	client := o.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("open-meteo: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, nil, fmt.Errorf("open-meteo: status %d", resp.StatusCode)
	}
	var body struct {
		Offset  *int `json:"utc_offset_seconds"`
		Current struct {
			Temperature *float64 `json:"temperature_2m"`
		} `json:"current"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, nil, fmt.Errorf("open-meteo: %w", err)
	}
	if body.Offset == nil {
		return 0, nil, fmt.Errorf("open-meteo: no utc offset")
	}
	return time.Duration(*body.Offset) * time.Second, body.Current.Temperature, nil
}
