package requests

import (
	"fmt"
	"strings"

	"github.com/infercore/infercore/internal/config"
)

// NewFromConfig returns a ledger Store or nil when ledger is disabled.
func NewFromConfig(cfg *config.Config) (Store, error) {
	if cfg == nil || !cfg.Ledger.Enabled {
		return nil, nil
	}
	d := strings.ToLower(strings.TrimSpace(cfg.Ledger.Driver))
	switch d {
	case "", "memory":
		return NewMemoryStore(), nil
	case "sqlite":
		path := strings.TrimSpace(cfg.Ledger.Path)
		if path == "" {
			return nil, fmt.Errorf("ledger: sqlite path is empty")
		}
		return NewSQLiteStore(path)
	case "postgres":
		dsn := strings.TrimSpace(cfg.Ledger.DSN)
		if dsn == "" {
			return nil, fmt.Errorf("ledger: postgres dsn is empty")
		}
		return NewPostgresStore(dsn)
	default:
		return nil, fmt.Errorf("ledger: unsupported driver %q", cfg.Ledger.Driver)
	}
}
