package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	forceSetup := false
	for _, arg := range os.Args[1:] {
		if arg == "--init" {
			forceSetup = true
			continue
		}
		if arg == "-h" || arg == "--help" {
			fmt.Println("usage: wt-manager [--init]")
			return
		}
		fmt.Fprintf(os.Stderr, "unknown argument: %s\nusage: wt-manager [--init]\n", arg)
		os.Exit(2)
	}

	cfg, err := defaultConfig(forceSetup)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	p := tea.NewProgram(newModel(cfg), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "wt-manager failed: %v\n", err)
		os.Exit(1)
	}
}
