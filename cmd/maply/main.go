// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/api"
	"github.com/FreshLabDev/maply/internal/auth"
	"github.com/FreshLabDev/maply/internal/avatar"
	"github.com/FreshLabDev/maply/internal/bot"
	"github.com/FreshLabDev/maply/internal/card"
	"github.com/FreshLabDev/maply/internal/config"
	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/geo"
	"github.com/FreshLabDev/maply/internal/health"
	"github.com/FreshLabDev/maply/internal/profile"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/internal/schedule"
	"github.com/FreshLabDev/maply/internal/telegram"
	"github.com/FreshLabDev/maply/migrations"
	"github.com/FreshLabDev/maply/web"
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
	tgc := telegram.New(client)
	tgc.Upload = &telegram.Uploader{Base: cfg.TelegramAPIBase, Token: cfg.TelegramBotToken}
	// ffmpeg encodes the animated posts and the statistics picture. Without
	// it posts go out as photos and text, and the statistics as text.
	var ffmpeg string
	if cfg.Animations {
		if path, err := exec.LookPath(cfg.FFmpeg); err != nil {
			log.Warn("ffmpeg not found, posts go out as photos and text", "ffmpeg", cfg.FFmpeg, "error", err)
		} else {
			ffmpeg = path
			log.Info("animated posts on", "ffmpeg", path)
		}
	}
	opts := bot.Options{
		Username: me.Username,
		Version:  version,
		AppURL:   cfg.AppURL,
		Zones:    zones,
	}
	if ffmpeg != "" {
		profiles := &profile.Service{Store: store, Zones: zones, FFmpeg: ffmpeg, Log: log}
		opts.Profiles = profiles
		go profiles.Run(ctx, 10*time.Minute)
	}
	b := bot.New(store, tgc, log, opts)
	// The players' photos for the pins and tables of the results.
	avatars := &avatar.Service{Store: store, TG: tgc, Log: log}

	signer := auth.NewSigner(cfg.TelegramBotToken)
	// Without a renderer the result goes out with the place's photo and the
	// Mini App shares through t.me/share, as before the card existed.
	var cards *card.Service
	if cfg.CardRendererURL != "" {
		cards = &card.Service{
			Store:     store,
			Signer:    signer,
			Zones:     zones,
			Shot:      card.NewChrome(cfg.CardRendererURL),
			PageURL:   cfg.CardPageURL,
			PublicURL: cfg.AppURL,
			Username:  me.Username,
			TG:        tgc,
			Avatars:   avatars,
			Log:       log,
		}
		log.Info("result cards on", "renderer", cfg.CardRendererURL, "sharing", cards.CanShare())
	}

	gameAPI := &api.Server{
		Store:       store,
		InitData:    auth.NewVerifier(cfg.TelegramBotToken),
		Payloads:    signer,
		Members:     auth.NewMembers(client),
		Zones:       zones,
		Countries:   countries,
		ImagesDir:   cfg.ImagesDir,
		BotUsername: me.Username,
		Avatars:     avatars,
		Log:         log,
	}
	if cards != nil {
		gameAPI.Shares = cards
	}
	game := gameAPI.Handler()

	app, err := web.Handler()
	if err != nil {
		return err
	}

	started := time.Now()
	mux := http.NewServeMux()
	mux.Handle("/api/", game)
	mux.Handle("/img/", game)
	if cards != nil {
		// The card page is for the renderer, behind a signed link and the
		// chat's reveal; the shared images are what Telegram fetches.
		mux.Handle("/card/", cards.Handler())
	}
	mux.Handle("/", app)
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

	errCh := make(chan error, 3)
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

	if cfg.Schedule {
		sched := &schedule.Scheduler{
			Store:     store,
			TG:        tgc,
			Weather:   schedule.OpenMeteo{},
			Payloads:  signer,
			Zones:     zones,
			Countries: countries,
			ImagesDir: cfg.ImagesDir,
			Username:  me.Username,
			AppURL:    cfg.AppURL,
			Avatars:   avatars,
			Log:       log,
		}
		if cards != nil {
			sched.Cards = cards
		}
		if ffmpeg != "" {
			sched.Anim = &schedule.Animator{FFmpeg: ffmpeg}
		}
		go func() {
			log.Info("scheduler starting")
			if err := sched.Run(ctx); err != nil {
				errCh <- err
			}
		}()
	}

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
