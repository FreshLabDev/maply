// SPDX-License-Identifier: Apache-2.0

// Command maply-dev is a local stand for the Mini App and the site: the real
// API and pages on a local PostgreSQL, Telegram players faked with signed
// launches, past days seeded through the store, and a clock that can move.
//
// It is for development only. It refuses a database whose name does not
// contain "test", truncates the maply schema of that database on a fresh
// start, and signs everything with a fixed fake bot token.
//
//	scripts/dev-db.sh start
//	go run ./cmd/maply-dev -places ../curation/out-004,../curation/out-free
//
// Then open http://127.0.0.1:8399/dev.
//
// Features add themselves in their own files with Register, so a new screen
// can bring its own seed, its own handlers under /dev/<name>/ and its own
// block on the /dev page without editing the shared ones.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FreshLabDev/maply/internal/api"
	"github.com/FreshLabDev/maply/internal/auth"
	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/geo"
	"github.com/FreshLabDev/maply/internal/profile"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/migrations"
	"github.com/FreshLabDev/maply/web"
)

// BotToken is the fake bot's token: initData and startapp payloads are signed
// with it, so the stand's launches verify and nothing real does.
const BotToken = "123456:maply-dev-stand"

// Env is what an extension gets: the stand's store, clock and people.
type Env struct {
	Store     *db.Store
	Pool      *pgxpool.Pool
	API       *api.Server
	Signer    *auth.Signer
	Zones     round.Zones
	Countries *geo.Countries
	Clock     *Clock
	Log       *slog.Logger
	// ImagesDir is what /img/ serves; StateDir is the stand's own folder.
	ImagesDir string
	StateDir  string
	// Places are the curation folders the places came from, if any.
	Places  []string
	Players []Player
	Groups  []Group
	// Days is how many past days the seed plays.
	Days int
	// Handler is the whole site, /dev included: an extension can call the
	// real API through it.
	Handler http.Handler
	// Addr is where the stand listens; Bot runs the bot and the scheduler on
	// the fake Telegram.
	Addr string
	Bot  bool

	ctx   context.Context
	stops []func()
}

// Extension is a feature's part of the stand. Every field is optional.
type Extension struct {
	Name string
	// Seed runs after the shared seed on a fresh stand.
	Seed func(ctx context.Context, e *Env) error
	// Mount adds handlers, by convention under /dev/<Name>/.
	Mount func(e *Env, mux *http.ServeMux)
	// Panel is the extension's block on the /dev page.
	Panel func(e *Env, r *http.Request) template.HTML
	// Start runs once the stand listens, and again after /dev/fresh; what
	// it returns stops it before the next wipe.
	Start func(ctx context.Context, e *Env) (stop func(), err error)
}

func (e *Env) startExtensions() error {
	for _, x := range extensions {
		if x.Start == nil {
			continue
		}
		stop, err := x.Start(e.ctx, e)
		if err != nil {
			return fmt.Errorf("start %s: %w", x.Name, err)
		}
		e.stops = append(e.stops, stop)
	}
	return nil
}

func (e *Env) stopExtensions() {
	for i := len(e.stops) - 1; i >= 0; i-- {
		e.stops[i]()
	}
	e.stops = nil
}

var extensions []Extension

// Register adds an extension. Call it from an init function in the
// extension's own file.
func Register(x Extension) { extensions = append(extensions, x) }

// Clock is the stand's now: real time moved by a shift.
type Clock struct{ shift atomic.Int64 }

func (c *Clock) Now() time.Time { return time.Now().Add(time.Duration(c.shift.Load())) }

// Move shifts the clock by d; a negative d goes back.
func (c *Clock) Move(d time.Duration) { c.shift.Add(int64(d)) }

// Set puts the clock at t.
func (c *Clock) Set(t time.Time) { c.shift.Store(int64(time.Until(t))) }

// Reset brings the clock back to real time.
func (c *Clock) Reset() { c.shift.Store(0) }

// Shifted says how far the clock is from real time.
func (c *Clock) Shifted() time.Duration { return time.Duration(c.shift.Load()) }

type yesMember struct{}

func (yesMember) IsMember(context.Context, round.Date, int64, int64) (bool, error) {
	return true, nil
}

func main() {
	dsn := flag.String("db", envOr("MAPLY_DEV_DATABASE_URL", "postgres://maply@127.0.0.1:55491/maply_dev_test?sslmode=disable"), "PostgreSQL URL; the database name must contain \"test\"")
	addr := flag.String("addr", "127.0.0.1:8399", "address to listen on")
	places := flag.String("places", "", "comma-separated curation output folders with places.sql and images/ (empty: synthetic places)")
	days := flag.Int("days", 30, "past days to seed")
	fresh := flag.Bool("fresh", false, "wipe the stand's tables and seed again even when it has data")
	state := flag.String("state", "", "the stand's own folder (default: the user cache folder)")
	runBot := flag.Bool("bot", true, "run the bot and the scheduler on a fake Telegram, shown at /dev/tg")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(logger, *dsn, *addr, *places, *state, *days, *fresh, *runBot); err != nil {
		log.Fatal(err)
	}
}

func run(logger *slog.Logger, dsn, addr, places, state string, days int, fresh, runBot bool) error {
	if name := dbName(dsn); !strings.Contains(name, "test") {
		return fmt.Errorf("refusing database %q: the stand wipes it, so its name must contain \"test\"", name)
	}
	if state == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return err
		}
		state = filepath.Join(cache, "maply-dev")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	store, err := db.Connect(ctx, dsn)
	if err != nil {
		return err
	}
	defer store.Close()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()
	boot, err := os.ReadFile(filepath.Join(repoRoot(), "deploy", "core-init.sql"))
	if err != nil {
		return fmt.Errorf("read the local core bootstrap: %w", err)
	}
	if _, err := pool.Exec(ctx, string(boot)); err != nil {
		return fmt.Errorf("core bootstrap: %w", err)
	}
	if err := store.Migrate(ctx, migrations.FS); err != nil {
		return err
	}
	zones, err := round.LoadZones()
	if err != nil {
		return err
	}
	countries, err := geo.World()
	if err != nil {
		return err
	}
	e := &Env{
		Store:     store,
		Pool:      pool,
		Signer:    auth.NewSigner(BotToken),
		Zones:     zones,
		Countries: countries,
		Clock:     &Clock{},
		Log:       logger,
		ImagesDir: filepath.Join(state, "images"),
		StateDir:  state,
		Places:    splitList(places),
		Players:   players,
		Groups:    groups,
		Days:      days,
		Addr:      addr,
		Bot:       runBot,
		ctx:       ctx,
	}
	e.API = &api.Server{
		Store:       store,
		InitData:    auth.NewVerifier(BotToken),
		Payloads:    e.Signer,
		Members:     yesMember{},
		Zones:       zones,
		Countries:   countries,
		ImagesDir:   e.ImagesDir,
		BotUsername: "maplyin_bot",
		AppURL:      "http://" + e.Addr,
		Stills:      &profile.Service{Store: store, Zones: zones, Log: logger, Now: e.Clock.Now},
		Log:         logger,
		Now:         e.Clock.Now,
	}
	game := e.API.Handler()
	app, err := web.HandlerWithMeta(e.API.PageMeta)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.Handle("/api/", game)
	mux.Handle("/img/", game)
	mux.Handle("/thumb/", game)
	mux.Handle("/og/", game)
	mux.Handle("/", app)
	e.Handler = mux
	mountDev(e, mux)
	for _, x := range extensions {
		if x.Mount != nil {
			x.Mount(e, mux)
		}
	}

	seeded, err := hasRounds(ctx, pool)
	if err != nil {
		return err
	}
	if fresh || !seeded {
		if err := seed(ctx, e); err != nil {
			return err
		}
	} else {
		logger.Info("stand kept as it was; -fresh seeds it again")
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	logger.Info("maply-dev listening", "url", "http://"+addr+"/dev", "images", e.ImagesDir)
	if err := e.startExtensions(); err != nil {
		_ = srv.Close()
		return err
	}
	select {
	case err = <-served:
	case <-ctx.Done():
		shut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err = srv.Shutdown(shut)
	}
	e.stopExtensions()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func hasRounds(ctx context.Context, pool *pgxpool.Pool) (bool, error) {
	var n int
	err := pool.QueryRow(ctx, `SELECT count(*) FROM maply.round`).Scan(&n)
	return n > 0, err
}

// repoRoot finds the repository from the working directory, so the stand
// runs from the root or from cmd/maply-dev.
func repoRoot() string {
	dir, _ := os.Getwd()
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
		if filepath.Dir(d) == d {
			return dir
		}
	}
}

func dbName(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(u.Path, "/")
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
