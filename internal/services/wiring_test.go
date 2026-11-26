package services

import (
	"github.com/VashingMachine/wt-manager/internal/integrations/agent"
	"github.com/VashingMachine/wt-manager/internal/integrations/browser"
	"github.com/VashingMachine/wt-manager/internal/integrations/configfile"
	"github.com/VashingMachine/wt-manager/internal/integrations/editor"
	"github.com/VashingMachine/wt-manager/internal/integrations/git"
	"github.com/VashingMachine/wt-manager/internal/integrations/github"
	"github.com/VashingMachine/wt-manager/internal/integrations/jira"
	"github.com/VashingMachine/wt-manager/internal/integrations/terminal"
)

// NewService wires the real integrations with an in-memory cache, the way
// internal/app does. Tests that use it run real git in throwaway repositories
// and put scripted gh and acli executables on PATH, so they cover the
// application layer together with the integrations behind it.
func NewService() *Service {
	return New(Integrations{
		Config:   configfile.New(),
		Git:      git.New(),
		GitHub:   github.New(),
		Jira:     jira.New(),
		Terminal: terminal.New(),
		Editor:   editor.New(),
		Browser:  browser.New(),
		Agents:   agent.New(),
	}, nil)
}
