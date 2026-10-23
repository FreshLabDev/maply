// SPDX-License-Identifier: Apache-2.0
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// DefaultTelegramAPIBase is Telegram's own server. TELEGRAM_API_BASE exists for
// tests against a stand-in; maply does not use a self-hosted Bot API server.
const DefaultTelegramAPIBase = "https://api.telegram.org"

type Config struct {
	TelegramBotToken string
	TelegramAPIBase  string
	DatabaseURL      string
	AutoMigrate      bool
	HTTPAddr         string
	LogLevel         string
	// ImagesDir holds the place images, one file per random key. Empty
	// serves none.
	ImagesDir string
	// AppURL is the public https address of the Mini App, which Telegram opens
	// from the Play button. Empty leaves the button out.
	AppURL string
	// CardRendererURL is the DevTools address of the headless Chromium that
	// draws the result card, e.g. http://card-renderer:9222, on the internal
	// network only. Empty sends the place's photo instead and leaves sharing
	// on t.me/share.
	CardRendererURL string
	// CardPageURL is where that browser reaches this service for the card
	// page, e.g. http://maply-bot:8080. Required with CardRendererURL.
	CardPageURL string
	// Animations sends the morning post, the intrigue of the time there and
	// the result as looping animations drawn with ffmpeg. Off, or without
	// ffmpeg, they go out as photos and text as before.
	Animations bool
	// FFmpeg is the ffmpeg binary, looked up on PATH unless it is a path.
	FFmpeg string
	// Schedule sends the day's post, intrigue and result. A second instance
	// against the same database can leave it off; the job queue would keep
	// it from sending twice anyway.
	Schedule bool
}

func Load() (Config, error) {
	cfg := Config{
		TelegramBotToken: strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")),
		DatabaseURL:      strings.TrimSpace(os.Getenv("DATABASE_URL")),
		TelegramAPIBase:  strings.TrimRight(valueOrDefault("TELEGRAM_API_BASE", DefaultTelegramAPIBase), "/"),
		HTTPAddr:         valueOrDefault("HTTP_ADDR", ":8080"),
		LogLevel:         valueOrDefault("LOG_LEVEL", "info"),
		ImagesDir:        strings.TrimSpace(os.Getenv("IMAGES_DIR")),
		AppURL:           strings.TrimSpace(os.Getenv("APP_URL")),
		CardRendererURL:  strings.TrimRight(strings.TrimSpace(os.Getenv("CARD_RENDERER_URL")), "/"),
		CardPageURL:      strings.TrimRight(strings.TrimSpace(os.Getenv("CARD_PAGE_URL")), "/"),
		FFmpeg:           valueOrDefault("FFMPEG", "ffmpeg"),
	}
	var err error
	if cfg.AutoMigrate, err = strconv.ParseBool(valueOrDefault("AUTO_MIGRATE", "true")); err != nil {
		return Config{}, fmt.Errorf("AUTO_MIGRATE must be a boolean: %w", err)
	}
	if cfg.Animations, err = strconv.ParseBool(valueOrDefault("ANIMATIONS", "true")); err != nil {
		return Config{}, fmt.Errorf("ANIMATIONS must be a boolean: %w", err)
	}
	if cfg.Schedule, err = strconv.ParseBool(valueOrDefault("SCHEDULE", "true")); err != nil {
		return Config{}, fmt.Errorf("SCHEDULE must be a boolean: %w", err)
	}
	if !strings.HasPrefix(cfg.TelegramAPIBase, "http://") && !strings.HasPrefix(cfg.TelegramAPIBase, "https://") {
		return Config{}, fmt.Errorf("TELEGRAM_API_BASE must be an http or https URL")
	}
	// Telegram opens a Mini App only over https.
	if cfg.AppURL != "" && !strings.HasPrefix(cfg.AppURL, "https://") {
		return Config{}, fmt.Errorf("APP_URL must be an https URL")
	}
	if cfg.CardRendererURL != "" {
		if !isHTTPURL(cfg.CardRendererURL) {
			return Config{}, fmt.Errorf("CARD_RENDERER_URL must be an http or https URL")
		}
		if !isHTTPURL(cfg.CardPageURL) {
			return Config{}, fmt.Errorf("CARD_PAGE_URL must be an http or https URL when CARD_RENDERER_URL is set")
		}
	}
	if cfg.TelegramBotToken == "" {
		return Config{}, fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	return cfg, nil
}

func isHTTPURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func valueOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
