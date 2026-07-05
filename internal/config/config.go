// Package config parses command-line flags and environment variables for the
// warehouse application.
//
// Precedence (highest wins):
//  1. Command-line flags  (--host, --port, --db)
//  2. Environment variables (WAREHOUSE_HOST, WAREHOUSE_PORT, WAREHOUSE_DB_PATH)
//  3. Defaults (127.0.0.1:17880, ~/.warehouse/data.db)
package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// Config holds the resolved application configuration.
type Config struct {
	Host     string // HTTP listen host (default "127.0.0.1")
	Port     int    // HTTP listen port (default 17880)
	DBPath   string // SQLite database path
	Headless bool   // skip GUI, start HTTP+MCP directly (for CI/e2e/testing)
}

// DefaultDBPath returns the default SQLite database location:
// $HOME/.warehouse/data.db.
func DefaultDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".warehouse", "data.db")
	}
	return filepath.Join(home, ".warehouse", "data.db")
}

// Parse resolves configuration from args (typically os.Args[1:]),
// environment variables, and defaults.
func Parse(args []string) Config {
	cfg := Config{
		Host:   "127.0.0.1",
		Port:   17880,
		DBPath: DefaultDBPath(),
	}

	if args == nil {
		args = os.Args[1:]
	} else if len(args) > 0 {
		args = args[1:]
	}

	// Env vars set the effective default — explicit flags override.
	if v, ok := os.LookupEnv("WAREHOUSE_HOST"); ok {
		cfg.Host = v
	}
	if v, ok := os.LookupEnv("WAREHOUSE_PORT"); ok {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.Port = p
		}
	}
	if v, ok := os.LookupEnv("WAREHOUSE_DB_PATH"); ok {
		cfg.DBPath = v
	}

	fs := flag.NewFlagSet("warehouse", flag.ContinueOnError)
	fs.StringVar(&cfg.Host, "host", cfg.Host, "HTTP listen host")
	fs.IntVar(&cfg.Port, "port", cfg.Port, "HTTP listen port")
	fs.StringVar(&cfg.DBPath, "db", cfg.DBPath, "SQLite database path")
	fs.BoolVar(&cfg.Headless, "headless", false, "skip GUI, start HTTP+MCP directly")
	_ = fs.Parse(args)

	if cfg.Port <= 0 || cfg.Port > 65535 {
		fmt.Fprintf(os.Stderr, "warning: invalid port %d, using 17880\n", cfg.Port)
		cfg.Port = 17880
	}
	return cfg
}