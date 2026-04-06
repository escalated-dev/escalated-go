// Package escalated provides an embeddable support ticket system for Go applications.
//
// It works with standard net/http and popular routers (chi, gin, echo) by exposing
// handlers as plain http.HandlerFunc values. Database access is abstracted behind
// the Store interface with ready-made PostgreSQL and SQLite implementations.
//
// Quick start with Chi:
//
//	cfg := escalated.DefaultConfig()
//	cfg.DB = db
//	esc, _ := escalated.New(cfg)
//	r := chi.NewRouter()
//	router.MountChi(r, esc)
//	http.ListenAndServe(":8080", r)
//
// Quick start with stdlib:
//
//	cfg := escalated.DefaultConfig()
//	cfg.DB = db
//	esc, _ := escalated.New(cfg)
//	mux := http.NewServeMux()
//	router.MountStdlib(mux, esc)
//	http.ListenAndServe(":8080", mux)
package escalated

import (
	"fmt"
	"net/http"

	"github.com/escalated-dev/escalated-go/renderer"
	"github.com/escalated-dev/escalated-go/store"
)

// Version is the current version of the Escalated Go package.
const Version = "0.1.0"

// Escalated is the central container that holds the store, renderer, and config.
// Create one with New() and pass it to router.MountChi or router.MountStdlib.
type Escalated struct {
	Config   Config
	Store    store.Store
	Renderer renderer.Renderer
}

// New creates a new Escalated instance from the given config.
// It initialises the default PostgreSQL store and the appropriate renderer
// based on Config.UIEnabled.
func New(cfg Config) (*Escalated, error) {
	if cfg.DB == nil {
		return nil, fmt.Errorf("escalated: Config.DB is required")
	}

	applyDefaults(&cfg)
	s := store.NewPostgresStore(cfg.DB, cfg.TablePrefix)

	var rend renderer.Renderer
	if cfg.UIEnabled {
		rend = renderer.NewInertiaRenderer("")
	} else {
		rend = renderer.NewJSONRenderer()
	}

	return &Escalated{
		Config:   cfg,
		Store:    s,
		Renderer: rend,
	}, nil
}

// NewSQLite is like New but uses the SQLite store implementation.
func NewSQLite(cfg Config) (*Escalated, error) {
	if cfg.DB == nil {
		return nil, fmt.Errorf("escalated: Config.DB is required")
	}

	applyDefaults(&cfg)
	s := store.NewSQLiteStore(cfg.DB, cfg.TablePrefix)

	var rend renderer.Renderer
	if cfg.UIEnabled {
		rend = renderer.NewInertiaRenderer("")
	} else {
		rend = renderer.NewJSONRenderer()
	}

	return &Escalated{
		Config:   cfg,
		Store:    s,
		Renderer: rend,
	}, nil
}

func applyDefaults(cfg *Config) {
	if cfg.RoutePrefix == "" {
		cfg.RoutePrefix = "/escalated"
	}
	if cfg.TablePrefix == "" {
		cfg.TablePrefix = "escalated_"
	}
	if cfg.AdminCheck == nil {
		cfg.AdminCheck = func(_ *http.Request) bool { return false }
	}
	if cfg.AgentCheck == nil {
		cfg.AgentCheck = func(_ *http.Request) bool { return false }
	}
	if cfg.UserIDFunc == nil {
		cfg.UserIDFunc = func(_ *http.Request) int64 { return 0 }
	}
}
