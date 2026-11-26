// Package app is the composition root: the one place that knows every concrete
// integration. It builds them, hands them to the application layer
// (internal/services) and owns the persistent cache. Everything else depends
// on interfaces only.
package app

import (
	"os"
	"path/filepath"

	"github.com/VashingMachine/wt-manager/internal/cache"
	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/integrations/agent"
	"github.com/VashingMachine/wt-manager/internal/integrations/browser"
	"github.com/VashingMachine/wt-manager/internal/integrations/configfile"
	"github.com/VashingMachine/wt-manager/internal/integrations/editor"
	"github.com/VashingMachine/wt-manager/internal/integrations/git"
	"github.com/VashingMachine/wt-manager/internal/integrations/github"
	"github.com/VashingMachine/wt-manager/internal/integrations/jira"
	"github.com/VashingMachine/wt-manager/internal/integrations/terminal"
	"github.com/VashingMachine/wt-manager/internal/services"
)

// NewServices wires the real integrations into the application. A nil store
// means an in-memory cache: one-shot commands and tests use it so they never
// touch the user's persistent cache.
//
// A new integration is added here, next to its port in services.Integrations.
func NewServices(store *cache.Store) *services.Service {
	return services.New(services.Integrations{
		Config:   configfile.New(),
		Git:      git.New(),
		GitHub:   github.New(),
		Jira:     jira.New(),
		Terminal: terminal.New(),
		Editor:   editor.New(),
		Browser:  browser.New(),
		Agents:   agent.New(),
	}, store)
}

// App is the running application: its services and the cache behind them.
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
	svc := NewServices(store)
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

// Services is what the UIs drive.
func (a *App) Services() core.Services {
	return a.services
}

func (a *App) DefaultConfig(forceSetup bool) (core.Config, error) {
	return a.services.DefaultConfig(forceSetup)
}

// Close flushes cache writes still queued in the background.
func (a *App) Close() error {
	return a.cache.Close()
}
