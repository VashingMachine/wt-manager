package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
)

// sweepTimeout bounds one sweep run; scanning reads the status of every
// worktree and removing deletes whole checkouts.
const sweepTimeout = 5 * time.Minute

type sweepTarget struct {
	cfg       core.Config
	candidate core.SweepCandidate
}

// runSweep removes worktrees that are clean and have been untouched for
// longer than the configured number of days, from the command line: the same
// rules as X in the app, usable from a script or a scheduled job with --yes.
func (c *command) runSweep(args []string) (int, error) {
	flags := flag.NewFlagSet("wt-manager sweep", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dryRun := flags.Bool("dry-run", false, "")
	yes := flags.Bool("yes", false, "")
	days := flags.Int("days", 0, "")
	project := flags.String("project", "", "")
	if err := flags.Parse(args); err != nil || flags.NArg() > 0 {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(c.opts.Out, Usage)
			return 0, nil
		}
		fmt.Fprintf(c.opts.Out, "unknown sweep arguments\n%s\n", Usage)
		return 2, nil
	}

	app, _, _, err := c.loadConfig()
	if err != nil {
		return 1, err
	}
	profile, err := c.sweepProject(app, *project)
	if err != nil {
		return 1, err
	}
	olderThanDays := *days
	if olderThanDays <= 0 {
		olderThanDays = app.Sweep.OlderThanDays
	}
	olderThan := core.SweepConfig{OlderThanDays: olderThanDays}.OlderThan()
	olderThanDays = int(olderThan / (24 * time.Hour))

	ctx, cancel := context.WithTimeout(context.Background(), sweepTimeout)
	defer cancel()
	resolved := c.services().ResolveProject(ctx, profile, c.opts.HomeDir)
	cfg := core.Config{HomeDir: c.opts.HomeDir, App: app, ActiveProfile: resolved, RepoSlug: resolved.GitHubRepo}

	var targets []sweepTarget
	for _, repository := range cfg.Repositories() {
		scoped := cfg.ForRepository(repository.Name)
		candidates, err := c.services().SweepCandidates(ctx, scoped, olderThan)
		if err != nil {
			return 1, fmt.Errorf("%s: %w", repository.Name, err)
		}
		for _, candidate := range candidates {
			targets = append(targets, sweepTarget{cfg: scoped, candidate: candidate})
		}
	}
	sort.SliceStable(targets, func(i, j int) bool {
		return targets[i].candidate.LastActivity.Before(targets[j].candidate.LastActivity)
	})

	if len(targets) == 0 {
		fmt.Fprintf(c.opts.Out, "Nothing to sweep in %s: no clean worktree has been untouched for %d days.\n", profile.Name, olderThanDays)
		return 0, nil
	}
	fmt.Fprintf(c.opts.Out, "Clean worktrees of %s untouched for %d+ days:\n", profile.Name, olderThanDays)
	table := tabwriter.NewWriter(c.opts.Out, 0, 0, 2, ' ', 0)
	for _, target := range targets {
		wt := target.candidate.Worktree
		idle := int(time.Since(target.candidate.LastActivity) / (24 * time.Hour))
		fmt.Fprintf(table, "  %s\t%s\t%dd\t%s\n", wt.Repository, wt.Branch, idle, c.compact(wt.Path))
	}
	if err := table.Flush(); err != nil {
		return 1, err
	}
	if *dryRun {
		fmt.Fprintln(c.opts.Out, "Dry run: nothing was removed.")
		return 0, nil
	}
	if !*yes {
		remove, err := c.prompt.confirm(fmt.Sprintf("Remove %s? Their branches stay.", countWorktrees(len(targets))), false)
		if err != nil {
			return 1, err
		}
		if !remove {
			fmt.Fprintln(c.opts.Out, "Nothing was removed.")
			return 0, nil
		}
	}

	failed := 0
	for _, target := range targets {
		wt := target.candidate.Worktree
		// The candidate carries the clean status just read, so git removes the
		// worktree without force and refuses if it changed since.
		if err := c.services().RemoveWorktree(ctx, target.cfg, wt); err != nil {
			failed++
			fmt.Fprintf(c.opts.Out, "  failed   %s: %v\n", c.compact(wt.Path), err)
			continue
		}
		fmt.Fprintf(c.opts.Out, "  removed  %s\n", c.compact(wt.Path))
	}
	if failed > 0 {
		return 1, fmt.Errorf("%s could not be removed", countWorktrees(failed))
	}
	return 0, nil
}

// sweepProject picks the project to sweep: the named one, else the one the
// command runs in, else the default project.
func (c *command) sweepProject(app core.AppConfig, name string) (core.RepositoryProfile, error) {
	if len(app.Profiles) == 0 {
		return core.RepositoryProfile{}, errors.New("no projects are configured; run wt-manager --init first")
	}
	if name != "" {
		if idx := profileIndex(app, name); idx >= 0 {
			return app.Profiles[idx], nil
		}
		return core.RepositoryProfile{}, fmt.Errorf("no project named %q; choose one of: %s", name, projectNames(app))
	}
	if working := c.workingRepository(); working != "" {
		if idx, _ := c.repositoryOwner(app, working); idx >= 0 {
			return app.Profiles[idx], nil
		}
	}
	if idx := profileIndex(app, app.DefaultProfile); idx >= 0 {
		return app.Profiles[idx], nil
	}
	return app.Profiles[0], nil
}

func countWorktrees(count int) string {
	if count == 1 {
		return "1 worktree"
	}
	return fmt.Sprintf("%d worktrees", count)
}
