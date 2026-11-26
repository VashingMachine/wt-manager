# Agent Instructions

## Functional requirements and tests

- Record every new or changed functional requirement as a focused Go test.
- Precede each such test with a short comment describing the behavior it assures.
- Run the focused tests for the changed behavior and the full Go test suite before completing a feature or improvement, so existing behavior remains protected.

## Architecture

wt-manager is built as ports and adapters (hexagonal architecture). Dependencies point inwards. Two checks enforce that, and both need updating when a package is added or a rule changes:

- `go test ./internal/arch` (part of the test suite; the rules are the `allowedImports` table)
- `golangci-lint run ./...` (the `depguard` rules in `.golangci.yml`, which also show up in an editor)

```
main.go            picks the terminal UI or a CLI command
internal/
  tui/  cli/       the UIs; they call core rules directly and reach the outside world only through core.Services
  core/            the domain: types and pure rules; ports.go lists what a UI may ask for. No I/O, no internal imports
  services/        the application: implements core.Services with caching, fallbacks and concurrency limits;
                   sees the outside world only through the interfaces in its ports.go
  integrations/    one package per outside system; they never import each other
    git/  github/  jira/  terminal/  editor/  browser/  agent/  configfile/
  app/             the composition root: the only package that knows every integration
  cache/           the SQLite cache behind the services
  shell/           runs external commands and keeps the session log
  fspath/          path helpers that look at the filesystem
  testkit/         fixtures shared by tests
  arch/            tests for the rules above
```

### Where code goes

- A rule that needs no outside world (normalizing config, naming a branch, filtering rows) is a function in `core`, tested with plain values.
- Code that runs a program, reads a file or calls an API belongs to an integration.
- A decision about when and how to use integrations (what is cached and for how long, what a failed refresh falls back to, combining two of them) belongs to `services`.
- What is shown or typed belongs to `tui` or `cli`.

### Adding an integration

1. Create `internal/integrations/<name>/` with one exported type and a `New()` constructor. Keep the functions behind it unexported and test them in the same package, against a scripted executable on PATH (`testkit.WriteExecutable`) or a throwaway repository (`testkit.GitRepo`).
2. Declare what the application needs from it as an interface in `internal/services/ports.go` and add it to `Integrations`.
3. Use it from a `Service` method, and add that method to `internal/core/ports.go` when a UI needs it.
4. Wire it in `internal/app/app.go` and in `internal/services/wiring_test.go`.

Run external commands with `shell.Run` or `shell.CombinedOutput` and the context you were given, so they show up in the session log.

To extend an existing integration, stay inside its package: a new terminal app is a file in `integrations/terminal` plus its row in `hosts`; a new editor fallback goes in `integrations/editor`.
