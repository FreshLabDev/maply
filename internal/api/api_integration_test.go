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
		`TRUNCATE maply.job, maply.chat_round, maply.guess, maply.site_guess, maply.site_player, maply.chat_settings, maply.round, maply.place, maply.avatar, maply.world_card`,
		`TRUNCATE core.person, core.chat CASCADE`,
		`TRUNCATE core.language_observation`,
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
	if _, err := pool.Exec(ctx, `INSERT INTO maply.chat_settings (chat_id, kind, timezone) VALUES
		($1, 'group', 'Europe/Kyiv'), ($2, 'group', 'Europe/Moscow'), ($3, 'group', 'Europe/Minsk'),
		-- Dmitry set his DM to Moscow; Russian alone would put it on Kyiv.
		(5, 'dm', 'Europe/Moscow')`, kyivGroup, moscowGroup, minskGroup); err != nil {
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
			{moscowGroup, 6}: true, // Pavel
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
	pavel := initData(6, "Pavel", "ru", hs.payload(moscowGroup))

	var before []string
	record := func(body string) string { before = append(before, body); return body }

	// Morning. Oksana opens the Mini App from the Kyiv group, and once from
	// the Moscow one, so core knows both.
	today := record(hs.expect("GET", "/api/today", oksanaKyiv, "", http.StatusOK))
	record(hs.expect("GET", "/api/today", oksanaMoscow, "", http.StatusOK))
	record(hs.expect("GET", "/api/today", dmitryKyiv, "", http.StatusOK))
	hs.expect("POST", "/api/guess", dmitryDM, `{"lat":48,"lon":30}`, http.StatusOK)
	var td todayResponse
	if err := json.Unmarshal([]byte(today), &td); err != nil {
		t.Fatal(err)
	}
	// Oksana opened it from the Kyiv group: the zone is the group's, not the place's.
	// Neither group has a language yet, so the app speaks hers.
	if td.Day != 1 || td.Image != "/img/"+testImageKey || td.Answer != nil || td.Zone != "Europe/Kyiv" ||
		td.Lang != "uk" || td.Heading == nil || *td.Heading != 164.5 {
		t.Fatalf("today = %+v", td)
	}

	// Strangers and forged links get nothing.
	record(hs.expect("GET", "/api/today", stranger, "", http.StatusForbidden))
	record(hs.expect("GET", "/api/today", initData(1, "Oksana", "uk", "forged-payload"), "", http.StatusUnauthorized))
	record(hs.expect("GET", "/api/today", "", "", http.StatusUnauthorized))

	// Pavel answers first from the Moscow group, Ivan at noon. Each sees the
	// place and their own score at once; the other is a line without a name.
	pavelBody := hs.expect("POST", "/api/guess", pavel, `{"lat":55.75,"lon":37.6}`, http.StatusOK)
	if !strings.Contains(pavelBody, `"phase":"live"`) || !strings.Contains(pavelBody, "Pavel") {
		t.Fatalf("Pavel's answer = %s", pavelBody)
	}
	got := hs.expect("POST", "/api/guess", ivan, `{"lat":50.45,"lon":30.52}`, http.StatusOK)
	var live resultResponse
	if err := json.Unmarshal([]byte(got), &live); err != nil {
		t.Fatal(err)
	}
	if live.Phase != "live" || live.Place.Lat != placeLat || live.OpensAt == nil || !live.OpensAt.Equal(time.Date(2026, time.December, 10, 17, 0, 0, 0, time.UTC)) ||
		len(live.Table) != 2 || live.World == nil || live.World.Players != 3 || live.World.Rank == 0 || len(live.World.Pins) != 3 {
		t.Fatalf("Ivan's answer = %s", got)
	}
	for _, l := range live.Table {
		if l.Me != (l.Name == "Ivan") || l.Km == nil || l.Points == nil {
			t.Fatalf("line = %+v", l)
		}
		if !l.Me && (l.Name != "" || l.Photo != "" || l.Bonus == nil || l.Speed != 0) {
			t.Fatalf("Pavel is named before the reveal: %+v", l)
		}
	}
	if strings.Contains(got, "Pavel") {
		t.Fatalf("Ivan's live result names Pavel: %s", got)
	}
	record(hs.expect("POST", "/api/guess", ivan, `{"lat":10,"lon":10}`, http.StatusConflict))
	record(hs.expect("POST", "/api/guess", alesia, `{"lat":100,"lon":0}`, http.StatusBadRequest))

	// 16:30 UTC is 18:30 in Kyiv but 19:30 in Moscow. Alesia (Minsk, 19:30)
	// still has half an hour.
	hs.now = time.Date(2026, time.December, 10, 16, 30, 0, 0, time.UTC)
	hs.expect("POST", "/api/guess", alesia, `{"lat":53.9,"lon":27.56}`, http.StatusOK)
	if got := hs.expect("GET", "/api/result", ivan, "", http.StatusOK); !strings.Contains(got, `"phase":"live"`) || strings.Contains(got, "Pavel") {
		t.Fatalf("Ivan's result before the reveal = %s", got)
	}

	// 17:30 UTC: Moscow has revealed (20:30), Kyiv has not (19:30). Oksana's
	// window closed with Moscow's reveal, so the Kyiv group cannot buy her
	// another hour.
	hs.now = time.Date(2026, time.December, 10, 17, 30, 0, 0, time.UTC)
	record(hs.expect("POST", "/api/guess", oksanaKyiv, `{"lat":49.8,"lon":24}`, http.StatusConflict))
	// She never answered, so nothing opens for her before the Kyiv reveal:
	// not in the group, not in her DM, not as a share.
	if got := record(hs.expect("GET", "/api/result", oksanaKyiv, "", http.StatusForbidden)); !strings.Contains(got, "not_answered") {
		t.Fatalf("Oksana's result = %s", got)
	}
	record(hs.expect("GET", "/api/result", oksanaDM, "", http.StatusForbidden))
	record(hs.expect("POST", "/api/share", oksanaKyiv, "", http.StatusServiceUnavailable))
	// Dmitry answered in the morning: his DM shows his result, live until the
	// Kyiv group has its reveal.
	if got := hs.expect("GET", "/api/result", dmitryDM, "", http.StatusOK); !strings.Contains(got, `"phase":"live"`) {
		t.Fatalf("Dmitry's DM = %s", got)
	}

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
	if rr.Phase != "final" || rr.Place.Lat != placeLat || rr.Place.City != "Львов" || len(rr.Table) != 2 || !strings.Contains(res, "Pavel") {
		t.Fatalf("Moscow result = %s", res)
	}
	ivanLine := rr.Table[0]
	if !ivanLine.Me {
		ivanLine = rr.Table[1]
	}
	// Without Natural Earth embedded nothing earns a country bonus yet.
	if !ivanLine.Me || ivanLine.Km == nil || *ivanLine.Km < 400 || ivanLine.Points == nil || *ivanLine.Points <= 0 || *ivanLine.Points > 3500 {
		t.Fatalf("Ivan's line = %+v", ivanLine)
	}

	// 18:00 UTC: Kyiv reveals too, and the DM with it.
	hs.now = time.Date(2026, time.December, 10, 18, 0, 0, 0, time.UTC)
	res = hs.expect("GET", "/api/result", oksanaKyiv, "", http.StatusOK)
	if err := json.Unmarshal([]byte(res), &rr); err != nil {
		t.Fatal(err)
	}
	if rr.Place.City != "Львів" {
		t.Fatalf("Oksana reads the city as %q, want it in Ukrainian", rr.Place.City)
	}
	hs.expect("GET", "/api/result", oksanaDM, "", http.StatusOK)
	hs.expect("GET", "/api/result", dmitryDM, "", http.StatusOK)

	// Once the Moscow group has a language, the app opened from it speaks
	// that language, the one the bot posts there in; the DM keeps hers.
	if err := hs.srv.Store.(*db.Store).SetChatLang(context.Background(), moscowGroup, "ru"); err != nil {
		t.Fatal(err)
	}
	for who, want := range map[string]string{oksanaMoscow: "ru", oksanaKyiv: "uk", oksanaDM: "uk"} {
		var td todayResponse
		if err := json.Unmarshal([]byte(hs.expect("GET", "/api/today", who, "", http.StatusOK)), &td); err != nil {
			t.Fatal(err)
		}
		if td.Lang != want {
			t.Fatalf("lang = %q, want %q", td.Lang, want)
		}
	}
	res = hs.expect("GET", "/api/result", oksanaMoscow, "", http.StatusOK)
	if !strings.Contains(res, `"city":"Львов"`) {
		t.Fatalf("Oksana in the Moscow group reads %s, want the city in Russian", res)
	}
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

// The site: no sign-in, the place only after the latest reveal among the
// zones maply offers, and no date that has not begun in one of them.
func TestSiteWaitsForTheLatestReveal(t *testing.T) {
	hs := setup(t)
	hs.srv.BotUsername = "maplyin_bot"
	// 11:00 UTC: already 11 December on Kiritimati, still the 10th in Sydney.
	hs.now = time.Date(2026, time.December, 10, 11, 0, 0, 0, time.UTC)
	var today todayResponse
	for _, tz := range []string{"Europe/Kyiv", "Pacific/Kiritimati", "Not/AZone", ""} {
		body := hs.expect("GET", "/api/site/today?lang=uk&tz="+url.QueryEscape(tz), "", "", http.StatusOK)
		if leaks(body) {
			t.Fatalf("site today for %q leaks the place: %s", tz, body)
		}
		if err := json.Unmarshal([]byte(body), &today); err != nil {
			t.Fatal(err)
		}
		if today.Date != "2026-12-10" || today.Bot != "maplyin_bot" || today.Image != "/img/"+testImageKey {
			t.Fatalf("site today for %q = %+v", tz, today)
		}
	}
	// Los Angeles is the last of the zones: 20:00 there is 04:00 UTC.
	latest := time.Date(2026, time.December, 11, 4, 0, 0, 0, time.UTC)
	if !today.Reveal.Equal(latest) {
		t.Fatalf("site reveal = %v, want %v", today.Reveal, latest)
	}
	kyiv := hs.expect("GET", "/api/site/today?tz=Europe/Kyiv", "", "", http.StatusOK)
	if err := json.Unmarshal([]byte(kyiv), &today); err != nil || !today.Deadline.Equal(time.Date(2026, time.December, 10, 18, 0, 0, 0, time.UTC)) {
		t.Fatalf("Kyiv deadline = %v (%v)", today.Deadline, err)
	}

	hs.now = latest.Add(-time.Minute)
	body := hs.expect("GET", "/api/site/result?date=2026-12-10&lat=50&lon=30", "", "", http.StatusForbidden)
	if leaks(body) || !strings.Contains(body, "opens_at") {
		t.Fatalf("site result before the latest reveal: %s", body)
	}
	hs.expect("GET", "/api/site/result?date=yesterday", "", "", http.StatusBadRequest)

	hs.now = latest
	body = hs.expect("GET", "/api/site/result?date=2026-12-10&lang=uk&lat=50.45&lon=30.52", "", "", http.StatusOK)
	var res resultResponse
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		t.Fatal(err)
	}
	if res.Place.City != "Львів" || len(res.Table) != 1 || !res.Table[0].Me || res.Table[0].Km == nil || *res.Table[0].Km < 400 {
		t.Fatalf("site result = %+v", res)
	}
	// Without a pin it is the place alone.
	body = hs.expect("GET", "/api/site/result?date=2026-12-10", "", "", http.StatusOK)
	if err := json.Unmarshal([]byte(body), &res); err != nil || len(res.Table) != 0 {
		t.Fatalf("site result without a pin = %s", body)
	}
}

// A site player's answer is stored under the cookie's id, ranked with the
// Telegram players once the day closes everywhere, and the days page says
// nothing scored about a day before then.
func TestSitePlayerIsRanked(t *testing.T) {
	hs := setup(t)
	hs.now = time.Date(2026, time.December, 10, 11, 0, 0, 0, time.UTC)
	send := func(method, path, cookie, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: playerCookie, Value: cookie})
		}
		rr := httptest.NewRecorder()
		hs.h.ServeHTTP(rr, req)
		return rr
	}
	rr := send("POST", "/api/site/guess?tz=Europe/Kyiv", "", `{"lat":49.9,"lon":24.1,"name":"  Lucía "}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("site guess = %d %s", rr.Code, rr.Body)
	}
	var player string
	for _, c := range rr.Result().Cookies() {
		if c.Name == playerCookie && c.HttpOnly && c.Secure {
			player = c.Value
		}
	}
	if !uuidPattern.MatchString(player) {
		t.Fatalf("no player cookie: %v", rr.Result().Cookies())
	}
	if rr := send("POST", "/api/site/guess?tz=Europe/Kyiv", player, `{"lat":0,"lon":0}`); rr.Code != http.StatusConflict {
		t.Fatalf("second answer = %d", rr.Code)
	}
	if rr := send("POST", "/api/site/guess", "", `{"lat":99,"lon":0}`); rr.Code != http.StatusBadRequest {
		t.Fatalf("bad pin = %d", rr.Code)
	}
	var today todayResponse
	if err := json.Unmarshal(send("GET", "/api/site/today?tz=Europe/Kyiv", player, "").Body.Bytes(), &today); err != nil || today.Answer == nil || today.Name != "Lucía" {
		t.Fatalf("site today = %+v, %v", today, err)
	}

	days := send("GET", "/api/days", player, "").Body.String()
	if leaks(days) || !strings.Contains(days, `"final":false`) || strings.Contains(days, "Lucía") {
		t.Fatalf("days before the close: %s", days)
	}
	if rr := send("GET", "/api/day?date=2026-12-10", player, ""); rr.Code != http.StatusForbidden || leaks(rr.Body.String()) {
		t.Fatalf("day before the close = %d %s", rr.Code, rr.Body)
	}

	hs.now = time.Date(2026, time.December, 11, 4, 0, 0, 0, time.UTC)
	rr = send("GET", "/api/day?date=2026-12-10&lang=uk", player, "")
	var day struct {
		Players int        `json:"players"`
		Place   placeView  `json:"place"`
		Ranking []rankLine `json:"ranking"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &day); err != nil || rr.Code != http.StatusOK {
		t.Fatalf("day = %d %s", rr.Code, rr.Body)
	}
	if day.Players != 1 || day.Place.City != "Львів" || !day.Ranking[0].Me || !day.Ranking[0].Site || day.Ranking[0].Name != "Lucía" {
		t.Fatalf("day = %+v", day)
	}
	days = send("GET", "/api/days?lang=uk", player, "").Body.String()
	if !strings.Contains(days, `"final":true`) || !strings.Contains(days, `"rank":1`) || !strings.Contains(days, "Львів") {
		t.Fatalf("days after the close: %s", days)
	}
	var res resultResponse
	if err := json.Unmarshal(send("GET", "/api/site/result?date=2026-12-10", player, "").Body.Bytes(), &res); err != nil || len(res.Table) != 1 || res.Table[0].Km == nil || *res.Table[0].Km > 20 {
		t.Fatalf("site result from the stored answer = %+v, %v", res, err)
	}
}

// A DM that never picked a zone takes the phone's, once; a picked one stays.
// Moving the zone later in the day changes when the names open, never the
// score.
func TestDMZoneFromThePhone(t *testing.T) {
	hs := setup(t)
	lena := initData(7, "Lena", "en", "")
	var today todayResponse
	body := hs.expect("GET", "/api/today?tz=Europe/Paris", lena, "", http.StatusOK)
	if err := json.Unmarshal([]byte(body), &today); err != nil || today.ZoneSet != "Europe/Berlin" || today.Zone != "Europe/Berlin" {
		t.Fatalf("today from Paris = %s", body)
	}
	body = hs.expect("GET", "/api/today?tz=Asia/Tokyo", lena, "", http.StatusOK)
	today = todayResponse{}
	if err := json.Unmarshal([]byte(body), &today); err != nil || today.ZoneSet != "" || today.Zone != "Europe/Berlin" {
		t.Fatalf("today from Tokyo after Paris = %s", body)
	}
	// Dmitry picked Moscow himself.
	body = hs.expect("GET", "/api/today?tz=Europe/Berlin", initData(5, "Dmitry", "ru", ""), "", http.StatusOK)
	today = todayResponse{}
	if err := json.Unmarshal([]byte(body), &today); err != nil || today.ZoneSet != "" || today.Zone != "Europe/Moscow" {
		t.Fatalf("Dmitry's today = %s", body)
	}

	// Lena answers at 09:00 UTC, then moves her DM to Tokyo, where 20:00 was
	// 11:00 UTC. At noon her result is final and keeps its score.
	hs.expect("POST", "/api/guess", lena, `{"lat":50,"lon":25}`, http.StatusOK)
	if err := hs.srv.Store.(*db.Store).SetChatZone(context.Background(), 7, "Asia/Tokyo"); err != nil {
		t.Fatal(err)
	}
	hs.now = time.Date(2026, time.December, 10, 12, 0, 0, 0, time.UTC)
	var rr resultResponse
	body = hs.expect("GET", "/api/result", lena, "", http.StatusOK)
	if err := json.Unmarshal([]byte(body), &rr); err != nil || rr.Phase != "final" || len(rr.Table) != 1 || rr.Table[0].Points == nil || *rr.Table[0].Points == 0 {
		t.Fatalf("Lena's result after the move = %s", body)
	}

	// Tom plays in his DM only, on London's clock by his language. At 20:30
	// London he sees the place without answering; his phone then says Los
	// Angeles, where the day is open until 04:00 UTC. The zone is not taken,
	// and he cannot answer.
	tom := initData(8, "Tom", "en", "")
	hs.now = time.Date(2026, time.December, 10, 20, 30, 0, 0, time.UTC)
	hs.expect("GET", "/api/result", tom, "", http.StatusOK)
	body = hs.expect("GET", "/api/today?tz=America/Los_Angeles", tom, "", http.StatusOK)
	today = todayResponse{}
	if err := json.Unmarshal([]byte(body), &today); err != nil || today.ZoneSet != "" || today.Zone != "Europe/London" {
		t.Fatalf("Tom's today from Los Angeles after London's reveal = %s", body)
	}
	hs.expect("POST", "/api/guess", tom, `{"lat":50,"lon":25}`, http.StatusConflict)
}
