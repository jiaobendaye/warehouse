package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jiaobendaye/warehouse/internal/config"
)

func TestParse_Defaults(t *testing.T) {
	os.Unsetenv("WAREHOUSE_HOST")
	os.Unsetenv("WAREHOUSE_PORT")
	os.Unsetenv("WAREHOUSE_DB_PATH")

	cfg := config.Parse([]string{"warehouse"})

	if cfg.Host != "0.0.0.0" {
		t.Errorf("default Host = %q, want 0.0.0.0", cfg.Host)
	}
	if cfg.Port != 17880 {
		t.Errorf("default Port = %d, want 17880", cfg.Port)
	}
}

func TestParse_Flags(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "data.db")

	cfg := config.Parse([]string{
		"warehouse",
		"--host", "0.0.0.0",
		"--port", "9090",
		"--db", dbPath,
	})

	if cfg.Host != "0.0.0.0" {
		t.Errorf("Host = %q, want 0.0.0.0", cfg.Host)
	}
	if cfg.Port != 9090 {
		t.Errorf("Port = %d, want 9090", cfg.Port)
	}
	if cfg.DBPath != dbPath {
		t.Errorf("DBPath = %q, want %q", cfg.DBPath, dbPath)
	}
}

func TestParse_EnvOverride(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "env.db")

	t.Setenv("WAREHOUSE_HOST", "192.168.1.1")
	t.Setenv("WAREHOUSE_PORT", "9999")
	t.Setenv("WAREHOUSE_DB_PATH", dbPath)

	cfg := config.Parse([]string{"warehouse"})

	if cfg.Host != "192.168.1.1" {
		t.Errorf("env Host = %q, want 192.168.1.1", cfg.Host)
	}
	if cfg.Port != 9999 {
		t.Errorf("env Port = %d, want 9999", cfg.Port)
	}
	if cfg.DBPath != dbPath {
		t.Errorf("env DBPath = %q, want %q", cfg.DBPath, dbPath)
	}
}

func TestParse_FlagOverridesEnv(t *testing.T) {
	t.Setenv("WAREHOUSE_HOST", "10.0.0.1")
	t.Setenv("WAREHOUSE_PORT", "7777")

	cfg := config.Parse([]string{"warehouse", "--host", "10.0.0.2", "--port", "8888"})

	if cfg.Host != "10.0.0.2" {
		t.Errorf("flag Host = %q, want 10.0.0.2 (overrides env 10.0.0.1)", cfg.Host)
	}
	if cfg.Port != 8888 {
		t.Errorf("flag Port = %d, want 8888 (overrides env 7777)", cfg.Port)
	}
}