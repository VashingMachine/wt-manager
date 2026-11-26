package github

import (
	"context"
	"errors"
	"fmt"
	"os/exec"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/shell"
)

func checkGitHubCLI(ctx context.Context, repoSlug string) []string {
	if _, err := exec.LookPath("gh"); err != nil {
		return []string{"gh not found; PR lookup will be disabled until gh is installed"}
	}
	if _, err := shell.Run(ctx, "", "gh", "--version"); err != nil {
		return []string{"gh is installed but not runnable; PR lookup may be unavailable"}
	}
	if _, err := shell.Run(ctx, "", "gh", "auth", "status"); err != nil {
		return []string{"gh is not authenticated; PR lookup will be unavailable until gh auth login"}
	}
	if repoSlug != "" {
		if _, err := shell.Run(ctx, "", "gh", "repo", "view", repoSlug); err != nil {
			return []string{fmt.Sprintf("gh cannot access %s; PR lookup may be unavailable", repoSlug)}
		}
	}
	return nil
}

func requireGitHub(cfg core.Config) error {
	if cfg.RepoSlug == "" {
		return errors.New("GitHub repository is not configured")
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return errors.New("gh not found; run setup after installing GitHub CLI")
	}
	return nil
}
