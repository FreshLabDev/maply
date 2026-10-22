// SPDX-License-Identifier: Apache-2.0
package config

import (
	"fmt"
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
	}
	var err error
	if cfg.AutoMigrate, err = strconv.ParseBool(valueOrDefault("AUTO_MIGRATE", "true")); err != nil {
		return Config{}, fmt.Errorf("AUTO_MIGRATE must be a boolean: %w", err)
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
	if cfg.TelegramBotToken == "" {
		return Config{}, fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	return cfg, nil
}

func valueOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
