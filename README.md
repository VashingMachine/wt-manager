# wt-manager

`wt-manager` is a terminal UI for keeping Git worktrees under control across one or more repository profiles.

It gives you a compact dashboard for every worktree in a repository: branch, dirty state, ahead/behind counts, changed files, pull request metadata, and the latest commit. From the same screen you can create, delete, filter, refresh, open, and jump into worktrees without remembering the exact `git worktree` commands.

## Highlights

- Browse managed worktrees in a Bubble Tea terminal interface.
- See branch state, changed-file counts, PR state, and latest commit at a glance.
- Switch between repository profiles from inside the app.
- Create worktrees from existing local branches, remote branches, or new branches.
- Delete worktrees with a double-press confirmation.
- Open the selected worktree in VS Code.
- Open the selected PR in your browser.
- Open the selected worktree in a new Ghostty tab with a configured AI coding agent.
- Configure everything interactively on first run, including repository path, worktree folder, GitHub repo, and agent commands.

## Requirements

- Go 1.26 or newer
- Git
- GitHub CLI (`gh`) for pull request lookup
- VS Code CLI (`code`) if you want the `v` shortcut
- Ghostty on macOS if you want the `i` shortcut for agent sessions

`gh` is optional for core worktree management. If it is missing, unauthenticated, or cannot access the configured repository, `wt-manager` still runs and simply disables PR lookup.

## Install

Install the latest public version:

```bash
go install github.com/VashingMachine/wt-manager@latest
```

Or build from a local checkout:

```bash
go build -o ~/go/bin/wt-manager .
```

Then run:

```bash
wt-manager
```

To rerun setup manually:

```bash
wt-manager --init
```

## Configuration

`wt-manager` stores configuration in `~/.wt-manager.json`.

If the file is missing, has no profiles, or the current Git repository is not configured yet, the app opens an interactive setup wizard. The wizard can detect the current repository, choose a worktree folder, detect installed agent tools, infer the GitHub `owner/repo` value from `origin`, and verify `gh` access as a warning.

Example:

```json
{
  "defaultProfile": "app",
  "profiles": [
    {
      "name": "app",
      "repositoryPath": "~/projects/app",
      "worktreesDir": "~/worktrees/app",
      "remoteName": "origin",
      "githubRepo": "owner/app"
    }
  ],
  "defaultAgent": "codex",
  "agents": [
    { "name": "codex", "command": "codex" },
    { "name": "opencode", "command": "opencode" },
    { "name": "claude", "command": "claude" },
    { "name": "copilot", "command": "copilot" }
  ]
}
```

If `githubRepo` is empty, `wt-manager` tries to infer it from the configured remote when that remote points to GitHub.

## Keybindings

| Key | Action |
| --- | --- |
| `j` / `k`, arrows | Move selection |
| `tab` | Switch focus between table and details |
| `r` | Refresh |
| `a` | Toggle between managed worktrees and all repository worktrees |
| `/` | Filter by worktree name or branch |
| `esc` | Clear filter or cancel the current input |
| `n` | Create a worktree |
| `s` | Switch repository profile |
| `I` | Open setup |
| `p` | Toggle full PR description |
| `o` | Open selected PR in browser |
| `v` | Open selected worktree in VS Code |
| `i` | Open selected worktree in Ghostty with the configured agent |
| `m` | Choose the default agent |
| `d d` | Delete selected worktree |
| `q`, `ctrl+c` | Quit |

## How Worktrees Are Created

New worktrees are created under `<worktreesDir>/<branch-with-slashes-replaced-by-dashes>`.

When you create a worktree, `wt-manager`:

1. Reuses an existing local branch when it exists.
2. Tracks a matching remote branch when one exists.
3. Creates a new branch from the remote default branch when possible.
4. Falls back to the current repository state when no remote default branch is available.

Missing or prunable worktree entries are surfaced in the UI, and Git's worktree prune operation is used when cleanup is needed.

## Agent Sessions

The `i` shortcut opens the selected worktree in Ghostty and runs the configured agent command. Built-in defaults are `codex`, `opencode`, `claude`, and `copilot`, and custom commands can be added during setup.

On macOS, opening a new Ghostty tab uses UI automation. If macOS blocks the tab automation, grant Accessibility permission to your terminal or `wt-manager`.
