package app

import (
	"os"
	"path/filepath"

	"github.com/VashingMachine/wt-manager/internal/cache"
	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/services"
	"github.com/VashingMachine/wt-manager/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

type App struct {
	services core.Services
	cache    *cache.Store
}

// New opens the persistent cache next to the config file. If it cannot be
// opened the app keeps working with an in-memory cache and logs why.
func New() *App {
	store := cache.NewMemory()
	var openErr error
	if homeDir, err := os.UserHomeDir(); err == nil {
		removeLegacyCache(homeDir)
		store, openErr = cache.Open(cache.DefaultPath(homeDir))
	}
	svc := services.NewServiceWithCache(store)
	if openErr != nil {
		svc.RecordSessionEvent("persistent cache disabled: "+openErr.Error(), true)
	}
	return &App{services: svc, cache: store}
}

// removeLegacyCache deletes the per-board JSON ticket files that the SQLite
// cache replaced.
func removeLegacyCache(homeDir string) {
	matches, _ := filepath.Glob(filepath.Join(homeDir, ".wt-manager", "cache", "jira-*.json"))
	for _, path := range matches {
		_ = os.Remove(path)
	}
}

func (a *App) DefaultConfig(forceSetup bool) (core.Config, error) {
	return a.services.DefaultConfig(forceSetup)
}

func (a *App) NewProgram(cfg core.Config) *tea.Program {
	return tea.NewProgram(tui.NewModel(cfg, a.services), tea.WithAltScreen())
}

// Close flushes cache writes still queued in the background.
func (a *App) Close() error {
	return a.cache.Close()
}
