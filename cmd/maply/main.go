// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/api"
	"github.com/FreshLabDev/maply/internal/auth"
	"github.com/FreshLabDev/maply/internal/bot"
	"github.com/FreshLabDev/maply/internal/config"
	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/geo"
	"github.com/FreshLabDev/maply/internal/health"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/migrations"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: parseLevel(os.Getenv("LOG_LEVEL"))}))
	if err := run(log); err != nil {
		log.Error("exit", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
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
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	store, err := connect(ctx, cfg.DatabaseURL, log)
	if err != nil {
		return err
	}
	defer store.Close()
	if cfg.AutoMigrate {
		if err := store.Migrate(ctx, migrations.FS); err != nil {
			return err
		}
	}

	client := tg.New(cfg.TelegramBotToken,
		tg.WithAPIBase(cfg.TelegramAPIBase),
		// my_chat_member is what tells core which groups maply is in; Telegram
		// leaves it out of the default set, so it has to be asked for.
		tg.WithAllowedUpdates("message", "callback_query", "my_chat_member"),
		tg.WithLogger(log),
	)
	me, err := client.Preflight(ctx, tg.Needs{})
	if err != nil {
		return err
	}
	log.Info("telegram preflight passed", "username", me.Username, "bot_api", tg.BotAPI)
	b := bot.New(store, client, log)

	game := (&api.Server{
		Store:     store,
		InitData:  auth.NewVerifier(cfg.TelegramBotToken),
		Payloads:  auth.NewSigner(cfg.TelegramBotToken),
		Members:   auth.NewMembers(client),
		Zones:     zones,
		Countries: countries,
		ImagesDir: cfg.ImagesDir,
		Log:       log,
	}).Handler()

	started := time.Now()
	mux := http.NewServeMux()
	mux.Handle("/api/", game)
	mux.Handle("/img/", game)
	mux.Handle("/healthz", health.New(store, b.LastPoll, b.Initialized, started, health.Build{
		Version: version, Commit: commit, Date: date,
	}, log))
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 2)
	go func() {
		log.Info("http listening", "addr", cfg.HTTPAddr, "version", version)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	go func() {
		log.Info("telegram polling starting")
		if err := b.Run(ctx); err != nil {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		stop()
		_ = srv.Shutdown(context.Background())
		return err
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// connect retries the first PostgreSQL connection. Container DNS inside the
// shared core_net is not always resolvable the instant the process starts, and
// exiting there turns a two-second blip into a restart loop.
func connect(ctx context.Context, url string, log *slog.Logger) (*db.Store, error) {
	const maxAttempts = 8
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		store, err := db.Connect(ctx, url)
		if err == nil {
			return store, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		delay := time.Duration(1<<min(attempt-1, 4)) * time.Second
		log.Warn("database connect failed; retrying", "attempt", attempt, "retry_in", delay, "error", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
	return nil, lastErr
}

func parseLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
