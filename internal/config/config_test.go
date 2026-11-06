package config

import "testing"

func TestLoadDefaultsPort(t *testing.T) {
	t.Setenv("PORT", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != "8080" {
		t.Fatalf("port = %q, want 8080", cfg.Port)
	}
}

func TestLoadPortFromEnv(t *testing.T) {
	t.Setenv("PORT", "9090")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != "9090" {
		t.Fatalf("port = %q, want 9090", cfg.Port)
	}
}
