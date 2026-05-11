# wt-manager

`wt-manager` is a terminal UI for keeping Git worktrees under control across one or more repository profiles.

It gives you a compact dashboard for every worktree in a repository: branch, dirty state, ahead/behind counts, changed files, pull request metadata, and the latest commit. From the same screen you can create, delete, filter, refresh, open, and jump into worktrees without remembering the exact `git worktree` commands.

It also includes PR Radar, a remote pull request view backed by GitHub CLI. PR Radar lets you browse open, closed, and merged PRs for the active repository profile, filter them by user or text, inspect files/checks/comments/diffs, approve selected PRs, create a worktree from a PR branch, and ask a configured AI agent about the selected PR context.

## Highlights

- Browse managed worktrees in a Bubble Tea terminal interface.
- See branch state, changed-file counts, PR state, and latest commit at a glance.
- Switch between repository profiles from inside the app.
- Create worktrees from existing local branches, remote branches, or new branches.
- Delete worktrees with a double-press confirmation.
- Open the selected worktree in VS Code.
- Open the selected PR in your browser.
- Open the selected worktree in a new Ghostty tab with a configured AI coding agent.
- Browse remote repository PRs without creating local worktrees first.
- Filter remote PRs by state, text, and involved GitHub users.
- Inspect selected PR diffs in a full-terminal view with coloured Go and Python code.
- Approve selected PRs through GitHub CLI after confirmation.
- Ask a configured local AI agent about the selected remote PR using PR metadata, files, comments, and diff context.
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
| `h` | Show keybinding help |
| `tab` | Switch focus between table and details |
| `r` | Refresh |
| `P` | Toggle PR Radar mode |
| `a` | Toggle between managed worktrees and all repository worktrees |
| `/` | Filter by worktree name or branch |
| `S` | Cycle PR Radar state filter |
| `u` | Choose PR Radar user filter |
| `Y` | Toggle PR Radar author filter for your GitHub user |
| `b` | Toggle PR Radar final column between title and branch |
| `f` | Toggle failed GitHub Actions in PR details |
| `enter` | Open selected PR full-screen diff in PR Radar |
| `esc` | Close full-screen PR diff, clear filter, or cancel the current input |
| `A` | Approve selected PR in PR Radar after confirmation |
| `n` | Create a worktree |
| `s` | Switch repository profile |
| `I` | Open setup |
| `p` | Toggle full PR description |
| `o` | Open selected PR in browser |
| `c` | Ask configured agent about the selected remote PR |
| `v` | Open selected worktree in VS Code, or create/reuse selected PR worktree in PR Radar first |
| `i` | Open selected worktree in Ghostty with the configured agent, or create/reuse selected PR worktree in PR Radar first |
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

## PR Radar

Press `P` to toggle PR Radar. The app uses `gh` for GitHub access, so no separate OAuth app registration is required. If `gh` is missing, unauthenticated, or cannot access the configured repository, PR Radar shows a status error with the next action.

In PR Radar:

1. Press `/` to filter by title, branch, label, author, review decision, or PR number.
2. Press `S` to cycle state filters: open, all, closed, merged.
3. Press `u` to filter by users discovered from authors, assignees, and review requests.
4. Press `Y` to quickly show only PRs authored by your authenticated GitHub user.
5. Press `b` to switch the final table column between PR title and branch name.
6. Press `o` to open the selected PR in your browser.
7. Press `enter` to open a full-screen coloured diff view, then `esc` to return to the PR list.
8. Press `A` to approve the selected PR after a confirmation prompt. Draft PRs, non-open PRs, failing or pending checks, and requested changes are shown as warnings, but you can still confirm.
9. Press `n` to create a local worktree from the selected PR branch.
10. Press `v` to create or reuse a PR worktree and open it in VS Code.
11. Press `i` to create or reuse a PR worktree and open it with the configured agent.
12. Press `c` to ask an agent about the selected PR.

The `c` action prefers a configured `copilot` agent when present and otherwise falls back to the default agent. The selected command receives a prompt on stdin containing PR metadata, description, files, recent comments, and a truncated diff. Authentication for the agent remains owned by that local CLI.
