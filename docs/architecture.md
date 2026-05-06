# Architecture

Last verified: 2026-05-07

## Purpose

`droid-switcher` is a small Go CLI that manages multiple Factory Droid logins without reimplementing Droid authentication. It delegates login to the installed `droid` binary, stores each account in an isolated Factory home, and swaps only the auth files that Droid already expects in `~/.factory`.

## Key Files

| File | Role |
|------|------|
| `main.go` | Thin process entry point that delegates all behavior to the switcher package |
| `internal/switcher/cli.go` | Top-level command routing, flag parsing, list/current rendering, and help text |
| `internal/switcher/ui.go` | No-argument interactive home screen and guided menu flows |
| `internal/switcher/store.go` | Account persistence, switch operations, labels/defaults, and active-account tracking |
| `internal/switcher/login.go` | Droid-auth-backed login flow using isolated Factory homes |
| `internal/switcher/droid.go` | Process runner that launches `droid` with `FACTORY_HOME_OVERRIDE` |
| `internal/switcher/quota.go` | `/limits` execution and quota-window summarization |
| `internal/switcher/metadata.go` | Label/default-account metadata persisted alongside saved accounts |
| `internal/switcher/fileops.go` | Atomic file writes and copies for auth and metadata state |
| `internal/switcher/version.go` | Build-time version surface for the CLI |

## Flow / How It Works

```mermaid
flowchart TD
    A[main.go] --> B[internal/switcher/cli.go]
    B --> C[ui.go interactive menu]
    B --> D[store.go account operations]
    B --> E[login.go Droid-backed login]
    B --> F[quota.go /limits summary]
    E --> G[droid.go process runner]
    F --> G
    D --> H[fileops.go atomic writes]
    D --> I[metadata.go labels and default]
    D --> J[paths.go local storage layout]
```

The entry point intentionally stays thin. `main.go` calls `switcher.Run`, and `internal/switcher/cli.go` decides whether to enter a direct subcommand path or the interactive menu from `internal/switcher/ui.go`.

State is split into two layers:

1. Per-account isolated Factory homes under `~/.droid-switcher/accounts/<name>/.factory`
2. Small switcher-owned metadata files for active/default account markers, labels, and backups

The `droid` binary remains the source of truth for authentication creation and `/limits` output. The switcher’s job is orchestration, persistence, and display.

## Decisions and Trade-offs

- The project keeps the binary dependency one-way: the switcher calls `droid`, but does not inspect or mutate Droid internals beyond the auth files it already owns.
- The codebase prefers standard library primitives over external CLI/TUI dependencies, which keeps the binary small and the behavior easy to audit.
- The repo separates command routing, interactive UX, storage, and Droid delegation into distinct files so user-facing changes do not force filesystem or process-runner edits.
- Metadata such as labels and defaults is intentionally lightweight. The stable account id remains the filesystem key, while labels only affect UX surfaces.
- The switcher now includes a tiny build-time `version` surface, but keeps release metadata optional by defaulting to `dev` when nothing is injected.

## Gotchas

- `internal/switcher/login.go` must keep using `FACTORY_HOME_OVERRIDE`; bypassing that would mix account state into the real `~/.factory`.
- `internal/switcher/store.go` treats “active” and “default” as separate concepts. Changes that collapse them would alter quota and menu behavior.
- `internal/switcher/store.go` also owns sync-back from live `~/.factory` into the active saved account. That behavior is part of the account-safety contract, not a convenience detail.
- `internal/switcher/fileops.go` uses atomic writes for state files. Replacing those writes with direct writes would make interruptions riskier.
- `internal/switcher/quota.go` depends on human-readable `/limits` output, so `--raw` is part of the contract when parsing drifts.

## Related Docs

- [Account Lifecycle](./account-lifecycle.md)
- [Interactive CLI and Quota](./interactive-cli-and-quota.md)
