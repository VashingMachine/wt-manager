// Package services is the application layer: it implements the operations the
// UIs ask for (core.Services) by combining the integrations with the cache.
//
// What lives here is policy that belongs to no single tool: which data is
// served from the cache and for how long, what a failed refresh falls back to,
// how many requests run at once, and how a setup decision is made from the
// config file and the repository the app was started in.
//
// The package reaches the outside world only through the interfaces in
// ports.go. It never imports an integration; internal/app wires the real ones
// in, and tests can pass fakes.
package services

import (
	"github.com/VashingMachine/wt-manager/internal/cache"
	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/shell"
)

// Service implements core.Services.
type Service struct {
	config   ConfigStore
	git      Git
	github   GitHub
	jira     Jira
	terminal Terminal
	editor   Editor
	browser  Browser
	agents   AgentRunner

	sessionLog shell.Log
	cache      *cache.Store
	flights    flightGroup
	// ghLimit and gitLimit bound how many gh API calls and git status reads
	// progressive loading runs at once.
	ghLimit  chan struct{}
	gitLimit chan struct{}
}

var _ core.Services = (*Service)(nil)

// New builds the application on top of the given integrations. A nil store
// means an in-memory cache, which tests and one-shot commands use so they
// never touch the user's persistent cache.
func New(integrations Integrations, store *cache.Store) *Service {
	if store == nil {
		store = cache.NewMemory()
	}
	return &Service{
		config:   integrations.Config,
		git:      integrations.Git,
		github:   integrations.GitHub,
		jira:     integrations.Jira,
		terminal: integrations.Terminal,
		editor:   integrations.Editor,
		browser:  integrations.Browser,
		agents:   integrations.Agents,
		cache:    store,
		ghLimit:  make(chan struct{}, 6),
		gitLimit: make(chan struct{}, 8),
	}
}
