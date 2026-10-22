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
}

func Load() (Config, error) {
	cfg := Config{
		TelegramBotToken: strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")),
		DatabaseURL:      strings.TrimSpace(os.Getenv("DATABASE_URL")),
		TelegramAPIBase:  strings.TrimRight(valueOrDefault("TELEGRAM_API_BASE", DefaultTelegramAPIBase), "/"),
		HTTPAddr:         valueOrDefault("HTTP_ADDR", ":8080"),
		LogLevel:         valueOrDefault("LOG_LEVEL", "info"),
	}
	var err error
	if cfg.AutoMigrate, err = strconv.ParseBool(valueOrDefault("AUTO_MIGRATE", "true")); err != nil {
		return Config{}, fmt.Errorf("AUTO_MIGRATE must be a boolean: %w", err)
	}
	if !strings.HasPrefix(cfg.TelegramAPIBase, "http://") && !strings.HasPrefix(cfg.TelegramAPIBase, "https://") {
		return Config{}, fmt.Errorf("TELEGRAM_API_BASE must be an http or https URL")
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
