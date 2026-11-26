package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/shell"
)

func inferRepoFromCWD() (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cwd, err := os.Getwd()
	if err != nil {
		return "", false
	}
	return repoRootFromDir(ctx, cwd)
}

func repoRootFromDir(ctx context.Context, dir string) (string, bool) {
	output, err := shell.Run(ctx, dir, "git", "-C", dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", false
	}
	repoPath := filepath.Clean(strings.TrimSpace(output))
	if repoPath == "" {
		return "", false
	}
	return repoPath, true
}

// gitRemotes lists the remotes configured in a repository.
func gitRemotes(ctx context.Context, repoPath string) []string {
	output, err := shell.Run(ctx, repoPath, "git", "-C", repoPath, "remote")
	if err != nil {
		return nil
	}
	return strings.Fields(output)
}

func inferGitHubRepo(ctx context.Context, profile core.RepositoryProfile) string {
	remoteName := profile.RemoteName
	if remoteName == "" {
		remoteName = "origin"
	}
	output, err := shell.Run(ctx, profile.RepositoryPath, "git", "-C", profile.RepositoryPath, "config", "--get", "remote."+remoteName+".url")
	if err != nil {
		return ""
	}
	return core.GitHubSlugFromRemoteURL(strings.TrimSpace(output))
}
