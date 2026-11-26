// Package browser opens web pages in the system browser.
package browser

import (
	"context"
	"runtime"

	"github.com/VashingMachine/wt-manager/internal/shell"
)

// Browser is the system's default browser.
type Browser struct{}

func New() *Browser {
	return &Browser{}
}

// Open shows url in the default browser.
func (b *Browser) Open(ctx context.Context, url string) error {
	cmdName := "open"
	args := []string{url}
	if runtime.GOOS == "linux" {
		cmdName = "xdg-open"
	}
	if runtime.GOOS == "windows" {
		cmdName = "cmd"
		args = []string{"/c", "start", url}
	}
	_, err := shell.Run(ctx, "", cmdName, args...)
	return err
}
