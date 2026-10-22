// SPDX-License-Identifier: Apache-2.0
//go:build integration

// A day played against the real store: three people in three zones, a clock
// moved by hand, and every response before the reveal checked for the place.
//
//	MAPLY_TEST_DATABASE_URL=postgres://… go test -tags=integration ./internal/api/
package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FreshLabDev/maply/internal/auth"
	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/migrations"
)

const (
	botToken = "123456:integration"
	// The place: Lviv. Its coordinates must not appear before the reveal.
	placeLat     = 49.8419
	placeLon     = 24.0315
	testImageKey = "Zk3qT9xWm2LpV7aN"

	kyivGroup   = int64(-1005501)
	moscowGroup = int64(-1005502)
	minskGroup  = int64(-1005503)
)

type members map[[2]int64]bool

func (m members) IsMember(_ context.Context, _ round.Date, chatID, userID int64) (bool, error) {
	return m[[2]int64{chatID, userID}], nil
}

type harness struct {
	t      *testing.T
	logs   *strings.Builder
	srv    *Server
	h      http.Handler
	signer *auth.Signer
	now    time.Time
	date   round.Date
}

func setup(t *testing.T) *harness {
	t.Helper()
	rawURL := os.Getenv("MAPLY_TEST_DATABASE_URL")
	if rawURL == "" {
		t.Skip("set MAPLY_TEST_DATABASE_URL to run the API day test")
	}
	if u, err := url.Parse(rawURL); err != nil || !strings.Contains(strings.TrimPrefix(u.Path, "/"), "test") {
		t.Fatalf("MAPLY_TEST_DATABASE_URL must name a disposable database containing \"test\"")
	}
	ctx := context.Background()
	store, err := db.Connect(ctx, rawURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	pool, err := pgxpool.New(ctx, rawURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	bootstrap, err := os.ReadFile(filepath.Join("..", "..", "deploy", "core-init.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(bootstrap)); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx, migrations.FS); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`TRUNCATE maply.job, maply.chat_round, maply.guess, maply.chat_settings, maply.round, maply.place`,
		`TRUNCATE core.person, core.chat CASCADE`,
		`TRUNCATE core.user_language`,
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO maply.place (status, source, source_id, source_url, license, kind, lat, lon, image_key,
		                         country_iso, continent, city, fact, difficulty, reviewed_at, author, heading)
		VALUES ('approved', 'mapillary', '918273645', 'https://www.mapillary.com/app/?pKey=918273645',
		        'CC-BY-SA-4.0', 'photo', $1, $2, $3, 'UA', 'europe',
		        '{"uk":"Львів","ru":"Львов","be":"Львоў"}', '{"uk":"Факт","ru":"Факт","be":"Факт"}', 3, now(), 'someone', 164.5)`,
		placeLat, placeLon, testImageKey); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO maply.chat_settings (chat_id, kind, lang) VALUES
		($1, 'group', 'uk'), ($2, 'group', 'ru'), ($3, 'group', 'be')`, kyivGroup, moscowGroup, minskGroup); err != nil {
		t.Fatal(err)
	}

	zones, err := round.LoadZones()
	if err != nil {
		t.Fatal(err)
	}
	images := t.TempDir()
	if err := os.WriteFile(filepath.Join(images, testImageKey), []byte("\xff\xd8\xff\xe0 not really a jpeg"), 0o600); err != nil {
		t.Fatal(err)
	}
	hs := &harness{
		t:      t,
		logs:   &strings.Builder{},
		signer: auth.NewSigner(botToken),
		// A winter Thursday: Kyiv is UTC+2, Moscow and Minsk UTC+3.
		date: round.NewDate(2026, time.December, 10),
		now:  time.Date(2026, time.December, 10, 9, 0, 0, 0, time.UTC),
	}
	hs.srv = &Server{
		Store:    store,
		InitData: auth.NewVerifier(botToken),
		Payloads: hs.signer,
		Members: members{
			{kyivGroup, 1}: true, {moscowGroup, 1}: true, // Oksana is in both
			{moscowGroup, 2}: true, // Ivan
			{minskGroup, 3}:  true, // Alesia
			{kyivGroup, 5}:   true, // Dmitry speaks ru but plays in the Kyiv group
		},
		Zones:     zones,
		ImagesDir: images,
		Log:       slog.New(slog.NewTextHandler(hs.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Now:       func() time.Time { return hs.now },
	}
	hs.h = hs.srv.Handler()
	return hs
}

// initData signs a launch the way Telegram does. auth_date is the real clock:
// the verifier checks it against wall time, the game against hs.now.
func initData(userID int64, name, lang, startParam string) string {
	fields := map[string]string{
		"auth_date": strconv.FormatInt(time.Now().Unix(), 10),
		"user":      fmt.Sprintf(`{"id":%d,"first_name":%q,"language_code":%q}`, userID, name, lang),
	}
	if startParam != "" {
		fields["start_param"] = startParam
		fields["chat_type"] = "supergroup"
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, len(keys))
	values := url.Values{}
	for i, k := range keys {
		lines[i] = k + "=" + fields[k]
		values.Set(k, fields[k])
	}
	secret := hmacSHA256([]byte("WebAppData"), []byte(botToken))
	values.Set("hash", hex.EncodeToString(hmacSHA256(secret, []byte(strings.Join(lines, "\n")))))
	return values.Encode()
}

func hmacSHA256(key, msg []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(msg)
	return h.Sum(nil)
}

func (hs *harness) payload(chatID int64) string {
	raw, err := hs.signer.Sign(auth.Payload{Date: hs.date, ChatID: chatID})
	if err != nil {
		hs.t.Fatal(err)
	}
	return raw
}

func (hs *harness) do(method, path, auth, body string) (int, string) {
	hs.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if auth != "" {
		req.Header.Set("Authorization", "tma "+auth)
	}
	rr := httptest.NewRecorder()
	hs.h.ServeHTTP(rr, req)
	return rr.Code, rr.Body.String()
}

// leaks reports whether a response says where the place is.
func leaks(body string) bool {
	for _, s := range []string{
		strconv.FormatFloat(placeLat, 'f', -1, 64), strconv.FormatFloat(placeLon, 'f', -1, 64),
		"49.84", "24.03", "918273645", "mapillary", "Львів", "Львов", `"UA"`, "europe", `"km"`,
	} {
		if strings.Contains(body, s) {
			return true
		}
	}
	return false
}

func (hs *harness) expect(method, path, auth, body string, wantStatus int) string {
	hs.t.Helper()
	status, got := hs.do(method, path, auth, body)
	if status != wantStatus {
		hs.t.Fatalf("%s %s = %d %s, want %d", method, path, status, got, wantStatus)
	}
	return got
}

func TestADayAcrossZones(t *testing.T) {
	hs := setup(t)
	oksanaKyiv := initData(1, "Oksana", "uk", hs.payload(kyivGroup))
	oksanaMoscow := initData(1, "Oksana", "uk", hs.payload(moscowGroup))
	oksanaDM := initData(1, "Oksana", "uk", "")
	ivan := initData(2, "Ivan", "ru", hs.payload(moscowGroup))
	alesia := initData(3, "Alesia", "be", hs.payload(minskGroup))
	stranger := initData(4, "Stranger", "ru", hs.payload(kyivGroup))
	dmitryKyiv := initData(5, "Dmitry", "ru", hs.payload(kyivGroup))
	dmitryDM := initData(5, "Dmitry", "ru", "")

	var before []string
	record := func(body string) string { before = append(before, body); return body }

	// Morning. Oksana opens the Mini App from the Kyiv group, and once from
	// the Moscow one, so core knows both.
	today := record(hs.expect("GET", "/api/today", oksanaKyiv, "", http.StatusOK))
	record(hs.expect("GET", "/api/today", oksanaMoscow, "", http.StatusOK))
	record(hs.expect("GET", "/api/today", dmitryKyiv, "", http.StatusOK))
	record(hs.expect("POST", "/api/guess", dmitryDM, `{"lat":48,"lon":30}`, http.StatusOK))
	var td todayResponse
	if err := json.Unmarshal([]byte(today), &td); err != nil {
		t.Fatal(err)
	}
	// Oksana opened it from the Kyiv group: the zone is the group's, not the place's.
	if td.Day != 1 || td.Image != "/img/"+testImageKey || td.Answer != nil || td.Zone != "Europe/Kyiv" ||
		td.Heading == nil || *td.Heading != 164.5 {
		t.Fatalf("today = %+v", td)
	}

	// Strangers and forged links get nothing.
	record(hs.expect("GET", "/api/today", stranger, "", http.StatusForbidden))
	record(hs.expect("GET", "/api/today", initData(1, "Oksana", "uk", "forged-payload"), "", http.StatusUnauthorized))
	record(hs.expect("GET", "/api/today", "", "", http.StatusUnauthorized))

	// Ivan answers at noon; the answer is accepted and nothing else is said.
	if got := record(hs.expect("POST", "/api/guess", ivan, `{"lat":50.45,"lon":30.52}`, http.StatusOK)); strings.TrimSpace(got) != `{"status":"accepted"}` {
		t.Fatalf("guess answered %s", got)
	}
	record(hs.expect("POST", "/api/guess", ivan, `{"lat":10,"lon":10}`, http.StatusConflict))
	record(hs.expect("POST", "/api/guess", alesia, `{"lat":100,"lon":0}`, http.StatusBadRequest))

	// 16:30 UTC is 18:30 in Kyiv but 19:30 in Moscow. Alesia (Minsk, 19:30)
	// still has half an hour.
	hs.now = time.Date(2026, time.December, 10, 16, 30, 0, 0, time.UTC)
	record(hs.expect("POST", "/api/guess", alesia, `{"lat":53.9,"lon":27.56}`, http.StatusOK))
	record(hs.expect("GET", "/api/result", ivan, "", http.StatusForbidden))

	// 17:30 UTC: Moscow has revealed (20:30), Kyiv has not (19:30). Oksana's
	// window closed with Moscow's reveal, so the Kyiv group cannot buy her
	// another hour.
	hs.now = time.Date(2026, time.December, 10, 17, 30, 0, 0, time.UTC)
	record(hs.expect("POST", "/api/guess", oksanaKyiv, `{"lat":49.8,"lon":24}`, http.StatusConflict))
	record(hs.expect("GET", "/api/result", oksanaKyiv, "", http.StatusForbidden))
	// Her DM waits for the latest of her chats, Kyiv.
	record(hs.expect("GET", "/api/result", oksanaDM, "", http.StatusForbidden))
	// Dmitry's DM is on Moscow time, which has revealed, but his Kyiv group
	// is still guessing: the DM must not tell him first.
	record(hs.expect("GET", "/api/result", dmitryDM, "", http.StatusForbidden))

	for i, body := range before {
		if leaks(body) {
			t.Fatalf("response %d before the reveal says where the place is: %s", i, body)
		}
	}
	if leaks(hs.logs.String()) {
		t.Fatalf("the log before the reveal says where the place is:\n%s", hs.logs.String())
	}

	// The Moscow group sees the answer and its table.
	res := hs.expect("GET", "/api/result", ivan, "", http.StatusOK)
	var rr resultResponse
	if err := json.Unmarshal([]byte(res), &rr); err != nil {
		t.Fatal(err)
	}
	if rr.Place.Lat != placeLat || rr.Place.City != "Львов" || len(rr.Table) != 1 || !rr.Table[0].Me || rr.Table[0].Km == nil || *rr.Table[0].Km < 400 {
		t.Fatalf("Moscow result = %s", res)
	}
	// Without Natural Earth embedded nothing earns a country bonus yet.
	if rr.Table[0].Points == nil || *rr.Table[0].Points <= 0 || *rr.Table[0].Points > 3500 {
		t.Fatalf("Ivan's points = %v", rr.Table[0].Points)
	}

	// 18:00 UTC: Kyiv reveals too, and the DM with it.
	hs.now = time.Date(2026, time.December, 10, 18, 0, 0, 0, time.UTC)
	hs.expect("GET", "/api/result", oksanaKyiv, "", http.StatusOK)
	hs.expect("GET", "/api/result", oksanaDM, "", http.StatusOK)
	hs.expect("GET", "/api/result", dmitryDM, "", http.StatusOK)
}

func TestImages(t *testing.T) {
	hs := setup(t)
	req := httptest.NewRequest("GET", "/img/"+testImageKey, nil)
	rr := httptest.NewRecorder()
	hs.h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || rr.Header().Get("Last-Modified") != "" || !strings.Contains(rr.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("image: %d %v", rr.Code, rr.Header())
	}
	for _, path := range []string{"/img/short", "/img/..%2F..%2Fetc%2Fpasswd", "/img/" + strings.Repeat("a", 65), "/img/AAAAAAAAAAAAAAAAAAAA"} {
		rr := httptest.NewRecorder()
		hs.h.ServeHTTP(rr, httptest.NewRequest("GET", path, nil))
		if rr.Code != http.StatusNotFound {
			t.Fatalf("%s = %d, want 404", path, rr.Code)
		}
	}
}
