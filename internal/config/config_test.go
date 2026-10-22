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
