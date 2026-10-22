// SPDX-License-Identifier: Apache-2.0
package geo

import (
	"sync"
	"testing"
)

var embedded = sync.OnceValues(World)

func TestWorld(t *testing.T) {
	w, err := embedded()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name      string
		lat, lon  float64
		iso       string
		continent string
	}{
		// Crimea is Ukraine.
		{"Simferopol", 44.9521, 34.1024, "UA", "europe"},
		{"Sevastopol", 44.6054, 33.5221, "UA", "europe"},
		{"Yalta", 44.4952, 34.1663, "UA", "europe"},
		{"Kerch", 45.3566, 36.4740, "UA", "europe"},
		{"Donetsk", 48.0159, 37.8028, "UA", "europe"},
		{"Kyiv", 50.4501, 30.5234, "UA", "europe"},
		{"Minsk", 53.9006, 27.5590, "BY", "europe"},
		// Russia is cut at the Urals.
		{"Moscow", 55.7558, 37.6173, "RU", "europe"},
		{"Perm", 58.0105, 56.2502, "RU", "europe"},
		{"Kaliningrad", 54.7104, 20.4522, "RU", "europe"},
		{"Yekaterinburg", 56.8389, 60.6057, "RU", "asia"},
		{"Vladivostok", 43.1155, 131.8855, "RU", "asia"},
		{"Chukotka, west of the antimeridian", 66.1, -170.5, "RU", "asia"},
		// Countries on two continents.
		{"Istanbul, Sultanahmet", 41.0055, 28.9768, "TR", "europe"},
		{"Istanbul, Üsküdar", 41.0230, 29.0150, "TR", "asia"},
		{"Ankara", 39.9334, 32.8597, "TR", "asia"},
		{"Paris", 48.8566, 2.3522, "FR", "europe"},
		{"Cayenne", 4.9224, -52.3135, "FR", "south_america"},
		{"Fort-de-France", 14.6161, -61.0588, "FR", "north_america"},
		{"Saint-Denis, Réunion", -20.8821, 55.4504, "FR", "africa"},
		{"New York", 40.7128, -74.0060, "US", "north_america"},
		{"Honolulu", 21.3069, -157.8583, "US", "oceania"},
		{"Kralendijk, Bonaire", 12.1443, -68.2655, "NL", "north_america"},
		// Towns a river apart.
		{"Narva", 59.3750, 28.1800, "EE", "europe"},
		{"Ivangorod", 59.3660, 28.2225, "RU", "europe"},
		{"Strasbourg", 48.5818, 7.7509, "FR", "europe"},
		{"Kehl", 48.5730, 7.8150, "DE", "europe"},
		{"Gorizia", 45.9370, 13.6100, "IT", "europe"},
		{"Nova Gorica", 45.9560, 13.6480, "SI", "europe"},
		{"El Paso", 31.7619, -106.4850, "US", "north_america"},
		{"Ciudad Juárez", 31.6904, -106.4245, "MX", "north_america"},
		// Taveuni lies across the antimeridian.
		{"Taveuni, east", -16.85, 179.98, "FJ", "oceania"},
		{"Taveuni, west", -16.85, -179.98, "FJ", "oceania"},
		{"South Pole", -89.9, 0, "AQ", "antarctica"},
	} {
		c, ok := w.Locate(Point{Lat: tc.lat, Lon: tc.lon})
		if !ok || c.ISO != tc.iso || c.Continent != tc.continent {
			t.Errorf("%s: got %+v, %v; want %s %s", tc.name, c, ok, tc.iso, tc.continent)
		}
	}
	for _, sea := range []Point{{Lat: 43.5, Lon: 34}, {Lat: 0, Lon: -30}, {Lat: 89.9, Lon: 0}} {
		if c, ok := w.Locate(sea); ok {
			t.Errorf("%+v is sea, got %s", sea, c.ISO)
		}
	}
}
