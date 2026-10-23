// SPDX-License-Identifier: Apache-2.0
package config

import "testing"

func setRequired(t *testing.T) {
	t.Helper()
	t.Setenv("TELEGRAM_BOT_TOKEN", "123:abc")
	t.Setenv("DATABASE_URL", "postgres://maply@localhost/maply")
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("LOG_LEVEL", "")
	t.Setenv("AUTO_MIGRATE", "")
	t.Setenv("TELEGRAM_API_BASE", "")
	t.Setenv("APP_URL", "")
	t.Setenv("CARD_RENDERER_URL", "")
	t.Setenv("CARD_PAGE_URL", "")
}

func TestLoadDefaults(t *testing.T) {
	setRequired(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":8080" || cfg.TelegramAPIBase != DefaultTelegramAPIBase || cfg.LogLevel != "info" || !cfg.AutoMigrate {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestLoadRequiresSecrets(t *testing.T) {
	for _, key := range []string{"TELEGRAM_BOT_TOKEN", "DATABASE_URL"} {
		t.Run(key, func(t *testing.T) {
			setRequired(t)
			t.Setenv(key, " ")
			if _, err := Load(); err == nil {
				t.Fatalf("Load succeeded without %s", key)
			}
		})
	}
}

func TestLoadRejectsBadAutoMigrate(t *testing.T) {
	setRequired(t)
	t.Setenv("AUTO_MIGRATE", "sometimes")
	if _, err := Load(); err == nil {
		t.Fatal("Load accepted AUTO_MIGRATE=sometimes")
	}
}

func TestLoadRejectsAPIBaseWithoutScheme(t *testing.T) {
	setRequired(t)
	t.Setenv("TELEGRAM_API_BASE", "api.telegram.org")
	if _, err := Load(); err == nil {
		t.Fatal("Load accepted TELEGRAM_API_BASE without a scheme")
	}
}

func TestLoadRejectsAppURLWithoutHTTPS(t *testing.T) {
	setRequired(t)
	t.Setenv("APP_URL", "http://maply.example")
	if _, err := Load(); err == nil {
		t.Fatal("Load accepted an http APP_URL")
	}
	t.Setenv("APP_URL", "https://maply.example/")
	cfg, err := Load()
	if err != nil || cfg.AppURL != "https://maply.example/" {
		t.Fatalf("cfg = %+v, err = %v", cfg, err)
	}
}

func TestCardsAreOffUnlessARendererIsNamed(t *testing.T) {
	setRequired(t)
	cfg, err := Load()
	if err != nil || cfg.CardRendererURL != "" {
		t.Fatalf("cfg = %+v, %v", cfg, err)
	}
	t.Setenv("CARD_RENDERER_URL", "http://card-renderer:9222/")
	t.Setenv("CARD_PAGE_URL", "http://maply-bot:8080/")
	cfg, err = Load()
	if err != nil || cfg.CardRendererURL != "http://card-renderer:9222" || cfg.CardPageURL != "http://maply-bot:8080" {
		t.Fatalf("cfg = %+v, %v", cfg, err)
	}
}

func TestCardRendererNeedsAPageURLAndSchemes(t *testing.T) {
	for name, env := range map[string][2]string{
		"no page url":        {"http://card-renderer:9222", ""},
		"renderer no scheme": {"card-renderer:9222", "http://maply-bot:8080"},
		"page no scheme":     {"http://card-renderer:9222", "maply-bot:8080"},
		"ftp renderer":       {"ftp://card-renderer:9222", "http://maply-bot:8080"},
	} {
		setRequired(t)
		t.Setenv("CARD_RENDERER_URL", env[0])
		t.Setenv("CARD_PAGE_URL", env[1])
		if _, err := Load(); err == nil {
			t.Errorf("%s: Load accepted %q and %q", name, env[0], env[1])
		}
	}
}
