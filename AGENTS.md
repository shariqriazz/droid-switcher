# AGENTS.md

## Droid Switcher

`droid-switcher` is a Go CLI for storing multiple Factory Droid logins locally, switching the active auth files in `~/.factory`, and surfacing `/limits` quota across saved accounts.

## Commands

- `go test ./...` runs the meaningful validation for this repo.
- `go run .` opens the interactive numbered menu defined by the CLI layer.
- `go run . help` prints the supported command surface and is useful for checking README-to-code drift after CLI changes.

## Stack

Go | standard library CLI and filesystem code | Droid CLI delegation

## Domain Concepts

- A saved "account" is an isolated Factory home under `~/.droid-switcher/accounts/<name>/.factory`, not just a label or token alias.
- The "active account" is whichever saved account last copied its auth into `~/.factory`; it is tracked separately from the optional default account.
- The "default account" is a fallback for quota and menu flows when there is no active account marker.
- Friendly labels are optional metadata layered on top of stable account ids so generated ids can still render cleanly in menus and quota views.

## Critical Patterns

- The switcher must keep using Droid's own login flow rather than reimplementing auth. `internal/switcher/login.go` launches `droid` with `FACTORY_HOME_OVERRIDE` so OAuth state is created by Droid itself.
- Account switching only replaces `auth.v2.file` and `auth.v2.key`; do not expand the swap surface without a concrete reason. That contract is encoded in `internal/switcher/constants.go` and applied in `internal/switcher/store.go`.
- Quota is derived from Droid `/limits` output, then summarized into the `5h`, `1wk`, and `1month` windows. Any parsing change must stay resilient to formatting drift and preserve `--raw` as the fallback. See `internal/switcher/quota.go`.
- Interactive UX is a core product surface here, not a bolt-on helper. Running with no args enters the numbered menu in `internal/switcher/ui.go`, and `select`/bare `switch` rely on `internal/switcher/interactive.go`.
- Account labels and default-account state live alongside saved accounts and affect list, current, select, and quota displays. Keep those views consistent when changing metadata behavior. See `internal/switcher/metadata.go`, `internal/switcher/store.go`, and `internal/switcher/cli.go`.

## Modularization

The CLI entry point in `main.go` should stay thin. Keep user-facing command routing in `internal/switcher/cli.go`, interactive flows in `internal/switcher/ui.go` and `internal/switcher/interactive.go`, persistent account state in `internal/switcher/store.go` and `internal/switcher/metadata.go`, and Droid-specific delegation in `internal/switcher/login.go`, `internal/switcher/droid.go`, and `internal/switcher/quota.go`.

## Documentation

| Area | Doc |
|------|-----|
| System overview and module boundaries | `docs/architecture.md` |
| Account creation, storage, and switching | `docs/account-lifecycle.md` |
| Interactive flows and quota reporting | `docs/interactive-cli-and-quota.md` |
| Operator usage and command examples | `README.md` |

Read relevant docs before changes. Update docs after changes when the task explicitly includes documentation work.
