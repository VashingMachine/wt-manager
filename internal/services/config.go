package services

import (
	"context"
	"os"
	"path/filepath"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/fspath"
)

// DefaultConfig loads the settings and decides what the app starts with: the
// active project, or setup when there is nothing to show yet (no config, no
// project, or a repository the config does not know).
func (s *Service) DefaultConfig(forceSetup bool) (core.Config, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return core.Config{}, err
	}

	appConfig, configPath, existed, err := s.config.Load(homeDir)
	if err != nil {
		return core.Config{}, err
	}

	currentRepo, hasCurrentRepo := s.git.RepoRootFromCWD()
	cfg := core.Config{HomeDir: homeDir, ConfigPath: configPath, App: appConfig, ConfigExists: existed}
	if forceSetup {
		cfg.SetupNeeded = true
		cfg.SetupReason = "Manual setup requested"
		cfg.SetupRepo = currentRepo
		return cfg, nil
	}

	if len(appConfig.Profiles) == 0 {
		cfg.SetupNeeded = true
		if existed {
			cfg.SetupReason = "No repository profiles configured"
		} else {
			cfg.SetupReason = "No config found"
		}
		cfg.SetupRepo = currentRepo
		return cfg, nil
	}

	if hasCurrentRepo && !repoConfigured(appConfig, homeDir, currentRepo) {
		cfg.SetupNeeded = true
		cfg.SetupReason = "Current repository is not configured"
		cfg.SetupRepo = currentRepo
		return cfg, nil
	}

	activeProfile, err := core.ActiveProfile(appConfig, homeDir)
	if err != nil {
		cfg.SetupNeeded = true
		cfg.SetupReason = err.Error()
		cfg.SetupRepo = currentRepo
		return cfg, nil
	}

	cfg.ActiveProfile = s.inferRepositorySlugs(context.Background(), activeProfile)
	cfg.RepoSlug = cfg.ActiveProfile.GitHubRepo
	cfg.ConfigExists = existed
	return cfg, nil
}

// inferRepositorySlugs fills in the GitHub slug of every repository that does
// not configure one, from its remote.
func (s *Service) inferRepositorySlugs(ctx context.Context, profile core.RepositoryProfile) core.RepositoryProfile {
	repositories := append([]core.Repository(nil), profile.Repositories...)
	for i := range repositories {
		if repositories[i].GitHubRepo == "" {
			repositories[i].GitHubRepo = s.git.GitHubRepo(ctx, core.RepositoryProfile{RepositoryPath: repositories[i].RepositoryPath, RemoteName: repositories[i].RemoteName})
		}
	}
	profile.Repositories = repositories
	if len(repositories) > 0 {
		profile.GitHubRepo = repositories[0].GitHubRepo
	}
	return profile
}

func (s *Service) ResolveProject(ctx context.Context, profile core.RepositoryProfile, homeDir string) core.RepositoryProfile {
	ctx = s.commandContext(ctx)
	return s.inferRepositorySlugs(ctx, core.ResolveProfilePaths(profile, homeDir))
}

func (s *Service) ProfileFromRepo(ctx context.Context, homeDir string, repoPath string) core.RepositoryProfile {
	ctx = s.commandContext(ctx)
	profile := core.RepositoryProfile{
		Name:           filepath.Base(repoPath),
		RepositoryPath: repoPath,
		WorktreesDir:   filepath.Dir(repoPath),
		RemoteName:     "origin",
	}
	profile.GitHubRepo = s.git.GitHubRepo(ctx, core.ResolveProfilePaths(profile, homeDir))
	return profile
}

// repoConfigured reports whether a repository belongs to any configured project.
func repoConfigured(appConfig core.AppConfig, homeDir string, repoPath string) bool {
	want := fspath.Canonical(repoPath)
	for _, profile := range appConfig.Profiles {
		for _, repository := range core.ResolveProfilePaths(profile, homeDir).Repositories {
			if fspath.Canonical(repository.RepositoryPath) == want {
				return true
			}
		}
	}
	return false
}

func (s *Service) ReadAppConfig(configPath string) (core.AppConfig, error) {
	return s.config.Read(configPath)
}

func (s *Service) WriteAppConfig(configPath string, appConfig core.AppConfig) error {
	return s.config.Write(configPath, appConfig)
}

func (s *Service) InferRepoFromCWD() (string, bool) {
	return s.git.RepoRootFromCWD()
}

func (s *Service) RepoRootFromDir(ctx context.Context, dir string) (string, bool) {
	ctx = s.commandContext(ctx)
	return s.git.RepoRoot(ctx, dir)
}

func (s *Service) InferGitHubRepo(ctx context.Context, profile core.RepositoryProfile) string {
	ctx = s.commandContext(ctx)
	return s.git.GitHubRepo(ctx, profile)
}

func (s *Service) GitRemotes(ctx context.Context, repoPath string) []string {
	ctx = s.commandContext(ctx)
	return s.git.Remotes(ctx, repoPath)
}

func (s *Service) CheckGitHubCLI(ctx context.Context, repoSlug string) []string {
	ctx = s.commandContext(ctx)
	return s.github.CheckCLI(ctx, repoSlug)
}
