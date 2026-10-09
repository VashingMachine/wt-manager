# wt-manager

`wt-manager` is a terminal UI for keeping Git worktrees under control across one or more repository profiles.

It gives you a compact dashboard for every worktree in a repository: branch, dirty state, ahead/behind counts, changed files, pull request metadata, and the latest commit. From the same screen you can create, delete, filter, refresh, open, and jump into worktrees without remembering the exact `git worktree` commands.

It also includes PR Radar, a remote pull request view backed by GitHub CLI. PR Radar lets you browse open, closed, and merged PRs for the active repository profile, filter them by user or text, inspect files/checks/comments/diffs, approve selected PRs, create a worktree from a PR branch, and ask a configured AI agent about the selected PR context.

Profiles can optionally enable a third JIRA Tickets view backed by Atlassian CLI. It shows board tickets with their workflow status, matching local worktree, and matching GitHub pull request, and supports reading and updating the selected ticket without leaving the terminal.

## Highlights

- Browse managed worktrees in a Bubble Tea terminal interface.
- See branch state, changed-file counts, PR state, and latest commit at a glance.
- Switch between repository profiles from inside the app.
- Create worktrees from existing local branches, remote branches, or new branches.
- Delete worktrees with a double-press confirmation.
- Configure what `enter` does for selected worktrees.
- Search commands, settings, profiles, and worktrees from a colorful command palette.
- Inspect full execution output and errors in a scrollable session log viewer.
- Open the selected worktree in your configured editor or IDE.
- Open the selected PR in your browser.
- Open the selected worktree in a new Ghostty tab with a configured AI coding agent.
- Detect when it runs inside [cmux](https://cmux.com) and open worktrees as cmux workspaces, with PR and JIRA pages in cmux browser splits.
- Browse remote repository PRs without creating local worktrees first.
- Filter remote PRs by state, text, and involved GitHub users.
- Inspect selected PR diffs in a full-terminal view with coloured Go and Python code.
- Approve selected PRs through GitHub CLI after confirmation.
- Ask a configured local AI agent about the selected remote PR using PR metadata, files, comments, and diff context.
- Browse configured JIRA boards with assigned-to-me and active-sprint scopes.
- Read JIRA descriptions and comments, open tickets in the browser, create ticket worktrees, transition status, comment, and assign users.
- Paint cached ticket rows immediately and refresh them in the background.
- Configure everything interactively on first run, including repository path, worktree folder, GitHub repo, and agent commands.

## Requirements

- Go 1.26 or newer
- Git
- GitHub CLI (`gh`) for pull request lookup
- Atlassian CLI (`acli`) for optional JIRA support
- Your editor's launcher (`code`, `idea`, `cursor`, `zed`, etc.) if you want the `v` shortcut
- A supported terminal on macOS (cmux, Ghostty, Warp, or Terminal) if you want the `i` shortcut to open an agent in a new tab

`gh` is optional for core worktree management. If it is missing, unauthenticated, or cannot access the configured repository, `wt-manager` still runs and simply disables PR lookup.

JIRA is also optional. To enable it, install ACLI and authenticate its Jira app:

```bash
acli jira auth login
acli jira auth status
```

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
      "githubRepo": "owner/app",
      "jira": {
        "site": "example.atlassian.net",
        "boards": [
          {
            "url": "https://example.atlassian.net/jira/software/c/projects/APP/boards/123",
            "id": 123,
            "name": "App Board",
            "projects": ["APP"]
          }
        ],
        "defaultBoard": "App Board",
        "mineOnly": true,
        "sprintOnly": true,
        "openOnly": true,
        "filtersConfigured": true,
        "limit": 100
      }
    }
  ],
  "defaultAgent": "codex",
  "agents": [
    { "name": "codex", "command": "codex" },
    { "name": "opencode", "command": "opencode" },
    { "name": "claude", "command": "claude" },
    { "name": "copilot", "command": "copilot" }
  ],
  "defaultEnterAction": "vscode",
  "editor": { "name": "VS Code", "command": "code" }
}
```

If `githubRepo` is empty, `wt-manager` tries to infer it from the configured remote when that remote points to GitHub.

## Keybindings

| Key | Action |
| --- | --- |
| `ctrl+p` / mapped `⌘P` | Open the searchable command palette |
| `j` / `k`, arrows | Move selection |
| `h` | Show keybinding help |
| `tab` | Switch focus between table and details |
| `r` | Refresh |
| `P` | Toggle PR Radar mode |
| `J` | Toggle JIRA Tickets mode |
| `a` | Toggle between managed worktrees and all repository worktrees |
| `/` | Filter by worktree name or branch |
| `S` | Cycle PR Radar state filter |
| `u` | Choose PR Radar user filter |
| `Y` | Toggle PR Radar author filter for your GitHub user |
| `b` | Toggle PR Radar final column between title and branch |
| `f` | Toggle failed GitHub Actions in PR details |
| `enter` | Run the selected worktree Enter action, or open selected PR full-screen diff in PR Radar |
| `e` | Choose the default worktree Enter action |
| `esc` | Close full-screen PR diff, clear filter, or cancel the current input |
| `A` | Approve selected PR in PR Radar after confirmation |
| `n` | Create a worktree |
| `s` | Switch repository profile |
| `I` | Open setup |
| `p` | Toggle full PR description |
| `o` | Open selected PR in browser |
| `c` | Ask configured agent about the selected remote PR |
| `v` | Open selected worktree in the configured editor, or create/reuse selected PR worktree in PR Radar first |
| `i` | Open selected worktree in a new terminal tab (or cmux workspace) with the configured agent, or create/reuse selected PR worktree in PR Radar first |
| `m` | Choose the default agent |
| `d d` | Delete selected worktree |
| `q`, `ctrl+c` | Quit |

In JIRA Tickets mode, `f` opens the ticket filter dialog, `Y` toggles assigned-to-me/all tickets, `S` toggles active-sprint scope, `b` selects a configured board, `t` changes status, `C` comments, `A` assigns, `n` creates or reuses a ticket worktree, `o` opens the ticket in a browser, and `enter` opens the full-screen ticket reader.

## Command Palette

Press **Ctrl+P**, type a command, setting, profile, or worktree name, then use the arrow keys and Enter to select a result. Escape closes the palette. Search is case-insensitive, supports abbreviated command names, and matches worktree branches and paths. The palette searches all worktrees in the active repository, including ones hidden by the current table filter or managed-only scope. Selecting a worktree brings it into focus; pressing `v` then opens it in your editor.

The palette includes the actions for the current view, quick choices for the default agent, editor and Enter action, and direct profile switching. Deletion and PR approval retain their existing confirmation steps. It is also available from full-screen PR diffs and ticket readers. Finish or cancel an existing text input or confirmation before opening it.

Search **logs** and run **View session logs** to investigate command execution. The viewer opens at the latest output, wraps long lines, and supports arrows or `j`/`k`, Page Up/Down, and Home/End. Press `r` to refresh or Escape to return to your previous view. Commands, working directories, timestamps, complete stdout/stderr, and errors are recorded in a private temporary `wt-manager-*.log` file; its path appears in the viewer. Each app session gets a separate file, retained after exit for investigation until temporary storage is cleaned up.

For **⌘P on macOS**, configure your terminal to send Ctrl+P (the byte `0x10`, often written `\x10`) when ⌘P is pressed. Command shortcuts are handled by the terminal itself and are not delivered as Command-modified keys by the current terminal input library. Ctrl+P works directly without that mapping.

## Editor / IDE

Open the command palette and search **Configure editor** to select VS Code, IntelliJ IDEA, Cursor, Zed, PyCharm, WebStorm, or **Custom command**. You can also search an IDE name to apply its preset directly. Changes are saved in `~/.wt-manager.json` and apply to `v`, the editor Enter action, and editor actions in PR Radar and JIRA Tickets.

For IntelliJ IDEA, the `idea` preset uses its command-line launcher. On macOS, if the launcher is missing or fails (including a stale JetBrains Toolbox script), it falls back to opening the installed **IntelliJ IDEA** application. You can also use a custom macOS app command:

```json
"editor": { "name": "IntelliJ IDEA", "command": "open -a 'IntelliJ IDEA'" }
```

Custom commands may include flags or a quoted executable path, such as `code --new-window` or `"/path/to/my IDE" --project`. The selected worktree path is appended automatically as one quoted argument. CLI launchers must be available on `PATH`. Existing configurations default to VS Code; the legacy `"defaultEnterAction": "vscode"` value now opens the configured editor.

## How Worktrees Are Created

New worktrees are created under `<worktreesDir>/<branch-with-slashes-replaced-by-dashes>`.

When you create a worktree, `wt-manager`:

1. Reuses an existing local branch when it exists.
2. Tracks a matching remote branch when one exists.
3. Creates a new branch from the remote default branch when possible.
4. Falls back to the current repository state when no remote default branch is available.

New worktrees appear in the dashboard immediately while Git creates them. A failed creation removes the temporary row and shows the error. Opening an editor, a terminal, or an agent for a worktree still being created waits until that creation succeeds.

Missing or prunable worktree entries are surfaced in the UI, and Git's worktree prune operation is used when cleanup is needed.

## Agents

The `i` shortcut opens the selected worktree in a new terminal tab and runs the configured agent command. Built-in defaults are `codex`, `opencode`, `claude`, and `copilot`, and custom commands can be added during setup.

In worktree mode, `enter` runs the configured `defaultEnterAction`: `vscode`, `agent`, or `terminal`. Press `e` to change it from the TUI. The `terminal` action opens a terminal tab in the selected worktree without running an agent command.

### cmux

When `wt-manager` runs inside a cmux terminal (detected from `CMUX_WORKSPACE_ID` / `CMUX_SURFACE_ID`, since cmux reports `TERM_PROGRAM=ghostty`), it drives cmux through its `cmux` CLI instead of UI automation:

- `i`, and `enter` when `defaultEnterAction` is `agent` or `terminal`, create a focused cmux workspace named after the worktree and rooted at its path. The `agent` action opens an agent tab plus a shell tab in one pane; `terminal` opens only the shell tab. If a workspace for that worktree already exists it is selected instead. `vscode` opens the configured editor.
- The new workspace gets the worktree's PR linked in the cmux sidebar (`cmux pr`) and a `jira` status pill when the branch name contains a ticket key.
- `o` on a PR or JIRA ticket opens the page in a cmux browser split next to `wt-manager`; later opens reuse that split.
- Approving a PR posts a cmux notification.

If the `cmux` CLI is not on `PATH`, the bundled `/Applications/cmux.app/Contents/Resources/bin/cmux` is used. cmux's socket must allow the process (default "cmux processes only" mode works because `wt-manager` runs inside cmux).

On macOS, opening a new terminal tab in other terminals uses UI automation. If macOS blocks the tab automation, grant Accessibility permission to your terminal or `wt-manager`.

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
10. Press `v` to create or reuse a PR worktree and open it in the configured editor.
11. Press `i` to create or reuse a PR worktree and open it with the configured agent.
12. Press `c` to ask an agent about the selected PR.

The `c` action prefers a configured `copilot` agent when present and otherwise falls back to the default agent. The selected command receives a prompt on stdin containing PR metadata, description, files, recent comments, and a truncated diff. Authentication for the agent remains owned by that local CLI.

## JIRA Tickets

Press `J` to toggle JIRA Tickets for the active repository profile. Run setup with `I`, paste a JIRA board link, and press enter once to resolve it and again to save. Multiple board links can be added before saving. ACLI authentication remains external to `wt-manager`; no Atlassian credentials are stored in the config.

Tickets default to the current user, the active sprint, and open workflow categories. Press `f` to change these three filters in one dialog. The app caches each profile/board/scope list for five minutes, paints cached rows immediately, and refreshes in the background. A failed refresh keeps stale rows visible with a warning. Successful ticket mutations invalidate the affected board cache.

Worktrees and PRs are matched by a JIRA key such as `APP-123` in the branch name, with PR titles used as a fallback. New ticket worktrees use `<KEY>-<summary-slug>` and reuse an existing matching worktree when possible.

ACLI currently exposes a board's associated projects but not its private board filter. Non-sprint board loading therefore searches those projects; active-sprint mode uses the board's active sprint IDs and is scoped exactly.
