package core

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// RepositoryProfile is a project: one or more repositories managed together,
// sharing a JIRA setup. A single-repository project keeps the flat
// repositoryPath/worktreesDir/remoteName/githubRepo fields; a project with
// several repositories lists them under repositories instead.
//
// In a resolved Config.ActiveProfile, Repositories always lists every
// repository and the flat fields describe the repository the Config is scoped
// to (see Config.ForRepository), so per-repository services keep reading them.
type RepositoryProfile struct {
	Name           string       `json:"name"`
	RepositoryPath string       `json:"repositoryPath,omitempty"`
	WorktreesDir   string       `json:"worktreesDir,omitempty"`
	RemoteName     string       `json:"remoteName,omitempty"`
	GitHubRepo     string       `json:"githubRepo,omitempty"`
	Repositories   []Repository `json:"repositories,omitempty"`
	// LastRepository names the repository the last worktree was created in,
	// offered first the next time the app asks for one.
	LastRepository string      `json:"lastRepository,omitempty"`
	Jira           *JiraConfig `json:"jira,omitempty"`
}

// Repository is one git repository of a project.
type Repository struct {
	Name           string `json:"name"`
	RepositoryPath string `json:"repositoryPath"`
	WorktreesDir   string `json:"worktreesDir"`
	RemoteName     string `json:"remoteName,omitempty"`
	GitHubRepo     string `json:"githubRepo,omitempty"`
}

// RepositoryList returns the project's repositories in configured order: the
// repositories list, or the flat fields as the only repository.
func (p RepositoryProfile) RepositoryList() []Repository {
	if len(p.Repositories) > 0 {
		return p.Repositories
	}
	if strings.TrimSpace(p.RepositoryPath) == "" {
		return nil
	}
	return []Repository{{
		Name:           RepositoryNameFromPath(p.RepositoryPath),
		RepositoryPath: p.RepositoryPath,
		WorktreesDir:   p.WorktreesDir,
		RemoteName:     p.RemoteName,
		GitHubRepo:     p.GitHubRepo,
	}}
}

// Repository returns the repository with the given name, or nil.
func (p RepositoryProfile) Repository(name string) *Repository {
	repositories := p.RepositoryList()
	for i := range repositories {
		if repositories[i].Name == name {
			return &repositories[i]
		}
	}
	return nil
}

// WithRepositories returns the profile listing repositories in place of its
// flat single-repository fields.
func (p RepositoryProfile) WithRepositories(repositories []Repository) RepositoryProfile {
	p.RepositoryPath, p.WorktreesDir, p.RemoteName, p.GitHubRepo = "", "", "", ""
	p.Repositories = repositories
	if p.LastRepository != "" && p.Repository(p.LastRepository) == nil {
		p.LastRepository = ""
	}
	return p
}

// UniqueRepositoryName returns name, with a numeric suffix when the project
// already has a repository of that name.
func (p RepositoryProfile) UniqueRepositoryName(name string) string {
	if name == "" {
		name = "repository"
	}
	candidate := name
	for suffix := 2; p.Repository(candidate) != nil; suffix++ {
		candidate = fmt.Sprintf("%s-%d", name, suffix)
	}
	return candidate
}

// SuggestWorktreesDir proposes the worktrees folder of a repository joining a
// project, following the project's layout: repositories keeping worktrees in
// a folder named after themselves (~/worktrees/api) suggest a sibling named
// after the new one; otherwise the first repository's folder is shared, where
// worktree folders get the repository name as a prefix. A project without
// repositories keeps worktrees next to the repository, as setup does.
func SuggestWorktreesDir(repositories []Repository, name string, repoPath string) string {
	for _, repository := range repositories {
		base := filepath.Base(repository.WorktreesDir)
		if base == repository.Name || base == filepath.Base(repository.RepositoryPath) {
			return filepath.Join(filepath.Dir(repository.WorktreesDir), name)
		}
	}
	if len(repositories) > 0 && repositories[0].WorktreesDir != "" {
		return repositories[0].WorktreesDir
	}
	return filepath.Dir(repoPath)
}

// RepositoryNameFromPath is the default repository name: its folder name.
func RepositoryNameFromPath(path string) string {
	path = strings.TrimRight(strings.TrimSpace(path), `/\`)
	if path == "" {
		return ""
	}
	return filepath.Base(path)
}

// Repositories lists the active project's repositories.
func (c Config) Repositories() []Repository {
	repositories := c.ActiveProfile.RepositoryList()
	if len(c.ActiveProfile.Repositories) == 0 && len(repositories) == 1 && repositories[0].GitHubRepo == "" {
		repositories[0].GitHubRepo = c.RepoSlug
	}
	return repositories
}

// MultiRepository reports whether the active project has more than one
// repository, which is when the UI shows and asks for repositories.
func (c Config) MultiRepository() bool {
	return len(c.ActiveProfile.Repositories) > 1
}

// RepositoryName names the repository the Config is scoped to.
func (c Config) RepositoryName() string {
	for _, repository := range c.Repositories() {
		if repository.RepositoryPath == c.ActiveProfile.RepositoryPath {
			return repository.Name
		}
	}
	return RepositoryNameFromPath(c.ActiveProfile.RepositoryPath)
}

// ForRepository scopes the Config to one repository of the active project: the
// profile's flat fields and RepoSlug describe that repository afterwards. An
// unknown name leaves the Config unchanged.
func (c Config) ForRepository(name string) Config {
	for _, repository := range c.ActiveProfile.Repositories {
		if repository.Name != name {
			continue
		}
		c.ActiveProfile.RepositoryPath = repository.RepositoryPath
		c.ActiveProfile.WorktreesDir = repository.WorktreesDir
		c.ActiveProfile.RemoteName = repository.RemoteName
		c.ActiveProfile.GitHubRepo = repository.GitHubRepo
		c.RepoSlug = repository.GitHubRepo
		return c
	}
	// A project without a repositories list has one repository, which the
	// Config already describes.
	return c
}

// SharesWorktreesDir reports whether another repository of the project puts
// its worktrees in the same folder as the scoped one, so folder names need the
// repository name to stay apart.
func (c Config) SharesWorktreesDir() bool {
	if !c.MultiRepository() {
		return false
	}
	scoped := filepath.Clean(c.ActiveProfile.WorktreesDir)
	for _, repository := range c.ActiveProfile.Repositories {
		if repository.RepositoryPath != c.ActiveProfile.RepositoryPath && filepath.Clean(repository.WorktreesDir) == scoped {
			return true
		}
	}
	return false
}

// normalizeProfileRepositories fills in repository defaults. A project keeps
// its single repository in the flat fields until it lists repositories; flat
// fields next to a list describe one more repository and are moved into it.
func normalizeProfileRepositories(profile *RepositoryProfile) (bool, error) {
	changed := false
	if len(profile.Repositories) == 0 {
		if profile.RemoteName == "" {
			profile.RemoteName = "origin"
			changed = true
		}
	} else {
		if profile.RepositoryPath != "" {
			flat := Repository{
				Name:           RepositoryNameFromPath(profile.RepositoryPath),
				RepositoryPath: profile.RepositoryPath,
				WorktreesDir:   profile.WorktreesDir,
				RemoteName:     profile.RemoteName,
				GitHubRepo:     profile.GitHubRepo,
			}
			listed := false
			for _, repository := range profile.Repositories {
				if filepath.Clean(strings.TrimSpace(repository.RepositoryPath)) == filepath.Clean(flat.RepositoryPath) {
					listed = true
					break
				}
			}
			if !listed {
				profile.Repositories = append([]Repository{flat}, profile.Repositories...)
			}
		}
		if profile.RepositoryPath != "" || profile.WorktreesDir != "" || profile.RemoteName != "" || profile.GitHubRepo != "" {
			profile.RepositoryPath, profile.WorktreesDir, profile.RemoteName, profile.GitHubRepo = "", "", "", ""
			changed = true
		}
		seen := map[string]struct{}{}
		for i := range profile.Repositories {
			repository := &profile.Repositories[i]
			before := *repository
			repository.Name = strings.TrimSpace(repository.Name)
			repository.RepositoryPath = strings.TrimSpace(repository.RepositoryPath)
			repository.WorktreesDir = strings.TrimSpace(repository.WorktreesDir)
			repository.RemoteName = strings.TrimSpace(repository.RemoteName)
			repository.GitHubRepo = strings.TrimSpace(repository.GitHubRepo)
			if repository.Name == "" {
				repository.Name = RepositoryNameFromPath(repository.RepositoryPath)
			}
			if repository.RemoteName == "" {
				repository.RemoteName = "origin"
			}
			if *repository != before {
				changed = true
			}
			if repository.RepositoryPath == "" {
				return false, fmt.Errorf("profile %q repository %d repositoryPath is required", profile.Name, i+1)
			}
			if repository.WorktreesDir == "" {
				return false, fmt.Errorf("profile %q repository %q worktreesDir is required", profile.Name, repository.Name)
			}
			if _, ok := seen[repository.Name]; ok {
				return false, fmt.Errorf("profile %q has two repositories named %q; give each a unique name", profile.Name, repository.Name)
			}
			seen[repository.Name] = struct{}{}
		}
	}
	profile.LastRepository = strings.TrimSpace(profile.LastRepository)
	if profile.LastRepository != "" && profile.Repository(profile.LastRepository) == nil {
		profile.LastRepository = ""
		changed = true
	}
	return changed, nil
}

func validateProfile(profile RepositoryProfile) error {
	if profile.Name == "" {
		return errors.New("profile name is required")
	}
	if len(profile.Repositories) > 0 {
		// Repositories were validated while being normalized.
		return nil
	}
	if profile.RepositoryPath == "" {
		return fmt.Errorf("profile %q repositoryPath is required", profile.Name)
	}
	if profile.WorktreesDir == "" {
		return fmt.Errorf("profile %q worktreesDir is required", profile.Name)
	}
	if profile.RemoteName == "" {
		return fmt.Errorf("profile %q remoteName is required", profile.Name)
	}
	return nil
}

// ActiveProfile returns the default project with its paths resolved.
func ActiveProfile(appConfig AppConfig, homeDir string) (RepositoryProfile, error) {
	if len(appConfig.Profiles) == 0 {
		return RepositoryProfile{}, errors.New("no repository profiles configured")
	}
	profile := FindProfile(appConfig.Profiles, appConfig.DefaultProfile)
	if profile == nil {
		return RepositoryProfile{}, fmt.Errorf("default profile %q not found", appConfig.DefaultProfile)
	}
	return ResolveProfilePaths(*profile, homeDir), nil
}

// ResolveProfilePaths expands every path of a project, lists all of its
// repositories under Repositories and scopes the flat fields to the first.
func ResolveProfilePaths(profile RepositoryProfile, homeDir string) RepositoryProfile {
	listed := profile.RepositoryList()
	repositories := make([]Repository, len(listed))
	for i, repository := range listed {
		repository.RepositoryPath = ExpandPath(repository.RepositoryPath, homeDir)
		repository.WorktreesDir = ExpandPath(repository.WorktreesDir, homeDir)
		if repository.RemoteName == "" {
			repository.RemoteName = "origin"
		}
		repositories[i] = repository
	}
	profile.Repositories = repositories
	if len(repositories) > 0 {
		primary := repositories[0]
		profile.RepositoryPath = primary.RepositoryPath
		profile.WorktreesDir = primary.WorktreesDir
		profile.RemoteName = primary.RemoteName
		profile.GitHubRepo = primary.GitHubRepo
	}
	return profile
}

// ExpandPath replaces a leading ~ with the home directory, the way paths are
// written in the config file.
func ExpandPath(path string, homeDir string) string {
	if path == "~" {
		return homeDir
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(homeDir, strings.TrimPrefix(path, "~/"))
	}
	return path
}

// CreateWorktreePath is the folder a worktree for a branch is created in:
// the branch name with slashes as dashes, inside the worktrees folder of the
// repository cfg is scoped to.
func CreateWorktreePath(cfg Config, name string) string {
	worktreeName := strings.ReplaceAll(name, "/", "-")
	if cfg.SharesWorktreesDir() {
		// The same branch (say, one JIRA ticket) is often created in several
		// repositories of a project; a shared folder keeps them apart by name.
		worktreeName = cfg.RepositoryName() + "-" + worktreeName
	}
	return filepath.Join(cfg.ActiveProfile.WorktreesDir, worktreeName)
}
