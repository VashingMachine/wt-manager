package app

import (
	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/services"
	"github.com/VashingMachine/wt-manager/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

type App struct {
	services core.Services
}

func New() *App {
	return &App{services: services.NewService()}
}

func (a *App) DefaultConfig(forceSetup bool) (core.Config, error) {
	return a.services.DefaultConfig(forceSetup)
}

func (a *App) NewProgram(cfg core.Config) *tea.Program {
	return tea.NewProgram(tui.NewModel(cfg, a.services), tea.WithAltScreen())
}
