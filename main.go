package main

import (
	"fmt"
	"os"

	"github.com/VashingMachine/wt-manager/internal/app"
	"github.com/VashingMachine/wt-manager/internal/cli"
	"github.com/VashingMachine/wt-manager/internal/tui"
)

const usage = "usage: wt-manager [--init]\n" + cli.Usage

func main() {
	if cli.IsCommand(os.Args[1:]) {
		os.Exit(cli.Main(os.Args[1:], app.NewServices(nil)))
	}
	forceSetup := false
	for _, arg := range os.Args[1:] {
		if arg == "--init" {
			forceSetup = true
			continue
		}
		if arg == "-h" || arg == "--help" {
			fmt.Println(usage)
			return
		}
		fmt.Fprintf(os.Stderr, "unknown argument: %s\n%s\n", arg, usage)
		os.Exit(2)
	}

	application := app.New()
	cfg, err := application.DefaultConfig(forceSetup)
	if err != nil {
		application.Close()
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	p := tui.NewProgram(cfg, application.Services())
	_, err = p.Run()
	application.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "wt-manager failed: %v\n", err)
		os.Exit(1)
	}
}
