// SPDX-License-Identifier: Apache-2.0
package geo

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"fmt"
	"io"
)

// countriesGz is Natural Earth's 1:10m admin-0 countries from the Ukrainian
// point of view, simplified by gen_countries.go. Natural Earth is in the
// public domain.
//
//go:generate go run gen_countries.go -src ne_10m_admin_0_countries_ukr.geojson -o countries.geojson
//go:generate gzip -9 -n -f countries.geojson
//go:embed countries.geojson.gz
var countriesGz []byte

// World parses the embedded countries. It takes a fifth of a second and the
// result is read-only, so call it once at start.
func World() (*Countries, error) {
	zr, err := gzip.NewReader(bytes.NewReader(countriesGz))
	if err != nil {
		return nil, fmt.Errorf("countries: %w", err)
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("countries: %w", err)
	}
	return ParseCountries(raw)
}
