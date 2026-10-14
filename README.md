# wt-manager

`wt-manager` is a terminal UI for keeping Git worktrees under control across one or more repository profiles.

It gives you a compact dashboard for every worktree in a repository: branch, dirty state, ahead/behind counts, changed files, pull request metadata, and the latest commit. From the same screen you can create, delete, filter, refresh, open, and jump into worktrees without remembering the exact `git worktree` commands.

It also includes PR Radar, a remote pull request view backed by GitHub CLI. PR Radar lets you browse open, closed, and merged PRs for the active repository profile, filter them by user or text, inspect files/checks/comments/diffs, approve selected PRs, create a worktree from a PR branch, and ask a configured AI agent about the selected PR context.

Profiles can optionally enable a third JIRA Tickets view backed by Atlassian CLI. It shows board tickets with their workflow status, matching local worktree, and matching GitHub pull request, and supports reading and updating the selected ticket without leaving the terminal.

## Highlights

- Browse managed worktrees in a Bubble Tea terminal interface.
- See branch state, changed-file counts, PR state, and latest commit at a glance.
- Switch between repository profiles from inside the app.
- Group several repositories into one project: their worktrees are listed together with a Repo column, and every new worktree asks which repository it belongs to.
- Edit the configuration from the app: open the JSON file in your editor, add or remove project repositories from the command palette, and have saved edits applied while the app runs.
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
- Paint worktrees, PRs, and tickets from a local cache immediately, then refresh them piece by piece in the background.
- Prefetch PR Radar and JIRA Tickets while you work elsewhere, so switching views shows current data without waiting.
- Apply ticket status changes, comments, assignments, and PR approvals immediately, rolling back if the remote call fails.
- Configure everything interactively on first run with a guided project setup: the project's repositories and worktree folders, GitHub repos, agent commands, and JIRA boards.

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

To add or remove a project's repositories with guided questions, see [Adding and removing repositories from the command line](#adding-and-removing-repositories-from-the-command-line).

## Configuration

`wt-manager` stores configuration in `~/.wt-manager.json`.

If the file is missing, has no profiles, or the current Git repository is not configured yet, the app opens the setup wizard. It walks through five steps, shown at the top of the wizard; `esc` returns to the previous step and nothing is saved before the last one:

1. **Project**: the project's name. It defaults to the project that already contains the current repository, or to the repository's folder name. Typing the name of another configured project edits that project instead (and adds the current repository to it); typing a new name while editing renames the project.
2. **Repositories**: the project's repositories, starting with the current one. `a` adds another: the picker lists only git repositories (no plain folders, nothing inside a repository, no linked worktrees, nor repositories already in the project), then asks for its name and worktrees folder with defaults that follow the project's layout. `enter` on a repository edits it and `d` removes it from the list.
3. **Agents**: the agent tools to offer, with installed ones preselected.
4. **JIRA** (optional): board links to browse tickets for.
5. **Review**: everything that will be saved, including each repository's GitHub repository (inferred from its remote) and worktree folders that will be created. `enter` saves; `gh` access is checked as a warning.

Inside the app, `I` opens the wizard for the active project, or for the repository the app was started in when no project has it yet. **New project…** in the command palette sets up another project.

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

### Projects with several repositories

A profile is a project. A project with one repository uses the flat `repositoryPath`, `worktreesDir`, `remoteName`, and `githubRepo` fields shown above. A project spanning several repositories lists them under `repositories` instead; the JIRA setup is shared by all of them:

```json
{
  "name": "shop",
  "repositories": [
    { "name": "api", "repositoryPath": "~/projects/api", "worktreesDir": "~/worktrees/api", "githubRepo": "owner/api" },
    { "name": "web", "repositoryPath": "~/projects/web", "worktreesDir": "~/worktrees/web" }
  ],
  "lastRepository": "api",
  "jira": { "...": "..." }
}
```

- `name` defaults to the repository folder name and must be unique within the project; `remoteName` defaults to `origin`; `githubRepo` is inferred from the remote when empty.
- If a profile has both the flat fields and `repositories`, the flat repository is moved into the list as its first entry.
- `lastRepository` is maintained by the app: it is the repository offered first the next time you create a worktree.
- Repositories may share a `worktreesDir`. New worktree folders there are prefixed with the repository name (`api-APP-123-fix`, `web-APP-123-fix`), so the same branch can exist in every repository.

With more than one repository:

- The worktree list shows every repository's worktrees, grouped by repository, with a **Repo** column. `/` also matches repository names.
- `n` asks which repository to create the worktree in. The last used repository is highlighted and preselected; use `j`/`k` and `enter`, or press its number. Projects with one repository are never asked.
- Creating a worktree from a JIRA ticket (`n`, or `v`/`i` when the ticket has no worktree yet) asks the same question. If the ticket already has a worktree in the chosen repository, that worktree is reused. Ticket rows and details show which repository each ticket worktree belongs to.
- PR labels come from each worktree's own repository.
- PR Radar shows one repository at a time; press `R` to choose which.

### Adding and removing repositories from the command line

`wt-manager repo` changes projects without opening the UI. Every question shows its default in brackets; press enter to accept it. Invalid answers are explained and asked again, and nothing is written until you confirm the summary.

```text
$ cd ~/projects/web && wt-manager repo add
Add a repository to a wt-manager project. Press enter to accept the [default].

Repository folder [~/projects/web]:
Projects: shop (api)
Project [shop]:
Repository name [web]:
Worktrees folder [~/worktrees/web]:
Remotes: origin, upstream
Git remote [origin]:
GitHub repository (owner/name) [acme/web]:

  Project:     shop (api, web)
  Repository:  web  ~/projects/web
  Worktrees:   ~/worktrees/web
  Remote:      origin
  GitHub:      acme/web
Save to ~/.wt-manager.json? [Y/n]:
Added web to shop. A running wt-manager picks the change up within a few seconds.
```

- `wt-manager repo add [folder]` adds a repository. The defaults are the git repository you run it in, the default project, the folder name (made unique within the project), a worktrees folder following the project's layout, `origin` (or the repository's only remote), and the GitHub repository of that remote. Naming a project that does not exist creates it; with no config at all, the first project is created from the repository.
- `wt-manager repo remove [repository]` removes a repository from a project, after a `y/N` confirmation that defaults to no. It suggests the repository you run it in and accepts a number or a name. Removing a project's only repository offers to remove the project. Folders and worktrees stay on disk.
- `wt-manager repo list` shows every project and its repositories.

### Editing the configuration from the app

The command palette (`ctrl+p`) includes:

- **Edit configuration file**: opens `~/.wt-manager.json` in the configured editor. When `$VISUAL` or `$EDITOR` is set, **Edit configuration file in terminal** opens it in that editor inside the terminal and applies it when the editor exits.
- **Reload configuration**: reads the file again.
- **Add repository to <project>…**: the same repository questions as setup: pick one of the git repositories found near the project, then confirm its name and worktrees folder. The project is converted to the `repositories` form if needed.
- **Remove repository from <project>…**: stops listing a repository after a second `enter`; its files and worktrees stay on disk.
- **Edit project <project>…** (`I`), **New project…**, **Choose PR Radar repository**, **Switch project**, and the editor, agent, and Enter action settings.

The file is checked every two seconds while the app runs, and saved edits are applied immediately; the active project is reloaded when its own settings changed. If the file has errors, the app reports them and does not save in-app changes until the file is fixed, so a half-finished edit is never overwritten. If the file changed on disk before an in-app change was saved, the file wins: it is reloaded and the app asks you to repeat the change.

Saving the setup wizard gives the project exactly the repositories listed in its Repositories step.

## Caching and Background Loading

`wt-manager` keeps what it fetched in memory and in a SQLite cache at `~/.wt-manager/cache/wt-manager.db`, so every view paints immediately, also after a restart, and is then refreshed in the background:

- **Worktrees**: `git worktree list` replaces the rows first; each worktree's status and last commit then arrive independently, and PR labels arrive separately. Rows whose status has never been read show a loading marker instead of `clean`. The status bar shows the progress, for example `status 3/12 · PR labels`. PR labels are reused for two minutes; `r` refetches them.
- **PR Radar**: one request lists the PRs and their check runs fill in per row, open PRs first. Completed check runs are reused for 15 minutes, pending ones for 30 seconds. A previously viewed PR shows its cached details and diff immediately and is refetched only when it changed. A PR detail's GitHub requests run concurrently.
- **Prefetching**: shortly after start and then every four minutes, the PR list and the active JIRA board are refreshed while you are in another view.
- **Mutations**: worktree creation and deletion run one at a time per repository in the order you issued them, so concurrent `git` operations cannot fight over repository locks. Ticket edits are queued per ticket and approvals per PR. Creating a worktree for an existing local branch skips `git fetch`, and creations queued together share one fetch.

The cache only speeds things up: deleting the file is safe, and if it cannot be opened the app keeps running with an in-memory cache and records why in the session log.

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
| `n` | Create a worktree (asks for the repository when the project has several) |
| `R` | Choose the repository PR Radar shows (projects with several repositories) |
| `s` | Switch project (repository profile) |
| `I` | Edit the active project in the setup wizard |
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

New worktrees are created under `<worktreesDir>/<branch-with-slashes-replaced-by-dashes>` of the chosen repository. When several repositories of a project share that folder, the folder name is prefixed with the repository name.

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

Press `P` to toggle PR Radar. In a project with several repositories it shows one repository's PRs at a time; press `R` to switch. The app uses `gh` for GitHub access, so no separate OAuth app registration is required. If `gh` is missing, unauthenticated, or cannot access the configured repository, PR Radar shows a status error with the next action.

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

Press `J` to toggle JIRA Tickets for the active repository profile. Open the setup wizard with `I`, go to its JIRA step, paste a JIRA board link, and press enter to resolve it; enter on an empty input continues to the review, where enter saves. Multiple board links can be added before saving. ACLI authentication remains external to `wt-manager`; no Atlassian credentials are stored in the config.

Tickets default to the current user, the active sprint, and open workflow categories. Press `f` to change these three filters in one dialog. The app caches each profile/board/scope list for five minutes, paints cached rows immediately, and refreshes in the background. A failed refresh keeps stale rows visible with a warning. Status changes, comments, and assignments appear immediately; a failed change is rolled back with an error. Successful changes update every cached copy of the ticket and mark the board's lists for revalidation.

Worktrees and PRs are matched by a JIRA key such as `APP-123` in the branch name, with PR titles used as a fallback. New ticket worktrees use `<KEY>-<summary-slug>` and reuse an existing matching worktree when possible. In a project with several repositories, worktrees and PRs of every repository are matched, and creating a ticket worktree asks for the repository first.

ACLI currently exposes a board's associated projects but not its private board filter. Non-sprint board loading therefore searches those projects; active-sprint mode uses the board's active sprint IDs and is scoped exactly.
