package config

import "testing"

// PR #294: MIGRATIONS_DROP_RECREATE default FALSE olmalıdır (fail-closed) —
// prod-da env verilməsə belə cədvəllər DROP olunmamalıdır.
// Dev-də istəyə görə açıq şəkildə true yazılır.

// setRequiredEnv DB üçün məcburi env-ləri qoyur (requireEnv exit etməsin deyə).
func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_USER", "sa")
	t.Setenv("DB_PASSWORD", "test-pass")
}

func TestDropRecreateDefaultFalse(t *testing.T) {
	setRequiredEnv(t)
	// Boş dəyər = default yolu (getEnvBool fallback qaytarır)
	t.Setenv("MIGRATIONS_DROP_RECREATE", "")

	cfg := Load()
	if cfg.MigrationsDropRecreate {
		t.Fatal("MIGRATIONS_DROP_RECREATE default true-dir — PR #294-ə görə false olmalıdır (fail-closed)")
	}
}

func TestDropRecreateExplicitTrue(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("MIGRATIONS_DROP_RECREATE", "true")

	cfg := Load()
	if !cfg.MigrationsDropRecreate {
		t.Fatal("MIGRATIONS_DROP_RECREATE=true açıq şəkildə verildikdə true olmalıdır (dev rejimi)")
	}
}

func TestDropRecreateExplicitFalse(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("MIGRATIONS_DROP_RECREATE", "false")

	cfg := Load()
	if cfg.MigrationsDropRecreate {
		t.Fatal("MIGRATIONS_DROP_RECREATE=false verildikdə false olmalıdır")
	}
}

// PR #360: ekspert iş saatları env-dən oxunur; default 9-20.
func TestExpertWorkHoursDefault(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("EXPERT_WORK_START_HOUR", "")
	t.Setenv("EXPERT_WORK_END_HOUR", "")

	cfg := Load()
	if cfg.ExpertWorkStartHour != 9 || cfg.ExpertWorkEndHour != 20 {
		t.Fatalf("default iş saatları 9-20 olmalıdır, got %d-%d",
			cfg.ExpertWorkStartHour, cfg.ExpertWorkEndHour)
	}
}

func TestExpertWorkHoursFromEnv(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("EXPERT_WORK_START_HOUR", "10")
	t.Setenv("EXPERT_WORK_END_HOUR", "19")

	cfg := Load()
	if cfg.ExpertWorkStartHour != 10 || cfg.ExpertWorkEndHour != 19 {
		t.Fatalf("env-dən 10-19 oxunmalıdır, got %d-%d",
			cfg.ExpertWorkStartHour, cfg.ExpertWorkEndHour)
	}
}
